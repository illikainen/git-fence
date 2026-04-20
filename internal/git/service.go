package git

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/illikainen/git-fence/internal/errutil"
	"github.com/illikainen/git-fence/internal/fsutil"
	"github.com/illikainen/git-fence/internal/log"
)

const repoRoot = "~/git"

type Service struct{}

func NewService() (*Service, error) {
	return &Service{}, nil
}

type Command[T NoArgs | ListArgs | FetchArgs | PushArgs] struct {
	Name string `json:"name"`
	Repo string `json:"repo"`
	Args T      `json:"args"`
}

type NoArgs struct{}

func (s *Service) Run() error {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}

	var cmd Command[NoArgs]
	if err := json.Unmarshal(data, &cmd); err != nil {
		return err
	}

	root, err := fsutil.ExpandPath(repoRoot)
	if err != nil {
		return err
	}

	dir := filepath.Join(root, filepath.Base(cmd.Repo))
	cli, err := NewCLI(dir)
	if err != nil {
		return err
	}

	verbosity, err := cli.Verbosity()
	if err != nil {
		return err
	}

	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: log.ParseLevel(verbosity),
	})
	slog.SetDefault(slog.New(handler))
	slog.Debug("service", "cmd", string(data[:min(len(data), 512)]))

	switch cmd.Name {
	case "list":
		var c Command[ListArgs]
		if err := json.Unmarshal(data, &c); err != nil {
			return err
		}
		return s.list(c.Args, cli)
	case "push":
		var c Command[PushArgs]
		if err := json.Unmarshal(data, &c); err != nil {
			return err
		}
		return s.push(c.Args, cli)
	case "fetch":
		var c Command[FetchArgs]
		if err := json.Unmarshal(data, &c); err != nil {
			return err
		}
		return s.fetch(c.Args, cli)
	}

	return fmt.Errorf("invalid command: %s", data)
}

type ListArgs struct {
	Origins map[string]string `json:"origins"`
	ForPush bool              `json:"for_push"`
}

func (s *Service) list(args ListArgs, cli *CLI) error {
	for name, uri := range args.Origins {
		if err := s.listOrigin(name, uri, args, cli); err != nil {
			return err
		}
	}

	if _, err := os.Stat(cli.Dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	refs, err := cli.References()
	if err != nil {
		return err
	}

	// references from origin are returned because if the local refs are
	// used, then the git client wouldn't know if a push reached the
	// intermediate fence but not the real origin
	originRefs := map[string]string{}
	for name := range args.Origins {
		for _, ref := range refs {
			elts := strings.SplitN(ref.Name, "/", 4)
			if len(elts) == 4 && elts[0] == "refs" && elts[1] == "remotes" &&
				elts[2] == name && elts[3] != "HEAD" {
				originRefs["refs/heads/"+elts[3]] = ref.Hash
			}
		}
	}

	for name, hsh := range originRefs {
		data := hsh + " " + name + "\n"
		if n, err := os.Stdout.WriteString(data); err != nil || n != len(data) {
			return fmt.Errorf("write failure: %w", err)
		}
	}
	return nil
}

func (s *Service) listOrigin(name string, uri string, args ListArgs, cli *CLI) error {
	if _, err := os.Stat(cli.Dir); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}

		if err := cli.Clone(uri, name); err != nil {
			// FIXME: cloning empty repositories fail, but we
			// should test `err` specifically for that
			if args.ForPush {
				return os.RemoveAll(cli.Dir)
			}
			return err
		}
	}

	remotes, err := cli.ListRemotes()
	if err != nil {
		return err
	}

	if remotes[name] == "" {
		if err := cli.AddRemote(name, uri); err != nil {
			return err
		}
	}

	if err := cli.Fetch(name); err != nil {
		// Fetch is allowed to fail on push commands to allow for empty origin
		// repositories.
		if args.ForPush {
			slog.Debug("fetch (in list for-push)", "err", err)
			return nil
		}
		return err
	}

	return nil
}

type PushArgs struct {
	Origins map[string]string `json:"origins"`
	Dst     string            `json:"dst"`
	Bundle  []byte            `json:"bundle"`
}

func (s *Service) push(args PushArgs, cli *CLI) (err error) {
	tmp, err := os.MkdirTemp("", "")
	if err != nil {
		return err
	}
	defer errutil.DeferRemove(tmp, &err)

	bundle := filepath.Join(tmp, "bundle")
	if err := os.WriteFile(bundle, args.Bundle, 0o600); err != nil {
		return err
	}

	for name, uri := range args.Origins {
		if err := s.pushOrigin(name, uri, args, cli, bundle); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) pushOrigin(name string, uri string, args PushArgs, cli *CLI, bundle string) (err error) {
	// if upstream is an empty repository, an empty repository won't exist
	// on disk
	if _, err := os.Stat(cli.Dir); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}

		if err := cli.Init(); err != nil {
			return err
		}
		defer errutil.DeferRemoveOnErr(cli.Dir, &err)
	}

	remotes, err := cli.ListRemotes()
	if err != nil {
		return err
	}

	if remotes["bundle"] != "" {
		if err := cli.RemoveRemote("bundle"); err != nil {
			return err
		}
	}

	if err := cli.AddRemote("bundle", bundle); err != nil {
		return err
	}

	if err := cli.Fetch("bundle"); err != nil {
		return err
	}

	refs, err := cli.References()
	if err != nil {
		return err
	}

	var heads []string
	var active []string

	for _, ref := range refs {
		if strings.HasPrefix(ref.Name, "refs/heads/") {
			heads = append(heads, path.Base(ref.Name))
		} else if strings.HasPrefix(ref.Name, "refs/remotes/bundle/") {
			branch := path.Base(ref.Name)
			if err := cli.Checkout(branch, ref.Name); err != nil {
				return err
			}
			active = append(active, branch)
		}
	}

	for _, head := range heads {
		if !slices.Contains(active, head) {
			if err := cli.RemoveBranch(head); err != nil {
				return err
			}
		}
	}

	if remotes[name] == "" {
		if err := cli.AddRemote(name, uri); err != nil {
			return err
		}
	}

	// while it's a forced push, the client still needs to push with
	// `--force` for it to happen
	if err := cli.Push(path.Base(args.Dst), name, true); err != nil {
		return err
	}

	if _, err := os.Stdout.WriteString("ok " + args.Dst + "\n\n"); err != nil {
		return err
	}
	return nil
}

type FetchArgs struct {
	Origins map[string]string `json:"origins"`
	RefSHA1 string            `json:"ref_sha1"`
	RefName string            `json:"ref_name"`
	OldRefs []*Reference      `json:"old_refs"`
}

func (s *Service) fetch(args FetchArgs, cli *CLI) error {
	var ignore []string
	for _, ref := range args.OldRefs {
		tmp, err := cli.RevList(ref.Hash)
		if err == nil {
			for _, rev := range tmp {
				ignore = append(ignore, rev.Hash)
			}
		}
	}
	encoder := json.NewEncoder(os.Stdout)

	for name := range args.Origins {
		if err := s.fetchOrigin(name, args, cli, ignore, encoder); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) fetchOrigin(name string, args FetchArgs, cli *CLI, ignore []string, w *json.Encoder) error {
	if err := cli.Fetch(name); err != nil {
		return err
	}

	revs, err := cli.RevList(args.RefSHA1)
	if err != nil {
		return err
	}

	for _, rev := range revs {
		if slices.Contains(ignore, rev.Hash) {
			continue
		}

		typ, err := cli.ObjectType(rev.Hash)
		if err != nil {
			return err
		}

		size, err := cli.ObjectSize(rev.Hash)
		if err != nil {
			return err
		}

		data, err := cli.ObjectData(rev.Hash, typ)
		if err != nil {
			return err
		}

		raw := append([]byte(typ), ' ')
		raw = append(raw, []byte(size)...)
		raw = append(raw, '\x00')
		raw = append(raw, data...)
		if err := w.Encode(&Object{
			Ref: rev.Hash,
			Raw: raw,
		}); err != nil {
			return err
		}
	}
	return nil
}
