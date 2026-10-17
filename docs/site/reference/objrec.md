# 🤖 objrec

Use `objrec` to keep small JSON records in your app's object store instead of a database. It suits an app whose records are few and small and looked up by key, like a sign-in home or a directory. Such an app then has nothing to back up or migrate.

Every write is conditional. `Create` succeeds once per key. `Update` succeeds only if nobody has written since you read. When a write loses, read again and decide.

```go
store, err := objrec.S3FromEnv()
if err != nil {
	return err
}
if store == nil {
	store = objrec.Dir("records") // no bucket: local development
}

// An address can belong to one user only: the first create wins.
if _, err := objrec.CreateJSON(ctx, store, "emails/"+hash+".json", emailRecord{UserID: uid}); errors.Is(err, objrec.ErrExists) {
	// Someone already signed up with this address.
}

// Change a record without losing a concurrent change.
user, _, err := objrec.Mutate(ctx, store, "users/"+uid+".json", func(u User, exists bool) (User, error) {
	u.Epoch++
	return u, nil
})
```

Keys are lowercase letters, digits, `.`, `_` and `-`, in segments separated by `/`. A record is at most `MaxRecord` bytes. Bytes of any size belong in [blobs](/docs/reference/blobs).

## API

```go
type Version string

var (
	ErrExists   = errors.New("objrec: record exists")
	ErrChanged  = errors.New("objrec: record changed")
	ErrNotFound = errors.New("objrec: record not found")
	ErrKey      = errors.New("objrec: invalid key")
	ErrTooLarge = errors.New("objrec: record too large")
)

const MaxRecord = 256 << 10

type Store interface {
	Get(ctx context.Context, key string) ([]byte, Version, error)
	Create(ctx context.Context, key string, body []byte) (Version, error)
	Update(ctx context.Context, key string, body []byte, v Version) (Version, error)
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix, cursor string, limit int) (keys []string, next string, err error)
}

func ValidKey(key string) error
func ValidPrefix(prefix string) error

func GetJSON[T any](ctx context.Context, s Store, key string) (T, Version, error)
func CreateJSON[T any](ctx context.Context, s Store, key string, v T) (Version, error)
func UpdateJSON[T any](ctx context.Context, s Store, key string, v T, ver Version) (Version, error)
func Mutate[T any](ctx context.Context, s Store, key string, fn func(cur T, exists bool) (T, error)) (T, Version, error)

type DirStore struct { /* unexported fields */ }
func Dir(root string) *DirStore
func (d *DirStore) Get(ctx context.Context, key string) ([]byte, Version, error)
func (d *DirStore) Create(ctx context.Context, key string, body []byte) (Version, error)
func (d *DirStore) Update(ctx context.Context, key string, body []byte, v Version) (Version, error)
func (d *DirStore) Delete(ctx context.Context, key string) error
func (d *DirStore) List(ctx context.Context, prefix, cursor string, limit int) ([]string, string, error)

type S3Config struct {
	Bucket, Region, Endpoint, AccessKey, SecretKey, Prefix string
	HTTP *http.Client
}
type S3Store struct { /* unexported fields */ }
func NewS3(cfg S3Config) (*S3Store, error)
func S3FromEnv() (*S3Store, error)
func (s *S3Store) Get(ctx context.Context, key string) ([]byte, Version, error)
func (s *S3Store) Create(ctx context.Context, key string, body []byte) (Version, error)
func (s *S3Store) Update(ctx context.Context, key string, body []byte, v Version) (Version, error)
func (s *S3Store) Delete(ctx context.Context, key string) error
func (s *S3Store) List(ctx context.Context, prefix, cursor string, limit int) ([]string, string, error)
```

`Update` answers `ErrNotFound`, not `ErrChanged`, when the record was deleted, so a retry loop stops. `Mutate` may call `fn` more than once, so `fn` must have no side effects. `S3FromEnv` returns `nil, nil` when the app has no store. `Delete` is unconditional and succeeds on a missing key. `List` returns keys in order, and an empty `next` means the last page.

`DirStore` is for tests and local development. It is correct within one process only.
