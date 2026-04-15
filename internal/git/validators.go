package git

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var rxSHA1 = regexp.MustCompile(`^[0-9a-f]{40}$`)

func ValidateSHA1(s string) (string, error) {
	if !rxSHA1.MatchString(s) {
		return "", fmt.Errorf("%s: invalid sha1", s)
	}
	return s, nil
}

func ValidateObjectType(s string) (string, error) {
	if !slices.Contains([]string{"blob", "commit", "tree"}, s) {
		return "", fmt.Errorf("%s: invalid object type", s)
	}
	return s, nil
}

var rxObjectSize = regexp.MustCompile(`^[0-9]{1,10}$`)

func ValidateObjectSize(s string) (string, error) {
	if !rxObjectSize.MatchString(s) {
		return "", fmt.Errorf("%s: invalid object size", s)
	}
	return s, nil
}

var rxObjectMode = regexp.MustCompile(`^[0-9]{5,6}$`)

func ValidateObjectMode(s string) (string, error) {
	if !rxObjectMode.MatchString(s) {
		return "", fmt.Errorf("%s: invalid object mode", s)
	}
	return s, nil
}

var rxRefLine = regexp.MustCompile(`^[0-9a-f]{40} refs/[a-zA-Z0-9/._-]+$`)

func ValidateRefLine(s string) (string, error) {
	if !rxRefLine.MatchString(s) || strings.Contains(s, "..") {
		return "", fmt.Errorf("%s: invalid ref line", s)
	}
	return s, nil
}

var rxPath = regexp.MustCompile(`^[a-zA-Z0-9.,+/_-]+$`)

func ValidatePath(s string) (string, error) {
	if !rxPath.MatchString(s) || strings.Contains(s, "..") {
		return "", fmt.Errorf("%s: invalid path", s)
	}
	return s, nil
}

var rxPrintable = regexp.MustCompile(`^[\x0a\x20-\x7e]+$`)

func ValidatePrintable(s string, strict bool) (string, error) {
	if strict {
		if !rxPrintable.MatchString(s) {
			return "", fmt.Errorf("%s: invalid printable", s)
		}
	} else {
		for _, r := range s {
			if !strconv.IsPrint(r) && r != '\n' && r != '\t' {
				return "", fmt.Errorf("%s: invalid printable", s)
			}
		}
	}
	return s, nil
}
