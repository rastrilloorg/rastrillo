// Package objrec keeps small JSON records in an object store with
// create-once and compare-and-swap writes. It exists so a central app
// (a home, a hub, a directory) can hold its records with no database
// of its own: nothing to replicate, restore or migrate, and nothing
// lost when the process moves. Conditional writes are the whole
// concurrency model: Create wins exactly once, Update refuses a
// version somebody else has already replaced.
package objrec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

// Version identifies one write of a record: the S3 ETag, or a content
// hash on Dir. It is opaque; pass back exactly what Get or a write
// returned.
type Version string

var (
	// ErrExists: Create lost to an earlier or concurrent writer.
	ErrExists = errors.New("objrec: record exists")
	// ErrChanged: Update's version is no longer current. Re-read and decide.
	ErrChanged = errors.New("objrec: record changed")
	// ErrNotFound: no record at that key. Kept apart from ErrChanged so a
	// retry loop never spins against a record that was deleted.
	ErrNotFound = errors.New("objrec: record not found")
	// ErrKey: the key or prefix is outside the allowed shape.
	ErrKey = errors.New("objrec: invalid key")
	// ErrTooLarge: the record is over MaxRecord.
	ErrTooLarge = errors.New("objrec: record too large")
)

// MaxRecord caps one record at 256 KiB. Records are indexes and small
// documents; anything bigger belongs in blobs, and a runaway write
// would otherwise grow the bucket bill with nothing to notice it.
const MaxRecord = 256 << 10

// Store is one app's record store. Keys are relative to the store's
// own prefix.
type Store interface {
	Get(ctx context.Context, key string) ([]byte, Version, error)
	Create(ctx context.Context, key string, body []byte) (Version, error)
	Update(ctx context.Context, key string, body []byte, v Version) (Version, error)
	// Delete is unconditional and idempotent.
	Delete(ctx context.Context, key string) error
	// List returns keys under prefix in lexical order, at most limit of
	// them, and a cursor for the next page ("" when there is none).
	List(ctx context.Context, prefix, cursor string, limit int) (keys []string, next string, err error)
}

// Segments are lowercase letters, digits, '.', '_' and '-', starting
// with a letter or digit. That keeps every key a literal S3 path with
// no escaping, and makes "..", a leading "/" and empty segments
// unrepresentable rather than checked for.
var (
	keyRE    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*(/[a-z0-9][a-z0-9._-]*)*$`)
	prefixRE = regexp.MustCompile(`^([a-z0-9][a-z0-9._-]*/)*$`)
)

// ValidKey reports whether key may name a record.
func ValidKey(key string) error {
	if len(key) > 512 || !keyRE.MatchString(key) {
		return fmt.Errorf("%w: %q", ErrKey, key)
	}
	return nil
}

// ValidPrefix reports whether prefix may be listed: empty, or whole
// segments each ending in "/".
func ValidPrefix(prefix string) error {
	if len(prefix) > 512 || !prefixRE.MatchString(prefix) {
		return fmt.Errorf("%w: prefix %q", ErrKey, prefix)
	}
	return nil
}

func checkWrite(key string, body []byte) error {
	if err := ValidKey(key); err != nil {
		return err
	}
	if len(body) > MaxRecord {
		return ErrTooLarge
	}
	return nil
}

// GetJSON reads and decodes one record.
func GetJSON[T any](ctx context.Context, s Store, key string) (T, Version, error) {
	var v T
	body, ver, err := s.Get(ctx, key)
	if err != nil {
		return v, "", err
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return v, "", fmt.Errorf("objrec: decode %s: %w", key, err)
	}
	return v, ver, nil
}

// CreateJSON encodes and creates one record.
func CreateJSON[T any](ctx context.Context, s Store, key string, v T) (Version, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return s.Create(ctx, key, body)
}

// UpdateJSON encodes v and replaces the record if ver is still current.
func UpdateJSON[T any](ctx context.Context, s Store, key string, v T, ver Version) (Version, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return s.Update(ctx, key, body, ver)
}

// mutateAttempts bounds Mutate's retries. Contention on one record in a
// central app is a handful of writers; 64 lets a burst settle without
// letting a bug spin forever.
const mutateAttempts = 64

// Mutate applies fn to the current record (the zero value and
// exists=false when there is none) and writes the result, re-reading
// and retrying when another writer got there first. fn may run more
// than once, so it must not have side effects.
func Mutate[T any](ctx context.Context, s Store, key string, fn func(cur T, exists bool) (T, error)) (T, Version, error) {
	var zero T
	for i := 0; i < mutateAttempts; i++ {
		cur, ver, err := GetJSON[T](ctx, s, key)
		exists := err == nil
		if err != nil && !errors.Is(err, ErrNotFound) {
			return zero, "", err
		}
		next, err := fn(cur, exists)
		if err != nil {
			return zero, "", err
		}
		var nv Version
		if exists {
			nv, err = UpdateJSON(ctx, s, key, next, ver)
		} else {
			nv, err = CreateJSON(ctx, s, key, next)
		}
		switch {
		case err == nil:
			return next, nv, nil
		case errors.Is(err, ErrChanged), errors.Is(err, ErrExists), errors.Is(err, ErrNotFound):
			continue
		default:
			return zero, "", err
		}
	}
	return zero, "", fmt.Errorf("objrec: %s: gave up after %d attempts", key, mutateAttempts)
}
