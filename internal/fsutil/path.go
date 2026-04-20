package fsutil

import (
	"os/user"
	"path/filepath"
	"strings"
)

func ExpandPath(name string) (string, error) {
	if strings.HasPrefix(name, "~") {
		usr, err := user.Current()
		if err != nil {
			return "", err
		}

		return filepath.Join(usr.HomeDir, name[1:]), nil
	}
	return name, nil
}
