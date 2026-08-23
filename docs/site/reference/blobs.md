# 🤖 blobs

`github.com/carlosframework/rastrillo/blobs`

Content-addressed bytes. Rows hold metadata — a `Ref`: hash, size,
content type — while the bytes live in a `Store` keyed by their SHA-256.

Content addressing keeps this simple. Write-once means no state machine
and no lease, and re-uploading the same bytes is idempotent by
construction.

## Store

```go
type Store interface {
	Put(ctx context.Context, r io.Reader, contentType string) (Ref, error)
	Get(ctx context.Context, hash string) (io.ReadCloser, error)
	Delete(ctx context.Context, hash string) error
}
```

There are three backends behind it.

### S3

```go
func S3FromEnv() (*S3, error)
func NewS3(cfg S3Config) (*S3, error)
```

The platform's object-storage primitive: your own bucket, with
credentials delivered as `CARLOS_STORE_*` environment variables. This is
where blobs belong.

`S3.Get`, `S3.Put` and `S3.Delete` are the store operations.
`S3.PresignGet` and `S3.PresignPut` mint presigned URLs locally, since
the platform ships no signing service, so a browser can upload and
download without your app proxying the bytes.

The SigV4 signer is hand-rolled, with no AWS SDK dependency, and pinned
against the official AWS test vectors.

### Dir

```go
func Dir(root string) (Store, error)
```

Content-addressed files under a directory. For development and tests.

### Inline

```go
func Inline(db *sql.DB) Store
```

A SQLite table, for small blobs only.

`InlineMax` is 4 KiB. It is a working rule: stated everywhere, enforced
nowhere. Anything larger belongs in the object store, where bytes do not
ride every database page the row touches, do not bloat the WAL stream
the platform replicates, and can be served by presigned URL.

`blobs.Schema` is the migration set for the inline table.

## Sealed

```go
func Sealed(store Store, key []byte) Store
```

Wraps any backend so bytes are sealed with
[`crypto`](/docs/reference/crypto) before they leave the process.

Two costs come with that, and both are inherent. The address becomes the
ciphertext's hash, so dedup happens per key instead of globally: two
users storing identical bytes store them twice.

## Ref and ErrNotFound

```go
type Ref struct {
	Hash        string // hex SHA-256 of the stored bytes
	Size        int64
	ContentType string
}

var ErrNotFound = errors.New("rastrillo/blobs: no such blob")
```

A `Ref` is what your row stores — the hash is the address, and it is the
hex SHA-256 of the stored bytes.

The interface streams rather than passing byte slices: `Put` takes an
`io.Reader` and `Get` returns an `io.ReadCloser`. That is what lets a
large upload go to the object store without the whole blob sitting in
the app's memory first — and it means **you must close what `Get`
returns.**

`ErrNotFound` is what every backend returns for a missing hash, so a
caller can handle a miss without knowing which backend it is talking
to.
