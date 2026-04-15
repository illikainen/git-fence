package bwrap

import (
	"bytes"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
)

type Options struct {
	Command  []string
	Stdin    []byte
	RO       []string
	RW       []string
	ShareNet bool
}

type Process struct {
	Stdout []byte
	Stderr []byte
}

func Bubblewrap(opts *Options) (*Process, error) {
	var args []string

	if strings.ToLower(runtime.GOOS) == "linux" {
		args = []string{
			"bwrap",
			"--new-session",
			"--die-with-parent",
			"--unshare-user",
			"--unshare-ipc",
			"--unshare-pid",
			"--unshare-uts",
			"--unshare-cgroup",
			"--dev", "/dev",
			"--tmpfs", "/tmp",
			"--ro-bind-try", "/etc/passwd", "/etc/passwd",
			"--ro-bind-try", "/etc/group", "/etc/group",
			"--ro-bind-try", "/bin", "/bin",
			"--ro-bind-try", "/sbin", "/sbin",
			"--ro-bind-try", "/usr", "/usr",
			"--ro-bind-try", "/lib", "/lib",
			"--ro-bind-try", "/lib32", "/lib32",
			"--ro-bind-try", "/lib64", "/lib64",
		}

		if !opts.ShareNet {
			args = append(args, "--unshare-net")
		} else {
			args = append(args, "--ro-bind-try", "/etc/hosts", "/etc/hosts")
			args = append(args, "--ro-bind-try", "/etc/resolv.conf", "/etc/resolv.conf")
			args = append(args, "--ro-bind-try", "/etc/ssl/certs", "/etc/ssl/certs")
		}

		for _, ro := range opts.RO {
			if ro == "/" {
				return nil, fmt.Errorf("%s: don't mount /", strings.Join(opts.Command, " "))
			}
			args = append(args, "--ro-bind-try", ro, ro)
		}

		for _, rw := range opts.RW {
			if rw == "/" {
				return nil, fmt.Errorf("%s: don't mount /", strings.Join(opts.Command, " "))
			}
			args = append(args, "--bind-try", rw, rw)
		}

		args = append(args, "--")
		args = append(args, opts.Command...)
	} else {
		slog.Debug("running without confinement")
		args = opts.Command
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	// nosemgrep
	cmd := exec.Command(args[0], args[1:]...) // #nosec G204
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if opts.Stdin != nil {
		cmd.Stdin = bytes.NewReader(opts.Stdin)
	}

	if err := cmd.Run(); err != nil {
		msg := strings.Trim(stderr.String(), " \t\r\n")
		return nil, fmt.Errorf("%s\n%w\ncmd: %s", msg, err, strings.Join(args, " "))
	}

	return &Process{
		Stdout: stdout.Bytes(),
		Stderr: stderr.Bytes(),
	}, nil
}
