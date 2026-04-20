package git

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/illikainen/git-fence/internal/errutil"
)

type Commit struct {
	*Object
}

func (c *Commit) VerifySignature(dir string) (err error) {
	var sig []byte
	var commit []byte
	inHeader := true
	inSig := false

	for _, line := range bytes.Split(bytes.TrimRight(c.Data(), "\n"), []byte("\n")) {
		if len(line) <= 0 {
			inHeader = false
		}

		if inHeader && bytes.HasPrefix(line, []byte("gpgsig ")) {
			line = line[len("gpgsig"):]
			inSig = true
		}

		if inSig && bytes.HasPrefix(line, []byte(" ")) {
			sig = append(sig, bytes.Trim(line, " ")...)
			sig = append(sig, '\n')
		} else {
			inSig = false
			commit = append(commit, line...)
			commit = append(commit, '\n')
		}
	}

	if len(sig) == 0 || len(commit) == 0 {
		return fmt.Errorf("%s: missing commit signature", dir)
	}

	tmp, err := os.MkdirTemp("", "")
	if err != nil {
		return err
	}
	defer errutil.DeferRemove(tmp, &err)

	sigFile := filepath.Join(tmp, "sig")
	if err := os.WriteFile(sigFile, sig, 0o600); err != nil {
		return err
	}

	cli, err := NewCLI(dir)
	if err != nil {
		return err
	}

	allowedSignersFile, err := cli.AllowedSignersFile()
	if err != nil {
		return err
	}

	principals, err := FindPrincipals(sigFile, allowedSignersFile)
	if err != nil {
		return err
	}

	verified := false
	for _, principal := range principals {
		if err := VerifyPrincipal(sigFile, allowedSignersFile, principal, commit); err == nil {
			verified = true
			break
		}
	}

	if !verified {
		return fmt.Errorf("%s: failed to verify object", dir)
	}

	if _, err := ValidatePrintable(string(commit), false); err != nil {
		return err
	}
	return nil
}

func (c *Commit) Tree() (string, error) {
	for _, line := range bytes.Split(bytes.TrimRight(c.Data(), "\n"), []byte("\n")) {
		if len(line) == 0 {
			break
		}

		if bytes.HasPrefix(line, []byte("tree ")) {
			return ValidateSHA1(string(line[len("tree "):]))
		}
	}
	return "", fmt.Errorf("no tree found: %s", c.Data())
}

func (c *Commit) Parent() (string, error) {
	for _, line := range bytes.Split(bytes.TrimRight(c.Data(), "\n"), []byte("\n")) {
		if len(line) == 0 {
			break
		}

		if bytes.HasPrefix(line, []byte("parent ")) {
			return ValidateSHA1(string(line[len("parent "):]))
		}
	}
	return "", nil
}
