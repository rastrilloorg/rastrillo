package objrec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// DirStore keeps records as files under a root directory, for tests and
// `rastrillo dev`. It gives the same answers as S3 within ONE process:
// a mutex makes each conditional write atomic. Two processes sharing a
// root are not supported, and nothing on the platform runs it.
type DirStore struct {
	root string
	mu   sync.Mutex
}

// Dir returns a store rooted at root, created on first write.
func Dir(root string) *DirStore { return &DirStore{root: root} }

// A content hash is the version, as an S3 single-part ETag is: two
// writes of identical bytes share a version, which can only let a
// no-op through.
func version(body []byte) Version {
	sum := sha256.Sum256(body)
	return Version(hex.EncodeToString(sum[:]))
}

func (d *DirStore) path(key string) string {
	return filepath.Join(d.root, filepath.FromSlash(key))
}

func (d *DirStore) read(key string) ([]byte, error) {
	b, err := os.ReadFile(d.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	return b, err
}

// write lands the bytes whole or not at all (temp file, then rename),
// so a crash never leaves half a record for the next reader.
func (d *DirStore) write(key string, body []byte) error {
	p := d.path(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".objrec-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

func (d *DirStore) Get(_ context.Context, key string) ([]byte, Version, error) {
	if err := ValidKey(key); err != nil {
		return nil, "", err
	}
	b, err := d.read(key)
	if err != nil {
		return nil, "", err
	}
	return b, version(b), nil
}

func (d *DirStore) Create(_ context.Context, key string, body []byte) (Version, error) {
	if err := checkWrite(key, body); err != nil {
		return "", err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, err := d.read(key); err == nil {
		return "", ErrExists
	} else if !errors.Is(err, ErrNotFound) {
		return "", err
	}
	if err := d.write(key, body); err != nil {
		return "", err
	}
	return version(body), nil
}

func (d *DirStore) Update(_ context.Context, key string, body []byte, v Version) (Version, error) {
	if err := checkWrite(key, body); err != nil {
		return "", err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	cur, err := d.read(key)
	if err != nil {
		return "", err
	}
	if version(cur) != v {
		return "", ErrChanged
	}
	if err := d.write(key, body); err != nil {
		return "", err
	}
	return version(body), nil
}

func (d *DirStore) Delete(_ context.Context, key string) error {
	if err := ValidKey(key); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	err := os.Remove(d.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// List walks the whole root. Fine for tests and dev; S3 is the
// backend that has to scale.
func (d *DirStore) List(_ context.Context, prefix, cursor string, limit int) ([]string, string, error) {
	if err := ValidPrefix(prefix); err != nil {
		return nil, "", err
	}
	if limit <= 0 {
		limit = 1000
	}
	var all []string
	err := filepath.WalkDir(d.root, func(p string, e fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if e.IsDir() || strings.HasPrefix(e.Name(), ".objrec-") {
			return nil
		}
		rel, err := filepath.Rel(d.root, p)
		if err != nil {
			return err
		}
		k := filepath.ToSlash(rel)
		if strings.HasPrefix(k, prefix) && k > cursor {
			all = append(all, k)
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	sort.Strings(all)
	if len(all) <= limit {
		return all, "", nil
	}
	return all[:limit], all[limit-1], nil
}
