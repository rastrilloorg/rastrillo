// Package privacy holds what export and erasure share: who a person is
// to an app, in every identifier their rows might carry.
package privacy

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"amadan.net/rastrillo/rastrillo/migrate"
)

// ErrUnidentifiable is what an Identifier answers when the app maps
// addresses to subjects one way only and gave no way back.
var ErrUnidentifiable = errors.New(
	"privacy: this app cannot list a person's addresses")

// Identity is every identifier that stands for one person.
type Identity struct {
	Subject string
	// Addresses are trimmed, sorted and deduplicated, with the domain
	// lower-cased and the local part's case kept, as signin.SplitAddress
	// does for a verified address. Empty when the app keeps no readable
	// address.
	Addresses []string
	// UserID is Subject when it is a positive decimal integer, else 0.
	UserID int64
}

// Identifier resolves a subject to its Identity.
type Identifier interface {
	Identify(ctx context.Context, subject string) (Identity, error)
}

// NewIdentity builds the Identity of subject with the given addresses,
// normalising and sorting them so Matches and callers can rely on it.
func NewIdentity(subject string, addresses []string) Identity {
	var clean []string
	for _, address := range addresses {
		address = normalise(address)
		if address != "" {
			clean = append(clean, address)
		}
	}
	slices.Sort(clean)
	return Identity{
		Subject:   subject,
		Addresses: slices.Compact(clean),
		UserID:    userID(subject),
	}
}

// Matches reports whether value, found in a column holding kind,
// stands for this person. An address is compared in the form Addresses
// keep. Free text never matches: a sentence is not an identifier, so
// HoldsFreeText is always false. An identity with no Subject matches
// nothing, so a caller's bug cannot claim every row with an empty column.
func (who Identity) Matches(kind migrate.Holds, value string) bool {
	if who.Subject == "" {
		return false
	}
	switch kind {
	case migrate.HoldsSubject:
		return value == who.Subject
	case migrate.HoldsAddress:
		return slices.Contains(who.Addresses, normalise(value))
	case migrate.HoldsUserID:
		return who.UserID != 0 && value == strconv.FormatInt(who.UserID, 10)
	}
	return false
}

// normalise trims and lower-cases the domain after the first "@", the
// rule signin.SplitAddress applies. Local parts stay case-sensitive
// because some mail systems treat them so.
func normalise(address string) string {
	address = strings.TrimSpace(address)
	local, domain, ok := strings.Cut(address, "@")
	if !ok {
		return address
	}
	return local + "@" + strings.ToLower(domain)
}

// userID accepts only the canonical decimal form, so "042" and "+3"
// cannot alias another person's id.
func userID(subject string) int64 {
	id, err := strconv.ParseInt(subject, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != subject {
		return 0
	}
	return id
}
