package roster

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/migrate"
)

// Path is where the root app posts snapshots.
const Path = "/_carlos/roster"

//go:embed migrations/*.sql
var migrationFS embed.FS

// Schema holds the one row that remembers the last applied snapshot. Apply
// it with migrate.Apply alongside the app's own migrations.
var Schema = migrate.MustFromFS(migrationFS, "roster")

// Store is the app's own roster. The receiver only reads and changes it
// through this interface, so each app keeps its own tables. Every method
// should honour ctx: the receiver bounds an apply to 25 seconds through it,
// and a method that ignores it can keep writing after the lease has lapsed.
// Such a stale write can land on top of a newer roster. The receiver then
// repairs it by applying the newest roster again before it answers.
type Store interface {
	Members(ctx context.Context) ([]Member, error)
	Put(ctx context.Context, member Member) error
	Remove(ctx context.Context, subject string) error
}

type Config struct {
	Verifier *Verifier
	Store    Store
	DB       *sql.DB
	Roles    []string // the app's roles, highest first
	Now      func() time.Time
	Logger   *slog.Logger

	// OnOrigin runs after a snapshot that changes the origin has been
	// applied and answered (origin.go).
	OnOrigin func(previous, next string)

	// RecordRolesOnly keeps members' emails and names out of the
	// receiver's own row, which then holds subjects, roles and grant
	// IDs only, and keeps the grant IDs through a repair (record.go).
	// The Store still gets every field.
	RecordRolesOnly bool
}

// Receiver applies signed snapshots to the app's Store.
type Receiver struct {
	verifier *Verifier
	store    Store
	db       *sql.DB
	roles    map[string]bool
	now      func() time.Time
	logger   *slog.Logger
	onOrigin func(previous, next string)

	recordRolesOnly bool

	// One apply at a time in this process. A lease row serialises other
	// processes, and an apply that lost its lease repairs what it wrote.
	applying sync.Mutex
}

var (
	errSequenceMoved = errors.New("roster: sequence moved")
	errLeaseHeld     = errors.New("roster: another apply is running")
	errLeaseLost     = errors.New("roster: lease taken by another apply")

	// A Store's own errors can quote a member's email or name, so only
	// which operation failed is kept.
	errStorePut     = errors.New("roster: store put failed")
	errStoreRemove  = errors.New("roster: store remove failed")
	errStoreMembers = errors.New("roster: store members failed")
)

// The apply timeout is shorter than the lease so that a Store honouring its
// context finishes before the lease lapses. One that ignores it can overrun,
// and the apply then repairs the roster if another took the lease meanwhile.
const (
	leaseLife    = 30 * time.Second
	applyTimeout = 25 * time.Second
)

func New(config Config) (*Receiver, error) {
	if config.Verifier == nil || config.Store == nil || config.DB == nil ||
		len(config.Roles) == 0 {
		return nil, ErrConfig
	}
	roles := make(map[string]bool, len(config.Roles))
	for _, role := range config.Roles {
		if roles[role] {
			return nil, ErrConfig
		}
		roles[role] = true
	}
	receiver := &Receiver{
		verifier: config.Verifier,
		store:    config.Store,
		db:       config.DB,
		roles:    roles,
		now:      config.Now,
		logger:   config.Logger,
		onOrigin: config.OnOrigin,

		recordRolesOnly: config.RecordRolesOnly,
	}
	if receiver.now == nil {
		receiver.now = time.Now
	}
	if receiver.logger == nil {
		receiver.logger = slog.Default()
	}
	return receiver, nil
}

func (receiver *Receiver) ServeHTTP(
	response http.ResponseWriter, request *http.Request,
) {
	// The root posts to the audience origin, which is the instance's own
	// host even for a hub tenant, so nothing else is answered.
	if !basepath.SameHost(request.Host, receiver.verifier.audience) {
		http.NotFound(response, request)
		return
	}
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limited := http.MaxBytesReader(response, request.Body, maxToken)
	body, err := io.ReadAll(limited)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(response, "too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(response, "bad request", http.StatusBadRequest)
		return
	}
	snapshot, err := receiver.verifier.Verify(string(body), receiver.now())
	if err != nil {
		receiver.logger.Warn("roster snapshot refused", "error", err)
		http.Error(response, "forbidden", http.StatusForbidden)
		return
	}
	previous, err := receiver.apply(request.Context(), snapshot)
	receiver.answer(response, err)
	if err == nil {
		receiver.announce(response, previous, snapshot.Origin)
	}
}

func (receiver *Receiver) answer(response http.ResponseWriter, err error) {
	switch {
	case err == nil:
		response.WriteHeader(http.StatusNoContent)
	case errors.Is(err, errLeaseHeld):
		response.Header().Set("Retry-After", "1")
		http.Error(response, "busy", http.StatusServiceUnavailable)
	case errors.Is(err, errSequenceMoved):
		http.Error(response, "older sequence", http.StatusConflict)
	case errors.Is(err, ErrUnknownRole):
		http.Error(response, "unknown role", http.StatusUnprocessableEntity)
	default:
		// Store failures arrive as a classification only. Database errors
		// come from the receiver's own statements and are logged as is.
		receiver.logger.Error("roster snapshot not applied", "error", err)
		http.Error(response, "internal error", http.StatusInternalServerError)
	}
}

// ErrUnknownRole means a member has a role the app does not define.
var ErrUnknownRole = errors.New("roster: unknown role")

// maxAttempts bounds the repairs after a lost lease. Losing it every time
// means other processes are busy applying, so the root's retry does better.
const maxAttempts = 3

// apply returns the origin recorded before snapshot, so the caller can
// tell whether it changed.
func (receiver *Receiver) apply(
	parent context.Context, snapshot Snapshot,
) (previous string, err error) {
	for _, member := range snapshot.Members {
		if !receiver.roles[member.Role] {
			return "", ErrUnknownRole
		}
	}
	receiver.applying.Lock()
	defer receiver.applying.Unlock()
	for range maxAttempts {
		previous, err = receiver.attempt(parent, snapshot)
		if !errors.Is(err, errLeaseLost) {
			return previous, err
		}
	}
	return "", errLeaseHeld
}

// attempt applies snapshot under a fresh lease. When snapshot is older than
// the recorded one, it applies the recorded one instead: that repairs any
// write a stale apply landed after the newer roster, and answers 409.
func (receiver *Receiver) attempt(
	parent context.Context, snapshot Snapshot,
) (previous string, err error) {
	// Detached from the request so a dropped connection cannot stop an
	// apply halfway; the timeout bounds it instead.
	ctx, cancel := context.WithTimeout(
		context.WithoutCancel(parent), applyTimeout)
	defer cancel()
	lease, err := receiver.takeLease(ctx)
	if err != nil {
		return "", err
	}
	defer receiver.releaseLease(parent, lease)
	newest, err := receiver.lastApplied(ctx)
	if err != nil {
		return "", err
	}
	target, outcome, keepContact := snapshot, error(nil), false
	if snapshot.Sequence < newest.Sequence {
		target, outcome = newest.Snapshot, errSequenceMoved
		keepContact = newest.contactless
	}
	target.Members, err = receiver.syncMembers(
		ctx, target.Members, keepContact)
	if err != nil {
		// Even a failed sync may have written over a newer apply.
		if lost := receiver.confirmLease(parent, lease); lost != nil {
			return "", lost
		}
		return "", err
	}
	// A repair from a roles-only row keeps it roles-only, whatever this
	// receiver's setting: the contact syncMembers filled in is the Store's,
	// not the root's, and recording it would make it look signed.
	rolesOnly := receiver.recordRolesOnly || keepContact
	err = receiver.record(parent, lease, target, rolesOnly)
	if err != nil {
		return "", err
	}
	return newest.Origin, outcome
}

// record writes the snapshot last, so a failure before it leaves the old
// sequence and the root's retry applies the snapshot again. It writes only
// while this apply still holds its lease: otherwise another apply may have
// run alongside this one's Store writes, and the caller must repair.
func (receiver *Receiver) record(
	parent context.Context, lease int64, snapshot Snapshot, rolesOnly bool,
) error {
	members, err := recordedMembers(snapshot.Members, rolesOnly)
	if err != nil {
		return err
	}
	ctx, cancel := bookkeeping(parent)
	defer cancel()
	result, err := receiver.db.ExecContext(ctx,
		`UPDATE roster_state
		 SET sequence = ?, state = ?, members = ?, origin = ?
		 WHERE id = 1 AND lease_until = ? AND sequence <= ?`,
		snapshot.Sequence, string(snapshot.State), string(members),
		snapshot.Origin, lease, snapshot.Sequence)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 1 {
		return nil
	}
	err = receiver.confirmLease(parent, lease)
	if err != nil {
		return err
	}
	return errSequenceMoved
}

// syncMembers makes the Store hold members, and returns them as written.
// keepContact keeps the email and name the Store has for each member,
// because a roles-only record has none to restore.
func (receiver *Receiver) syncMembers(
	ctx context.Context, members []Member, keepContact bool,
) ([]Member, error) {
	current, err := receiver.store.Members(ctx)
	if err != nil {
		return nil, errStoreMembers
	}
	have := make(map[string]Member, len(current))
	for _, member := range current {
		have[member.Subject] = member
	}
	written := make([]Member, 0, len(members))
	for _, member := range members {
		existing, found := have[member.Subject]
		if found && keepContact {
			member.Email, member.Name = existing.Email, existing.Name
		}
		written = append(written, member)
		if found && sameMember(existing, member) {
			continue
		}
		if err := receiver.store.Put(ctx, member); err != nil {
			return nil, errStorePut
		}
	}
	return written, receiver.removeMissing(ctx, have, written)
}

func (receiver *Receiver) removeMissing(
	ctx context.Context, have map[string]Member, members []Member,
) error {
	want := make(map[string]bool, len(members))
	for _, member := range members {
		want[member.Subject] = true
	}
	for subject := range have {
		if want[subject] {
			continue
		}
		if err := receiver.store.Remove(ctx, subject); err != nil {
			return errStoreRemove
		}
	}
	return nil
}

// Suspended reports whether the last applied snapshot suspended the
// instance. Before any snapshot it is false.
func (receiver *Receiver) Suspended(ctx context.Context) (bool, error) {
	applied, err := receiver.lastApplied(ctx)
	return applied.State == Suspended, err
}

// sameMember leaves Grants out: the Store keeps none, so comparing them
// would rewrite every member on each snapshot.
func sameMember(existing, member Member) bool {
	return existing.Subject == member.Subject &&
		existing.Email == member.Email && existing.Name == member.Name &&
		existing.Role == member.Role
}
