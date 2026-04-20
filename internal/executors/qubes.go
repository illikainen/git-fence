package executors

import (
	"os/exec"

	"github.com/illikainen/git-fence/internal/log"
)

type Qubes struct {
	qvm string
}

func NewQubes(qvm string) (*Qubes, error) {
	return &Qubes{
		qvm: qvm,
	}, nil
}

func (q *Qubes) Command() (*exec.Cmd, error) {
	cmd := exec.Command("qrexec-client-vm", q.qvm, "git.Run") // #nosec G204

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	log.LogReader(stderr)

	return cmd, nil
}
