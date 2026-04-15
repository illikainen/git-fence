package executors

import (
	"fmt"
	"net/url"
	"os/exec"
)

type Executor interface {
	Command() (*exec.Cmd, error)
}

func Lookup(uri *url.URL) (Executor, error) {
	if uri.Scheme == "qubes" {
		return NewQubes(uri.Hostname())
	}

	return nil, fmt.Errorf("%s: unsupported executor", uri.Scheme)
}
