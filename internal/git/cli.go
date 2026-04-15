package git

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/illikainen/git-fence/internal/bwrap"
	"github.com/illikainen/git-fence/internal/errutil"
	"github.com/illikainen/git-fence/internal/fsutil"
)

type CLI struct {
	Dir  string
	home string
}

func NewCLI(dir string) (*CLI, error) {
	usr, err := user.Current()
	if err != nil {
		return nil, err
	}

	return &CLI{
		Dir:  dir,
		home: usr.HomeDir,
	}, nil
}

func (c *CLI) Init() (err error) {
	// needs to exist for bubblewrap
	if err := os.MkdirAll(c.Dir, 0o700); err != nil { // nosemgrep
		return err
	}
	defer errutil.DeferRemoveOnErr(c.Dir, &err)

	slog.Debug("initializing", "dir", c.Dir)
	_, err = bwrap.Bubblewrap(&bwrap.Options{
		Command: []string{"git", "init", c.Dir},
		RW:      []string{c.Dir},
	})
	return err
}

func (c *CLI) Clone(uri string, origin string) (err error) {
	slog.Debug("clone", "dir", c.Dir, "url", uri)
	if !strings.HasPrefix(uri, "ssh://") {
		return fmt.Errorf("%s: unsupported scheme", uri)
	}

	// needs to exist for bubblewrap
	if err := os.MkdirAll(c.Dir, 0o700); err != nil { // nosemgrep
		return err
	}
	defer errutil.DeferRemoveOnErr(c.Dir, &err)

	if _, err := bwrap.Bubblewrap(&bwrap.Options{
		Command:  []string{"git", "clone", "--origin", origin, uri, c.Dir},
		RO:       []string{filepath.Join(c.home, ".ssh")},
		RW:       []string{c.Dir},
		ShareNet: true,
	}); err != nil {
		return err
	}

	refs, err := c.References()
	if err != nil || len(refs) <= 0 {
		return fmt.Errorf("can't parse references: %w", err)
	}

	for _, ref := range refs {
		verify, err := c.VerifyCommit(ref.Name)
		if err != nil {
			return err
		}
		slog.Debug("verified", "dir", c.Dir, "ref", ref.Name, "output", verify)
	}

	return nil
}

func (c *CLI) Fetch(ref string) error {
	slog.Debug("fetch", "dir", c.Dir, "ref", ref)

	remotes, err := c.ListRemotes()
	if err != nil {
		return err
	}

	ro := []string{filepath.Join(c.home, ".ssh")}
	// for local remotes/bundles
	for _, uri := range remotes {
		if strings.HasPrefix(uri, "/") {
			ro = append(ro, uri)
		}
	}

	if _, err := bwrap.Bubblewrap(&bwrap.Options{
		Command: []string{
			"git", "-C", c.Dir, "fetch", "--prune", "--tags",
			"--recurse-submodules=on-demand", ref,
		},
		RW:       []string{c.Dir},
		RO:       ro,
		ShareNet: true,
	}); err != nil {
		return err
	}

	refs, err := c.References()
	if err != nil || len(refs) <= 0 {
		return fmt.Errorf("can't parse references: %w", err)
	}

	for _, ref := range refs {
		verify, err := c.VerifyCommit(ref.Name)
		if err != nil {
			return err
		}
		slog.Debug("verify-commit", "dir", c.Dir, "ref", ref.Name, "output", verify)
	}

	return nil
}

func (c *CLI) Push(name string, remote string, force bool) error {
	slog.Debug("push", "dir", c.Dir, "name", name, "remote", remote, "force", force)

	cmd := []string{"git", "-C", c.Dir, "push", "-u", remote, name}
	if force {
		cmd = append(cmd, "--force")
	}

	if _, err := bwrap.Bubblewrap(&bwrap.Options{
		Command:  cmd,
		RO:       []string{c.Dir, filepath.Join(c.home, ".ssh")},
		ShareNet: true,
	}); err != nil {
		return err
	}

	return nil
}

func (c *CLI) VerifyCommit(ref string) (string, error) {
	allowedSignersFile, err := c.AllowedSignersFile()
	if err != nil {
		return "", err
	}

	p, err := bwrap.Bubblewrap(&bwrap.Options{
		Command: []string{"git", "-C", c.Dir, "verify-commit", ref},
		RO: []string{
			c.Dir,
			allowedSignersFile,
			filepath.Join(c.home, ".gitconfig"),
			filepath.Join(c.home, ".config", "git", "config"),
			"/etc/gitconfig",
		},
	})
	if err != nil {
		return "", err
	}

	return strings.Trim(string(p.Stderr), " \r\n"), nil
}

type Reference struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

func (c *CLI) References() ([]*Reference, error) {
	p, err := bwrap.Bubblewrap(&bwrap.Options{
		Command: []string{"git", "-C", c.Dir, "show-ref", "--head"},
		RO:      []string{c.Dir},
	})
	if err != nil {
		return nil, err
	}

	var refs []*Reference
	for _, line := range strings.Split(strings.Trim(string(p.Stdout), "\n"), "\n") {
		elts := strings.Split(line, " ")
		if len(elts) != 2 {
			return nil, fmt.Errorf("%s: invalid reference line: %s", c.Dir, line)
		}

		refs = append(refs, &Reference{
			Name: elts[1],
			Hash: elts[0],
		})
	}

	if len(refs) == 0 {
		return nil, fmt.Errorf("%s: no refs found", c.Dir)
	}

	return refs, nil
}

func (c *CLI) RevParse(ref string) (string, error) {
	p, err := bwrap.Bubblewrap(&bwrap.Options{
		Command: []string{"git", "-C", c.Dir, "rev-parse", "--verify", ref},
		RO:      []string{c.Dir},
	})
	if err != nil {
		return "", err
	}

	return strings.Trim(string(p.Stdout), " \t\r\n"), nil
}

type Revision struct {
	Name string
	Hash string
}

func (c *CLI) RevList(ref string) ([]*Revision, error) {
	p, err := bwrap.Bubblewrap(&bwrap.Options{
		Command: []string{"git", "-C", c.Dir, "rev-list", "--objects", ref},
		RO:      []string{c.Dir},
	})
	if err != nil {
		return nil, err
	}

	var revs []*Revision
	for _, line := range strings.Split(strings.Trim(string(p.Stdout), "\n"), "\n") {
		elts := strings.SplitN(strings.Trim(line, " "), " ", 2)

		cksum := elts[0]
		if _, err := ValidateSHA1(cksum); err != nil {
			return nil, err
		}

		name := ""
		if len(elts) == 2 {
			if _, err := ValidatePath(elts[1]); err != nil {
				return nil, err
			}
			name = elts[1]
		}

		revs = append(revs, &Revision{
			Name: name,
			Hash: cksum,
		})
	}
	return revs, nil
}

func (c *CLI) ObjectType(obj string) (string, error) {
	p, err := bwrap.Bubblewrap(&bwrap.Options{
		Command: []string{"git", "-C", c.Dir, "cat-file", "-t", obj},
		RO:      []string{c.Dir},
	})
	if err != nil {
		return "", err
	}

	return ValidateObjectType(strings.Trim(string(p.Stdout), " \n"))
}

func (c *CLI) ObjectSize(obj string) (string, error) {
	p, err := bwrap.Bubblewrap(&bwrap.Options{
		Command: []string{"git", "-C", c.Dir, "cat-file", "-s", obj},
		RO:      []string{c.Dir},
	})
	if err != nil {
		return "", err
	}

	return ValidateObjectSize(strings.Trim(string(p.Stdout), " \n"))
}

func (c *CLI) ObjectData(obj string, typ string) ([]byte, error) {
	p, err := bwrap.Bubblewrap(&bwrap.Options{
		Command: []string{"git", "-C", c.Dir, "cat-file", typ, obj},
		RO:      []string{c.Dir},
	})
	if err != nil {
		return nil, err
	}

	return p.Stdout, nil
}

func (c *CLI) CreateBundle() ([]byte, error) {
	p, err := bwrap.Bubblewrap(&bwrap.Options{
		Command: []string{"git", "-C", c.Dir, "bundle", "create", "-", "--all"},
		RO:      []string{c.Dir},
	})
	if err != nil {
		return nil, err
	}

	return p.Stdout, nil
}

func (c *CLI) ListRemotes() (map[string]string, error) {
	p, err := bwrap.Bubblewrap(&bwrap.Options{
		Command: []string{"git", "-C", c.Dir, "remote", "-v"},
		RO:      []string{c.Dir},
	})
	if err != nil {
		return nil, err
	}

	remotes := map[string]string{}
	if len(p.Stdout) > 0 {
		for _, line := range strings.Split(strings.Trim(string(p.Stdout), "\n"), "\n") {
			elts := strings.SplitN(line, "\t", 2)
			othr := strings.SplitN(elts[1], " ", 2)
			remotes[elts[0]] = othr[0]
		}
	}
	return remotes, nil
}

func (c *CLI) AddRemote(name string, uri string) error {
	_, err := bwrap.Bubblewrap(&bwrap.Options{
		Command: []string{"git", "-C", c.Dir, "remote", "add", name, uri},
		RW:      []string{c.Dir},
	})
	return err
}

func (c *CLI) RemoveRemote(name string) error {
	_, err := bwrap.Bubblewrap(&bwrap.Options{
		Command: []string{"git", "-C", c.Dir, "remote", "remove", name},
		RW:      []string{c.Dir},
	})
	return err
}

func (c *CLI) RemoveBranch(name string) error {
	_, err := bwrap.Bubblewrap(&bwrap.Options{
		Command: []string{"git", "-C", c.Dir, "branch", "-D", name},
		RW:      []string{c.Dir},
	})
	return err
}

func (c *CLI) Checkout(name string, ref string) error {
	_, err := bwrap.Bubblewrap(&bwrap.Options{
		Command: []string{"git", "-C", c.Dir, "checkout", "-B", name, ref},
		RW:      []string{c.Dir},
	})
	return err
}

func (c *CLI) GitDir() (string, error) {
	p, err := bwrap.Bubblewrap(&bwrap.Options{
		Command: []string{"git", "-C", c.Dir, "rev-parse", "--absolute-git-dir"},
		RO:      []string{c.Dir},
	})
	if err != nil {
		return "", err
	}

	return strings.Trim(string(p.Stdout), " \r\n"), nil
}

func (c *CLI) ConfigGet(opt string, fallback string) (string, error) {
	dir := c.Dir
	if _, err := os.Stat(dir); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		dir = "."
	}

	p, err := bwrap.Bubblewrap(&bwrap.Options{
		Command: []string{"git", "-C", dir, "config", "--get", "--default", fallback, "--", opt},
		RO: []string{
			c.Dir,
			filepath.Join(c.home, ".gitconfig"),
			filepath.Join(c.home, ".config", "git", "config"),
			"/etc/gitconfig",
		},
	})
	if err != nil {
		return "", err
	}

	return strings.Trim(string(p.Stdout), " \t\r\n"), nil
}

func (c *CLI) AllowedSignersFile() (string, error) {
	value, err := c.ConfigGet("gpg.ssh.allowedSignersFile", "")
	if err != nil || value == "" {
		return "", fmt.Errorf("%s: gpg.ssh.allowedSignersFile is not configured", c.Dir)
	}

	path, err := fsutil.ExpandPath(value)
	if err != nil {
		return "", err
	}

	return path, nil
}

func (c *CLI) Verbosity() (string, error) {
	return c.ConfigGet("fence.verbosity", "warn")
}
