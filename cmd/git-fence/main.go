package main

import (
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
	service, err := git.NewService()
	if err != nil {
		return err
	}
	return service.Run()
}
