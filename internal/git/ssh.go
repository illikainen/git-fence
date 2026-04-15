package git

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/illikainen/git-fence/internal/bwrap"
)

func FindPrincipals(sigFile string, allowedSignersFile string) ([]string, error) {
	p, err := bwrap.Bubblewrap(&bwrap.Options{
		Command: []string{"ssh-keygen", "-Y", "find-principals", "-f", allowedSignersFile, "-s", sigFile},
		RO:      []string{allowedSignersFile, sigFile},
	})
	if err != nil {
		return nil, err
	}

	principals := strings.Split(strings.Trim(string(p.Stdout), " \n"), "\n")
	if len(principals) == 0 {
		return nil, fmt.Errorf("%s: no principals found", allowedSignersFile)
	}

	return principals, nil
}

func VerifyPrincipal(sigFile string, allowedSignersFile string, identity string, data []byte) error {
	p, err := bwrap.Bubblewrap(&bwrap.Options{
		Command: []string{
			"ssh-keygen",
			"-Y", "verify",
			"-n", "git",
			"-I", identity,
			"-f", allowedSignersFile,
			"-s", sigFile,
		},
		Stdin: data,
		RO:    []string{allowedSignersFile, sigFile},
	})
	if err != nil {
		return err
	}

	slog.Debug("ssh-keygen", "operation", "verify", "stdout", string(p.Stdout))
	return nil
}
