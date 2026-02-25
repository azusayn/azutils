package auth

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pkg/errors"
)

type NameChecker struct {
	minLength  int
	maxLength  int
	allowPunct bool
	allowSpace bool
}

type NameCheckerOption func(*NameChecker)

// [lb, ub]
func WithLengthLimit(lb, ub int) NameCheckerOption {
	return func(nc *NameChecker) {
		nc.minLength = lb
		nc.maxLength = ub
	}
}

func WithAllowPunct() NameCheckerOption {
	return func(nc *NameChecker) {
		nc.allowPunct = true
	}
}

func WithAllowSpace() NameCheckerOption {
	return func(nc *NameChecker) {
		nc.allowSpace = true
	}
}

func NewNameChecker(opts ...NameCheckerOption) *NameChecker {
	nc := &NameChecker{
		minLength:  1,
		maxLength:  math.MaxInt,
		allowPunct: false,
		allowSpace: false,
	}
	for _, opt := range opts {
		opt(nc)
	}
	return nc
}

func (nc *NameChecker) BasicCheck(name string) error {
	// length.
	length := utf8.RuneCountInString(name)
	if length < nc.minLength || length > nc.maxLength {
		return fmt.Errorf("length must be between %d and %d", nc.minLength, nc.maxLength)
	}

	// disallow leading/trailing spaces.
	if name != strings.TrimSpace(name) {
		return errors.New("invalid trailing or leading spaces")
	}

	// check by characters.
	prevSpace := false
	for _, char := range name {
		if unicode.IsControl(char) {
			return errors.New("contains invalid control character")
		}

		if !nc.allowPunct && unicode.IsPunct(char) {
			return errors.New("contains invalid punct character")
		}

		if unicode.IsSpace(char) {
			if !nc.allowSpace {
				return errors.New("contains invalid space character")
			}
			if prevSpace {
				return errors.New("invalid consecutive spaces")
			}
			prevSpace = true
		} else {
			prevSpace = false
		}

	}
	return nil
}

func CheckUsername(username string) error {
	nc := NewNameChecker(WithLengthLimit(6, 15))
	if err := nc.BasicCheck(username); err != nil {
		return err
	}
	if matched, _ := regexp.MatchString(`^[a-zA-Z0-9_@]+$`, username); !matched {
		return errors.New("invalid username")
	}
	return nil
}
