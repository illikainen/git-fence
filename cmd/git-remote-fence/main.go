package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/illikainen/git-fence/internal/git"
)

func main() {
	if err := run(); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

func run() error {
	if os.Getenv("GIT_DIR") == "" {
		return fmt.Errorf("not running as a git hook")
	}

	if len(os.Args) != 3 {
		return fmt.Errorf("insufficient number of arguments (%d)", len(os.Args))
	}

	client, err := git.NewClient(os.Args[2])
	if err != nil {
		return err
	}
	return client.Run()
}
