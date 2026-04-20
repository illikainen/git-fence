package errutil

import (
	"errors"
	"io"
	"os"
)

// revive:disable-next-line
func DeferClose(c io.Closer, err *error) { //nolint
	*err = errors.Join(*err, c.Close())
}

// revive:disable-next-line
func DeferRemove(name string, err *error) { //nolint
	*err = errors.Join(*err, os.RemoveAll(name))
}

// revive:disable-next-line
func DeferRemoveOnErr(name string, err *error) { //nolint
	if *err != nil {
		*err = errors.Join(*err, os.RemoveAll(name))
	}
}
