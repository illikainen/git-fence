package git

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

type Tree struct {
	*Object
}

func (t *Tree) Objects() ([]string, error) {
	var objs []string
	r := bufio.NewReader(bytes.NewReader(t.Data()))

	for {
		chunk, err := r.ReadBytes('\x00')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		if len(chunk) <= 0 {
			break
		}

		elts := strings.SplitN(string(chunk), " ", 2)
		if _, err := ValidateObjectMode(elts[0]); err != nil {
			return nil, err
		}

		if _, err := ValidatePath(strings.TrimRight(elts[1], "\x00")); err != nil {
			return nil, err
		}

		cksumBytes := make([]byte, 20)
		if n, err := io.ReadFull(r, cksumBytes); err != nil || n != len(cksumBytes) {
			return nil, fmt.Errorf("bad object checksum: %w (%d/%d)", err, n, len(cksumBytes))
		}

		cksum, err := ValidateSHA1(hex.EncodeToString(cksumBytes))
		if err != nil {
			return nil, err
		}

		objs = append(objs, cksum)

		if errors.Is(err, io.EOF) {
			break
		}
	}
	return objs, nil
}
