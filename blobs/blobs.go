// Package blobs stores content-addressed bytes — design doc §5's blob
// layer, built on what the platform actually shipped: rows hold
// metadata (a Ref: hash, size, content type) while bytes live in a
// Store, keyed by their SHA-256. Content addressing is what keeps this
// simple — write-once means no state machine, no lease, and a re-upload
// of the same bytes is idempotent by construction (messenger's
// blobmirror insight: "the blobs table IS the journal").
//
// Three backends, one interface:
//
//   - S3 — the platform's object-storage primitive: the app's own
//     bucket, credentials delivered as CARLOS_STORE_* env vars
//     (S3FromEnv), a hand-rolled SigV4 signer (no SDK), and presigned
//     GET/PUT URLs minted locally, because the platform deliberately
//     ships no signing service. This is where blobs belong.
//   - Dir — content-addressed files under a directory: dev and tests.
//   - Inline — a SQLite table, for small blobs only. The working rule,
//     stated everywhere and enforced nowhere: anything over InlineMax
//     (4 KiB) belongs in the object store, where bytes don't ride every
//     database page the row touches, don't bloat the WAL stream the
//     platform replicates, and can be served by presigned URL without
//     the app proxying them.
//
// E2EE apps wrap any backend in Sealed: bytes are sealed with
// rastrillo/crypto before they leave the process (§5), at the honest
// cost that the address is the ciphertext's hash and dedup happens per
// key.
package blobs

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/carlosframework/rastrillo/crypto"
	"github.com/carlosframework/rastrillo/migrate"
)

// InlineMax is the guidance threshold: blobs larger than this belong in
// the object store, not the database. A recommendation the docs and
// checks repeat, deliberately not a hard limit.
const InlineMax = 4 << 10

// Ref is what an app's row stores about a blob: everything except the
// bytes.
type Ref struct {
	Hash        string `json:"hash"` // hex SHA-256 of the stored bytes
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}

// Store is the byte store. Implementations are content-addressed: Put
// hashes what it reads and stores under that key, so storing the same
// bytes twice yields the same Ref and one stored object.
type Store interface {
	Put(ctx context.Context, r io.Reader, contentType string) (Ref, error)
	Get(ctx context.Context, hash string) (io.ReadCloser, error)
	Delete(ctx context.Context, hash string) error
}

// ErrNotFound is a hash the store does not hold.
var ErrNotFound = errors.New("rastrillo/blobs: no such blob")

//go:embed migrations/*.sql
var migrationFS embed.FS

// Schema is Inline's migration set, applied with migrate.Apply
// alongside the app's own. It replaces the exported Migrations
// []string: the ledger records what ran, so these statements are no
// longer re-executed on every boot.
var Schema = migrate.MustFromFS(migrationFS, "blobs")

func hashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func checkHash(hash string) error {
	if len(hash) != 64 {
		return fmt.Errorf("rastrillo/blobs: %q is not a hex SHA-256", hash)
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return fmt.Errorf("rastrillo/blobs: %q is not a hex SHA-256", hash)
	}
	return nil
}

// --- Inline: SQLite-backed, small blobs only ---

type inlineStore struct{ db *sql.DB }

// Inline stores blobs in the app database. For blobs at or under
// InlineMax; see the package doc for why bigger ones belong in the
// object store.
func Inline(db *sql.DB) Store { return &inlineStore{db} }

func (s *inlineStore) Put(ctx context.Context, r io.Reader, contentType string) (Ref, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return Ref{}, err
	}
	ref := Ref{Hash: hashOf(data), Size: int64(len(data)), ContentType: contentType}
	_, err = s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO blobs (hash, content_type, size, data) VALUES (?, ?, ?, ?)`,
		ref.Hash, ref.ContentType, ref.Size, data)
	return ref, err
}

func (s *inlineStore) Get(ctx context.Context, hash string) (io.ReadCloser, error) {
	if err := checkHash(hash); err != nil {
		return nil, err
	}
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT data FROM blobs WHERE hash = ?`, hash).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader(string(data))), nil
}

func (s *inlineStore) Delete(ctx context.Context, hash string) error {
	if err := checkHash(hash); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM blobs WHERE hash = ?`, hash)
	return err
}

// --- Dir: content-addressed files ---

type dirStore struct{ root string }

// Dir stores blobs as files under root — dev and tests. Writes are
// temp-file-and-rename so a reader never sees half a blob.
func Dir(root string) (Store, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &dirStore{root}, nil
}

func (s *dirStore) path(hash string) string { return filepath.Join(s.root, hash) }

func (s *dirStore) Put(_ context.Context, r io.Reader, contentType string) (Ref, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return Ref{}, err
	}
	ref := Ref{Hash: hashOf(data), Size: int64(len(data)), ContentType: contentType}
	path := s.path(ref.Hash)
	if _, err := os.Stat(path); err == nil {
		return ref, nil // content-addressed: already stored
	}
	tmp, err := os.CreateTemp(s.root, ".put-*")
	if err != nil {
		return Ref{}, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return Ref{}, err
	}
	if err := tmp.Close(); err != nil {
		return Ref{}, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return Ref{}, err
	}
	return ref, nil
}

func (s *dirStore) Get(_ context.Context, hash string) (io.ReadCloser, error) {
	if err := checkHash(hash); err != nil {
		return nil, err
	}
	f, err := os.Open(s.path(hash))
	if os.IsNotExist(err) {
		return nil, ErrNotFound
	}
	return f, err
}

func (s *dirStore) Delete(_ context.Context, hash string) error {
	if err := checkHash(hash); err != nil {
		return err
	}
	if err := os.Remove(s.path(hash)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// --- Sealed: encrypt-before-store over any backend ---

type sealedStore struct {
	next Store
	key  []byte
}

// Sealed wraps a Store so bytes are sealed (crypto.SealSym, AES-256-GCM
// under key) before Put hands them on, and opened after Get. The
// address becomes the ciphertext's hash — identical plaintexts stored
// under different IVs are different blobs — and Ref.Size is the sealed
// size; both are the honest cost of the server never holding plaintext.
// ContentType passes through as visible metadata: put "application/
// octet-stream" there if the type itself is sensitive.
func Sealed(next Store, key []byte) Store { return &sealedStore{next, key} }

func (s *sealedStore) Put(ctx context.Context, r io.Reader, contentType string) (Ref, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return Ref{}, err
	}
	sealed, err := crypto.SealSym(s.key, data)
	if err != nil {
		return Ref{}, err
	}
	return s.next.Put(ctx, strings.NewReader(string(sealed)), contentType)
}

func (s *sealedStore) Get(ctx context.Context, hash string) (io.ReadCloser, error) {
	rc, err := s.next.Get(ctx, hash)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	sealed, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	plain, err := crypto.OpenSym(s.key, sealed)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader(string(plain))), nil
}

func (s *sealedStore) Delete(ctx context.Context, hash string) error {
	return s.next.Delete(ctx, hash)
}
