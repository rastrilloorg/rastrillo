package provision

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// MaxSlug is the most of a slug a label keeps. With --private-storage the
// label becomes the instance's environment name, which is at most 32
// characters (cli.md:877-880): 25, a hyphen and the 6-character suffix make
// 32, so a long slug cannot fail the create.
const MaxSlug = 25

// ErrSlug is a slug that cannot start a DNS label.
var ErrSlug = errors.New("provision: a slug is lower-case letters, digits and inner hyphens")

var slugRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// suffix is lower-case RFC 4648 base32: the alphabet a DNS label can carry
// with no case to fold.
var suffix = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewLabel returns slug, cut to MaxSlug, followed by a hyphen and 6 random
// characters. The 30 random bits make a collision with an existing label
// a first-call event (spec §7.2 step 1), so a relabel is rare, and
// MaxRelabels bounds it.
func NewLabel(slug string) (string, error) {
	if !slugRe.MatchString(slug) {
		return "", fmt.Errorf("%w: %q", ErrSlug, slug)
	}
	if len(slug) > MaxSlug {
		// A cut can end on a hyphen, and "-" then the suffix's "-" would
		// make "--", which the slug never had.
		slug = strings.TrimRight(slug[:MaxSlug], "-")
	}
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("provision: label: %w", err)
	}
	return slug + "-" + strings.ToLower(suffix.EncodeToString(b[:])[:6]), nil
}
