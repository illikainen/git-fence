package git

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/illikainen/git-fence/internal/errutil"
	"github.com/illikainen/git-fence/internal/executors"
	"github.com/illikainen/git-fence/internal/log"
)

type Client struct {
	name    string
	origins map[string]string
	cli     *CLI
	remote  executors.Executor
}

func NewClient(uri string) (*Client, error) {
	dir := os.Getenv("GIT_DIR")
	if dir == "" {
		return nil, fmt.Errorf("%s: GIT_DIR is not set", uri)
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	dir = abs

	cli, err := NewCLI(filepath.Dir(dir))
	if err != nil {
		return nil, err
	}

	verbosity, err := cli.Verbosity()
	if err != nil {
		return nil, err
	}

	slog.SetDefault(slog.New(log.NewSanitizedHandler(os.Stderr, &log.HandlerOptions{
		Name:  "fence",
		Level: log.ParseLevel(verbosity),
	})))

	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}

	origin, ok := u.Query()["origin"]
	if !ok || len(origin) < 1 {
		return nil, fmt.Errorf("%s: missing origin", uri)
	}

	origins := map[string]string{}
	for i, o := range origin {
		origins[fmt.Sprintf("origin%d", i)] = o
	}

	remote, err := executors.Lookup(u)
	if err != nil {
		return nil, err
	}

	return &Client{
		name:    filepath.Base(filepath.Dir(dir)),
		origins: origins,
		cli:     cli,
		remote:  remote,
	}, nil
}

func (c *Client) Run() error {
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		line := scan.Text()
		slog.Debug("client", "cmd", line)

		switch strings.SplitN(line, " ", 2)[0] {
		case "capabilities":
			if err := c.capabilities(); err != nil {
				return err
			}
		case "list":
			if err := c.list(line); err != nil {
				return err
			}
		case "push":
			if err := c.push(line); err != nil {
				return err
			}
		case "fetch":
			if err := c.fetch(line); err != nil {
				return err
			}
		case "":
			if n, err := os.Stdout.WriteString("\n"); err != nil || n != 1 {
				return fmt.Errorf("bad write: %w", err)
			}
		default:
			return fmt.Errorf("%s: unsupported command", line)
		}

		if line == "" {
			break
		}
	}

	return scan.Err()
}

func (c *Client) capabilities() error {
	_, err := os.Stdout.WriteString("fetch\npush\n\n")
	return err
}

func (c *Client) list(args string) error {
	forPush := false
	elts := strings.SplitN(args, " ", 2)
	if len(elts) == 2 && elts[1] == "for-push" {
		forPush = true
	}

	stdout := &bytes.Buffer{}
	stdin, err := json.Marshal(Command[ListArgs]{
		Name: "list",
		Repo: c.name,
		Args: ListArgs{
			Origins: c.origins,
			ForPush: forPush,
		},
	})
	if err != nil {
		return err
	}

	cmd, err := c.remote.Command()
	if err != nil {
		return err
	}

	cmd.Stdout = stdout
	cmd.Stdin = bytes.NewReader(stdin)

	if err := cmd.Run(); err != nil {
		return err
	}

	if stdout.Len() > 0 {
		var refs []string
		for _, ref := range strings.Split(strings.Trim(stdout.String(), "\n"), "\n") {
			if _, err := ValidateRefLine(ref); err != nil {
				return err
			}
			refs = append(refs, ref)
			slog.Debug("list", "ref", ref)
		}

		result := strings.Join(refs, "\n") + "\n\n"
		if n, err := os.Stdout.WriteString(result); err != nil || n != len(result) {
			return fmt.Errorf("bad write: %w", err)
		}
	} else {
		if n, err := os.Stdout.WriteString("\n"); err != nil || n != 1 {
			return fmt.Errorf("bad write: %w", err)
		}
	}
	return nil
}

func (c *Client) push(args string) error {
	elts := strings.SplitN(args, " ", 2)
	dst := strings.SplitN(elts[1], ":", 2)[1]

	bundle, err := c.cli.CreateBundle()
	if err != nil {
		return err
	}

	stdin, err := json.Marshal(Command[PushArgs]{
		Name: "push",
		Repo: c.name,
		Args: PushArgs{
			Origins: c.origins,
			Dst:     dst,
			Bundle:  bundle,
		},
	})
	if err != nil {
		return err
	}

	cmd, err := c.remote.Command()
	if err != nil {
		return err
	}

	stdout := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stdin = bytes.NewReader(stdin)
	if err := cmd.Run(); err != nil {
		return err
	}

	status, err := ValidatePrintable(stdout.String(), true)
	if err != nil {
		return err
	}

	if n, err := os.Stdout.WriteString(status); err != nil || n != len(status) {
		return fmt.Errorf("%s: invalid write", dst)
	}
	return nil
}

func (c *Client) fetch(args string) error {
	refs, err := c.cli.References()
	if err != nil {
		return err
	}

	elts := strings.SplitN(args, " ", 3)
	stdin, err := json.Marshal(Command[FetchArgs]{
		Name: "fetch",
		Repo: c.name,
		Args: FetchArgs{
			Origins: c.origins,
			RefSHA1: elts[1],
			RefName: elts[2],
			OldRefs: refs,
		},
	})
	if err != nil {
		return err
	}

	cmd, err := c.remote.Command()
	if err != nil {
		return err
	}
	cmd.Stdin = bytes.NewReader(stdin)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	var goErr error
	decoder := json.NewDecoder(stdout)
	objs := map[string]*Object{}

	go func() {
		for {
			var obj Object
			if err := decoder.Decode(&obj); err != nil {
				if !errors.Is(err, io.EOF) {
					goErr = err
				}
				return
			}
			objs[obj.Ref] = &obj
		}
	}()

	if err := cmd.Run(); err != nil {
		return err
	}

	if goErr != nil {
		return goErr
	}

	slog.Debug("fetched objects", "len", len(objs))
	return c.processObjects(elts[1], objs, true)
}

func (c *Client) processObjects(ref string, objs map[string]*Object, root bool) (err error) {
	if _, err := ValidateSHA1(ref); err != nil {
		return err
	}

	gitDir, err := c.cli.GitDir()
	if err != nil {
		return err
	}

	objFile := filepath.Join(gitDir, "objects", ref[:2], ref[2:])
	if _, err := os.Stat(objFile); err == nil || !errors.Is(err, os.ErrNotExist) {
		return err
	}

	obj, ok := objs[ref]
	if !ok {
		return fmt.Errorf("%s: unknown object", ref)
	}

	if err := obj.Verify(); err != nil {
		return err
	}

	if root {
		commit, err := obj.Commit()
		if err != nil {
			return err
		}

		if err := commit.VerifySignature(c.cli.Dir); err != nil {
			return err
		}
	}

	switch obj.Type() {
	case "commit":
		commit, err := obj.Commit()
		if err != nil {
			return err
		}

		tree, err := commit.Tree()
		if err != nil {
			return err
		}

		if err := c.processObjects(tree, objs, false); err != nil {
			return err
		}

		parent, err := commit.Parent()
		if err != nil {
			return err
		}

		if parent != "" {
			if err := c.processObjects(parent, objs, false); err != nil {
				return err
			}
		}
	case "tree":
		tree, err := obj.Tree()
		if err != nil {
			return err
		}

		treeObjs, err := tree.Objects()
		if err != nil {
			return err
		}

		for _, treeObj := range treeObjs {
			if err := c.processObjects(treeObj, objs, false); err != nil {
				return err
			}
		}
	case "blob":
	default:
		return fmt.Errorf("%s: unsupported object type", obj.Type())
	}

	if err := os.MkdirAll(filepath.Dir(objFile), 0o700); err != nil { // nosemgrep
		return err
	}
	defer errutil.DeferRemoveOnErr(filepath.Dir(objFile), &err)

	f, err := os.Create(objFile) // #nosecc G304
	if err != nil {
		return err
	}
	defer errutil.DeferClose(f, &err)

	writer := zlib.NewWriter(f)
	if n, err := writer.Write(obj.Raw); err != nil || n != len(obj.Raw) {
		return fmt.Errorf("bad write: %w", err)
	}

	return writer.Close()
}
