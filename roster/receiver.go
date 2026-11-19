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
	json "github.com/go-json-experiment/json"
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
}

// Receiver applies signed snapshots to the app's Store.
type Receiver struct {
	verifier *Verifier
	store    Store
	db       *sql.DB
	roles    map[string]bool
	now      func() time.Time
	logger   *slog.Logger

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
	receiver.answer(response, receiver.apply(request.Context(), snapshot))
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

func (receiver *Receiver) apply(
	parent context.Context, snapshot Snapshot,
) error {
	for _, member := range snapshot.Members {
		if !receiver.roles[member.Role] {
			return ErrUnknownRole
		}
	}
	receiver.applying.Lock()
	defer receiver.applying.Unlock()
	for range maxAttempts {
		err := receiver.attempt(parent, snapshot)
		if !errors.Is(err, errLeaseLost) {
			return err
		}
	}
	return errLeaseHeld
}

// attempt applies snapshot under a fresh lease. When snapshot is older than
// the recorded one, it applies the recorded one instead: that repairs any
// write a stale apply landed after the newer roster, and answers 409.
func (receiver *Receiver) attempt(
	parent context.Context, snapshot Snapshot,
) error {
	// Detached from the request so a dropped connection cannot stop an
	// apply halfway; the timeout bounds it instead.
	ctx, cancel := context.WithTimeout(
		context.WithoutCancel(parent), applyTimeout)
	defer cancel()
	lease, err := receiver.takeLease(ctx)
	if err != nil {
		return err
	}
	defer receiver.releaseLease(parent, lease)
	newest, err := receiver.lastApplied(ctx)
	if err != nil {
		return err
	}
	target, outcome := snapshot, error(nil)
	if snapshot.Sequence < newest.Sequence {
		target, outcome = newest, errSequenceMoved
	}
	err = receiver.syncMembers(ctx, target.Members)
	if err != nil {
		// Even a failed sync may have written over a newer apply.
		if lost := receiver.confirmLease(parent, lease); lost != nil {
			return lost
		}
		return err
	}
	err = receiver.record(parent, lease, target)
	if err != nil {
		return err
	}
	return outcome
}

// record writes the snapshot last, so a failure before it leaves the old
// sequence and the root's retry applies the snapshot again. It writes only
// while this apply still holds its lease: otherwise another apply may have
// run alongside this one's Store writes, and the caller must repair.
func (receiver *Receiver) record(
	parent context.Context, lease int64, snapshot Snapshot,
) error {
	members, err := json.Marshal(snapshot.Members)
	if err != nil {
		return err
	}
	ctx, cancel := bookkeeping(parent)
	defer cancel()
	result, err := receiver.db.ExecContext(ctx,
		`UPDATE roster_state SET sequence = ?, state = ?, members = ?
		 WHERE id = 1 AND lease_until = ? AND sequence <= ?`,
		snapshot.Sequence, string(snapshot.State), string(members),
		lease, snapshot.Sequence)
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

// confirmLease reports errLeaseLost once another process has taken the
// lease. A lapsed lease that no one took is still ours: nothing else ran.
func (receiver *Receiver) confirmLease(
	parent context.Context, lease int64,
) error {
	ctx, cancel := bookkeeping(parent)
	defer cancel()
	var until int64
	err := receiver.db.QueryRowContext(ctx,
		`SELECT lease_until FROM roster_state WHERE id = 1`).Scan(&until)
	if err != nil {
		return err
	}
	if until != lease {
		return errLeaseLost
	}
	return nil
}

// bookkeeping is the context for lease and state writes after the Store's.
// It is not the apply's, which a slow Store may have used up, and without
// these writes a lost lease would go unnoticed.
func bookkeeping(parent context.Context) (context.Context, func()) {
	return context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
}

func (receiver *Receiver) syncMembers(
	ctx context.Context, members []Member,
) error {
	current, err := receiver.store.Members(ctx)
	if err != nil {
		return errStoreMembers
	}
	have := make(map[string]Member, len(current))
	for _, member := range current {
		have[member.Subject] = member
	}
	want := make(map[string]bool, len(members))
	for _, member := range members {
		want[member.Subject] = true
		if existing, found := have[member.Subject]; found &&
			existing == member {
			continue
		}
		if err := receiver.store.Put(ctx, member); err != nil {
			return errStorePut
		}
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

// takeLease serialises applies across processes sharing the database. It
// returns the lease's expiry, which also names it: a process that takes a
// lapsed lease writes a later one.
func (receiver *Receiver) takeLease(ctx context.Context) (int64, error) {
	_, err := receiver.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO roster_state
		   (id, sequence, state, lease_until) VALUES (1, 0, 'active', 0)`)
	if err != nil {
		return 0, err
	}
	now := receiver.now()
	lease := now.Add(leaseLife).Unix()
	result, err := receiver.db.ExecContext(ctx,
		`UPDATE roster_state SET lease_until = ?
		 WHERE id = 1 AND lease_until < ?`,
		lease, now.Unix())
	if err != nil {
		return 0, err
	}
	taken, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if taken == 0 {
		return 0, errLeaseHeld
	}
	return lease, nil
}

// releaseLease clears only our own value, because a lapsed lease may
// already belong to another process.
func (receiver *Receiver) releaseLease(parent context.Context, lease int64) {
	ctx, cancel := bookkeeping(parent)
	defer cancel()
	_, err := receiver.db.ExecContext(ctx,
		`UPDATE roster_state SET lease_until = 0
		 WHERE id = 1 AND lease_until = ?`, lease)
	if err != nil {
		receiver.logger.Error("roster lease not released", "error", err)
	}
}

// lastApplied is the recorded snapshot's sequence, state and members, or
// sequence 0 and no members before the first.
func (receiver *Receiver) lastApplied(ctx context.Context) (Snapshot, error) {
	var sequence int64
	var state, members string
	err := receiver.db.QueryRowContext(ctx,
		`SELECT sequence, state, members FROM roster_state WHERE id = 1`,
	).Scan(&sequence, &state, &members)
	if errors.Is(err, sql.ErrNoRows) {
		return Snapshot{State: Active, Members: []Member{}}, nil
	}
	if err != nil {
		return Snapshot{}, err
	}
	applied := Snapshot{Sequence: sequence, State: State(state)}
	err = json.Unmarshal([]byte(members), &applied.Members)
	if err != nil {
		return Snapshot{}, err
	}
	return applied, nil
}

// Suspended reports whether the last applied snapshot suspended the
// instance. Before any snapshot it is false.
func (receiver *Receiver) Suspended(ctx context.Context) (bool, error) {
	applied, err := receiver.lastApplied(ctx)
	return applied.State == Suspended, err
}
