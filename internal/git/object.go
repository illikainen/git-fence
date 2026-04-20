package git

// nosemgrep: gitlab.gosec.G505-1
import (
	"bytes"
	"crypto/sha1" // #nosec G505
	"encoding/hex"
	"fmt"
)

type Object struct {
	Ref string `json:"ref"`
	Raw []byte `json:"raw"`
}

func (o *Object) Type() string {
	return string(bytes.SplitN(o.Raw, []byte(" "), 2)[0])
}

func (o *Object) Size() string {
	return string(bytes.SplitN(bytes.SplitN(o.Raw, []byte(" "), 2)[1], []byte("\x00"), 2)[0])
}

func (o *Object) Data() []byte {
	return bytes.SplitN(o.Raw, []byte("\x00"), 2)[1]
}

func (o *Object) Verify() error {
	// nosemgrep: go.lang.security.audit.crypto.use_of_weak_crypto.use-of-sha1
	cksumBytes := sha1.Sum(o.Raw) // #nosec G401
	cksum := hex.EncodeToString(cksumBytes[:])
	if cksum != o.Ref {
		return fmt.Errorf("invalid sha1: %s != %s", cksum, o.Ref)
	}

	if _, err := ValidateObjectType(o.Type()); err != nil {
		return err
	}

	if _, err := ValidateObjectSize(o.Size()); err != nil {
		return err
	}

	return nil
}

func (o *Object) Commit() (*Commit, error) {
	if o.Type() != "commit" {
		return nil, fmt.Errorf("%s: invalid object type", o.Type())
	}

	return &Commit{Object: o}, nil
}

func (o *Object) Tree() (*Tree, error) {
	if o.Type() != "tree" {
		return nil, fmt.Errorf("%s: invalid object type", o.Type())
	}

	return &Tree{Object: o}, nil
}
