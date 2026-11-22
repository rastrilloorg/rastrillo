package roster_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/dbtest"
	"amadan.net/rastrillo/rastrillo/roster"
)

const (
	rootOrigin     = "https://root.example.com"
	instanceOrigin = "https://acme.example.com"
)

var schema = dbtest.FromSet(roster.Schema)

func TestMain(main *testing.M) {
	code := main.Run()
	schema.Remove()
	os.Exit(code)
}

var epoch = time.Unix(1700000000, 0)

// fakeStore records the writes the receiver makes.
type fakeStore struct {
	members map[string]roster.Member
	puts    []string
	removes []string
	putErr  error
	onPut   func()
	// removeErr and membersErr fail Remove and Members, as putErr does Put.
	removeErr, membersErr error
	// onMembers runs before each Members call, as onPut does before Put.
	onMembers func()
}

func newFakeStore(members ...roster.Member) *fakeStore {
	store := &fakeStore{members: map[string]roster.Member{}}
	for _, member := range members {
		store.members[member.Subject] = member
	}
	return store
}

func (store *fakeStore) Members(context.Context) ([]roster.Member, error) {
	if store.onMembers != nil {
		store.onMembers()
	}
	if store.membersErr != nil {
		return nil, store.membersErr
	}
	var members []roster.Member
	for _, member := range store.members {
		members = append(members, member)
	}
	return members, nil
}

func (store *fakeStore) Put(_ context.Context, member roster.Member) error {
	if store.onPut != nil {
		store.onPut()
	}
	if store.putErr != nil {
		return store.putErr
	}
	store.puts = append(store.puts, member.Subject)
	store.members[member.Subject] = member
	return nil
}

func (store *fakeStore) Remove(_ context.Context, subject string) error {
	if store.removeErr != nil {
		return store.removeErr
	}
	store.removes = append(store.removes, subject)
	delete(store.members, subject)
	return nil
}

type fixture struct {
	key      *crypto.Keypair
	verifier *roster.Verifier
	store    *fakeStore
	db       *sql.DB
	receiver *roster.Receiver
	now      time.Time
	log      *bytes.Buffer
	// onOrigin, when set, is what every process's OnOrigin calls.
	onOrigin func(previous, next string)
	// rolesOnly is RecordRolesOnly for processes made after it is set.
	rolesOnly bool
}

func newFixture(t *testing.T, members ...roster.Member) *fixture {
	t.Helper()
	key, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := roster.NewVerifier(rootOrigin, instanceOrigin,
		map[string][]byte{"key_1": key.SignPub()})
	if err != nil {
		t.Fatal(err)
	}
	fix := &fixture{key: key, verifier: verifier,
		store: newFakeStore(members...), db: schema.Open(t), now: epoch,
		log: &bytes.Buffer{}}
	fix.receiver = fix.process(t)
	return fix
}

// process is a receiver sharing this fixture's database, Store and clock,
// as another process of the same app would.
func (fix *fixture) process(t *testing.T) *roster.Receiver {
	t.Helper()
	receiver, err := roster.New(roster.Config{
		Verifier: fix.verifier, Store: fix.store, DB: fix.db,
		Roles:           []string{"owner", "editor", "member"},
		RecordRolesOnly: fix.rolesOnly,
		Now:             func() time.Time { return fix.now },
		OnOrigin: func(previous, next string) {
			if fix.onOrigin != nil {
				fix.onOrigin(previous, next)
			}
		},
		Logger: slog.New(slog.NewTextHandler(
			io.MultiWriter(fix.log, testWriter{t}), &slog.HandlerOptions{})),
	})
	if err != nil {
		t.Fatal(err)
	}
	return receiver
}

type testWriter struct{ t *testing.T }

func (writer testWriter) Write(text []byte) (int, error) {
	writer.t.Log(strings.TrimSpace(string(text)))
	return len(text), nil
}

func (fix *fixture) token(
	t *testing.T, sequence int64, state roster.State,
	members ...roster.Member,
) string {
	t.Helper()
	return fix.tokenFor(t, instanceOrigin, sequence, state, members...)
}

func (fix *fixture) tokenFor(
	t *testing.T, audience string, sequence int64, state roster.State,
	members ...roster.Member,
) string {
	t.Helper()
	token, err := roster.Sign(fix.key, roster.Snapshot{
		Version: 1, Issuer: rootOrigin, Audience: audience,
		KeyID: "key_1", Sequence: sequence, IssuedAt: epoch.Unix(),
		ExpiresAt: epoch.Unix() + 60, State: state, Members: members,
	})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func (fix *fixture) post(token string) int {
	return fix.postTo(fix.receiver, token)
}

func (fix *fixture) postTo(receiver *roster.Receiver, token string) int {
	request := httptest.NewRequest(http.MethodPost,
		instanceOrigin+roster.Path, strings.NewReader(token))
	response := httptest.NewRecorder()
	receiver.ServeHTTP(response, request)
	return response.Code
}

func (fix *fixture) storedSequence(t *testing.T) int64 {
	t.Helper()
	var sequence int64
	err := fix.db.QueryRow(
		`SELECT sequence FROM roster_state WHERE id = 1`).Scan(&sequence)
	if errors.Is(err, sql.ErrNoRows) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return sequence
}

var (
	ownerA = roster.Member{
		Subject: "a", Email: "a@example.com", Name: "A", Role: "owner",
	}
	memberB = roster.Member{Subject: "b", Role: "member"}
	editorC = roster.Member{Subject: "c", Role: "editor"}
)

func TestReceiverAppliesASnapshot(t *testing.T) {
	fix := newFixture(t, ownerA, memberB)
	token := fix.token(t, 1, roster.Active, ownerA, editorC)
	if code := fix.post(token); code != 204 {
		t.Fatalf("status = %d, want 204", code)
	}
	if len(fix.store.members) != 2 || fix.store.members["c"] != editorC ||
		fix.store.members["a"] != ownerA {
		t.Errorf("store = %v, want a and c", fix.store.members)
	}
	if strings.Join(fix.store.puts, ",") != "c" {
		t.Errorf("puts = %v, want only c", fix.store.puts)
	}
	if strings.Join(fix.store.removes, ",") != "b" {
		t.Errorf("removes = %v, want only b", fix.store.removes)
	}
}

func TestReceiverRewritesAMemberWhoseDetailsChanged(t *testing.T) {
	fix := newFixture(t, ownerA)
	renamed := ownerA
	renamed.Name = "Ada"
	if code := fix.post(fix.token(t, 1, roster.Active, renamed)); code != 204 {
		t.Fatalf("status = %d, want 204", code)
	}
	if fix.store.members["a"] != renamed {
		t.Errorf("member = %v, want %v", fix.store.members["a"], renamed)
	}
}

func TestReceiverRefusesAnOlderSequence(t *testing.T) {
	fix := newFixture(t)
	first := fix.token(t, 2, roster.Active, ownerA)
	if code := fix.post(first); code != 204 {
		t.Fatalf("seq 2 status = %d, want 204", code)
	}
	older := fix.token(t, 1, roster.Suspended, editorC)
	if code := fix.post(older); code != 409 {
		t.Fatalf("seq 1 status = %d, want 409", code)
	}
	if len(fix.store.members) != 1 || fix.store.members["a"] != ownerA {
		t.Errorf("store = %v, want only a", fix.store.members)
	}
	if fix.storedSequence(t) != 2 {
		t.Errorf("sequence = %d, want 2", fix.storedSequence(t))
	}
	if suspended, _ := fix.receiver.Suspended(context.Background()); suspended {
		t.Error("an older snapshot changed the state")
	}
}

func TestReceiverAcceptsTheSameSequenceTwice(t *testing.T) {
	fix := newFixture(t, memberB)
	token := fix.token(t, 3, roster.Active, ownerA)
	for attempt := 1; attempt <= 2; attempt++ {
		if code := fix.post(token); code != 204 {
			t.Fatalf("attempt %d status = %d, want 204", attempt, code)
		}
		if attempt == 1 {
			fix.store.puts, fix.store.removes = nil, nil
		}
	}
	if len(fix.store.puts)+len(fix.store.removes) != 0 {
		t.Errorf("second delivery wrote: puts %v removes %v",
			fix.store.puts, fix.store.removes)
	}
}

func TestReceiverRefusesAnUnknownRole(t *testing.T) {
	fix := newFixture(t, memberB)
	stranger := roster.Member{Subject: "z", Role: "janitor"}
	token := fix.token(t, 1, roster.Active, editorC, stranger)
	if code := fix.post(token); code != 422 {
		t.Fatalf("status = %d, want 422", code)
	}
	if len(fix.store.puts)+len(fix.store.removes) != 0 {
		t.Errorf("something was applied: %v %v",
			fix.store.puts, fix.store.removes)
	}
	if fix.storedSequence(t) != 0 {
		t.Errorf("sequence = %d, want 0", fix.storedSequence(t))
	}
}

func TestReceiverRecordsSuspension(t *testing.T) {
	fix := newFixture(t)
	ctx := context.Background()
	fix.post(fix.token(t, 1, roster.Suspended))
	if suspended, err := fix.receiver.Suspended(ctx); err != nil || !suspended {
		t.Fatalf("Suspended = %v, %v, want true", suspended, err)
	}
	fix.post(fix.token(t, 2, roster.Active))
	if suspended, err := fix.receiver.Suspended(ctx); err != nil || suspended {
		t.Fatalf("Suspended = %v, %v, want false", suspended, err)
	}
}

func TestReceiverRefusesABadToken(t *testing.T) {
	fix := newFixture(t)
	wrong := fix.tokenFor(t, "https://other.example.com", 1, roster.Active)
	if code := fix.post(wrong); code != 403 {
		t.Errorf("wrong audience status = %d, want 403", code)
	}
	if code := fix.post("garbage"); code != 403 {
		t.Errorf("garbage status = %d, want 403", code)
	}
	request := httptest.NewRequest(
		http.MethodGet, instanceOrigin+roster.Path, nil)
	response := httptest.NewRecorder()
	fix.receiver.ServeHTTP(response, request)
	if response.Code != 405 {
		t.Errorf("GET status = %d, want 405", response.Code)
	}
}

func TestReceiverRefusesAnOversizedBody(t *testing.T) {
	fix := newFixture(t)
	if code := fix.post(strings.Repeat("a", 1<<20+1)); code != 413 {
		t.Errorf("status = %d, want 413", code)
	}
}

func TestReceiverRetriesAfterAStoreFailure(t *testing.T) {
	fix := newFixture(t)
	token := fix.token(t, 1, roster.Active, ownerA)
	fix.store.putErr = errors.New("disk full")
	if code := fix.post(token); code != 500 {
		t.Fatalf("status = %d, want 500", code)
	}
	if fix.storedSequence(t) != 0 {
		t.Errorf("sequence = %d, want 0", fix.storedSequence(t))
	}
	fix.store.putErr = nil
	if code := fix.post(token); code != 204 {
		t.Fatalf("retry status = %d, want 204", code)
	}
	if fix.store.members["a"] != ownerA || fix.storedSequence(t) != 1 {
		t.Errorf("retry did not apply: %v", fix.store.members)
	}
}

func TestSuspendedIsFalseBeforeAnySnapshot(t *testing.T) {
	fix := newFixture(t)
	suspended, err := fix.receiver.Suspended(context.Background())
	if err != nil || suspended {
		t.Errorf("Suspended = %v, %v, want false, nil", suspended, err)
	}
}

func TestNewRefusesAnIncompleteConfiguration(t *testing.T) {
	fix := newFixture(t)
	good := roster.Config{
		Verifier: fix.verifier, Store: fix.store, DB: fix.db,
		Roles: []string{"owner", "member"},
	}
	for name, mutate := range map[string]func(*roster.Config){
		"nil verifier": func(config *roster.Config) { config.Verifier = nil },
		"nil store":    func(config *roster.Config) { config.Store = nil },
		"nil database": func(config *roster.Config) { config.DB = nil },
		"no roles":     func(config *roster.Config) { config.Roles = nil },
		"duplicate roles": func(config *roster.Config) {
			config.Roles = []string{"a", "a"}
		},
	} {
		config := good
		mutate(&config)
		if _, err := roster.New(config); err == nil {
			t.Errorf("%s: New returned no error", name)
		}
	}
	if _, err := roster.New(good); err != nil {
		t.Errorf("good config refused: %v", err)
	}
}

// holdLease plays another process that is mid-apply.
func (fix *fixture) holdLease(t *testing.T, until int64) {
	t.Helper()
	_, err := fix.db.Exec(`INSERT INTO roster_state
		(id, sequence, state, lease_until) VALUES (1, 0, 'active', ?)
		ON CONFLICT(id) DO UPDATE SET lease_until = excluded.lease_until`,
		until)
	if err != nil {
		t.Fatal(err)
	}
}

func (fix *fixture) leaseUntil(t *testing.T) int64 {
	t.Helper()
	var until int64
	err := fix.db.QueryRow(
		`SELECT lease_until FROM roster_state WHERE id = 1`).Scan(&until)
	if err != nil {
		t.Fatal(err)
	}
	return until
}

func TestReceiverRefusesWhileAnotherProcessHoldsTheLease(t *testing.T) {
	fix := newFixture(t, memberB)
	fix.holdLease(t, epoch.Unix()+10)
	request := httptest.NewRequest(http.MethodPost,
		instanceOrigin+roster.Path,
		strings.NewReader(fix.token(t, 1, roster.Active, ownerA)))
	response := httptest.NewRecorder()
	fix.receiver.ServeHTTP(response, request)
	if response.Code != 503 {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	if response.Header().Get("Retry-After") != "1" {
		t.Errorf("Retry-After = %q, want 1",
			response.Header().Get("Retry-After"))
	}
	if len(fix.store.puts)+len(fix.store.removes) != 0 {
		t.Errorf("store written: %v %v", fix.store.puts, fix.store.removes)
	}
	if fix.leaseUntil(t) != epoch.Unix()+10 {
		t.Error("the other process's lease was disturbed")
	}
}

func TestReceiverTakesAnExpiredLease(t *testing.T) {
	fix := newFixture(t)
	fix.holdLease(t, epoch.Unix()-1)
	if code := fix.post(fix.token(t, 1, roster.Active, ownerA)); code != 204 {
		t.Fatalf("status = %d, want 204", code)
	}
	if fix.leaseUntil(t) != 0 {
		t.Errorf("lease = %d, want released", fix.leaseUntil(t))
	}
}

func TestReceiverReleasesTheLeaseAfterAFailedApply(t *testing.T) {
	fix := newFixture(t)
	token := fix.token(t, 1, roster.Active, ownerA)
	fix.store.putErr = errors.New("disk full")
	if code := fix.post(token); code != 500 {
		t.Fatalf("status = %d, want 500", code)
	}
	fix.store.putErr = nil
	if code := fix.post(token); code != 204 {
		t.Fatalf("retry status = %d, want 204", code)
	}
}

func TestReceiverKeepsTheSequenceFromMovingBackwards(t *testing.T) {
	fix := newFixture(t)
	// Another process finishes seq 5 while this one is writing seq 3, as
	// happens if its lease expired.
	fix.store.onPut = func() {
		_, err := fix.db.Exec(
			`UPDATE roster_state SET sequence = 5, state = 'suspended'`)
		if err != nil {
			t.Error(err)
		}
	}
	if code := fix.post(fix.token(t, 3, roster.Active, ownerA)); code != 409 {
		t.Fatalf("status = %d, want 409", code)
	}
	if fix.storedSequence(t) != 5 {
		t.Errorf("sequence = %d, want 5", fix.storedSequence(t))
	}
	suspended, _ := fix.receiver.Suspended(context.Background())
	if !suspended {
		t.Error("the older snapshot overwrote the newer state")
	}
}

func TestReceiverReleaseLeavesAnotherProcessesLease(t *testing.T) {
	fix := newFixture(t)
	other := epoch.Unix() + 99
	// Another process takes our lapsed lease while the Store is slow, so
	// this apply cannot confirm its writes and the root must retry.
	fix.store.onPut = func() { fix.holdLease(t, other) }
	if code := fix.post(fix.token(t, 1, roster.Active, ownerA)); code != 503 {
		t.Fatalf("status = %d, want 503", code)
	}
	if fix.leaseUntil(t) != other {
		t.Errorf("lease = %d, want %d: release freed another's lease",
			fix.leaseUntil(t), other)
	}
}

func TestReceiverRepairsTheRosterAfterLosingItsLease(t *testing.T) {
	fix := newFixture(t, ownerA)
	other := fix.process(t)
	renamed := ownerA
	renamed.Name = "Ada"
	// This process stalls inside Put until its lease lapses. Another
	// process then applies a newer roster that removes the owner, and
	// only after that does the stalled Put land.
	fix.store.onPut = func() {
		fix.store.onPut = nil
		fix.now = epoch.Add(31 * time.Second)
		newer := fix.token(t, 5, roster.Active, memberB)
		if code := fix.postTo(other, newer); code != 204 {
			t.Errorf("newer snapshot status = %d, want 204", code)
		}
	}
	if code := fix.post(fix.token(t, 3, roster.Active, renamed)); code != 409 {
		t.Fatalf("status = %d, want 409", code)
	}
	if len(fix.store.members) != 1 || fix.store.members["b"] != memberB {
		t.Errorf("store = %v, want only b: the stale write survived",
			fix.store.members)
	}
	if fix.storedSequence(t) != 5 {
		t.Errorf("sequence = %d, want 5", fix.storedSequence(t))
	}
	if fix.leaseUntil(t) != 0 {
		t.Errorf("lease = %d, want released", fix.leaseUntil(t))
	}
}

func TestReceiverGivesUpWhenItKeepsLosingTheLease(t *testing.T) {
	fix := newFixture(t)
	attempts := 0
	// Each time, another process takes and frees the lease mid-apply.
	fix.store.onMembers = func() {
		attempts++
		fix.holdLease(t, 0)
	}
	code := fix.post(fix.token(t, 1, roster.Active, ownerA))
	if code != 503 {
		t.Fatalf("status = %d, want 503", code)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	if fix.storedSequence(t) != 0 {
		t.Errorf("sequence = %d, want 0", fix.storedSequence(t))
	}
}

// watchedBody records whether anything read it.
type watchedBody struct {
	reader io.Reader
	read   bool
}

func (body *watchedBody) Read(buffer []byte) (int, error) {
	body.read = true
	return body.reader.Read(buffer)
}

func TestReceiverIsNotFoundOnAnotherHost(t *testing.T) {
	fix := newFixture(t, memberB)
	body := &watchedBody{
		reader: strings.NewReader(fix.token(t, 1, roster.Active, ownerA)),
	}
	request := httptest.NewRequest(http.MethodPost,
		"https://other.example.com"+roster.Path, body)
	response := httptest.NewRecorder()
	fix.receiver.ServeHTTP(response, request)
	if response.Code != 404 {
		t.Fatalf("status = %d, want 404", response.Code)
	}
	if body.read {
		t.Error("the body was read on another host")
	}
	if len(fix.store.puts)+len(fix.store.removes) != 0 ||
		fix.storedSequence(t) != 0 {
		t.Errorf("applied on another host: %v", fix.store.members)
	}
}

func TestReceiverMatchesItsHostTheWayBrowsersSendIt(t *testing.T) {
	fix := newFixture(t)
	request := httptest.NewRequest(http.MethodPost, roster.Path,
		strings.NewReader(fix.token(t, 1, roster.Active, ownerA)))
	request.Host = "ACME.example.com:443"
	response := httptest.NewRecorder()
	fix.receiver.ServeHTTP(response, request)
	if response.Code != 204 {
		t.Fatalf("status = %d, want 204", response.Code)
	}
}

func TestReceiverLogsStoreFailuresWithoutTheirText(t *testing.T) {
	leaked := errors.New("cannot save a@example.com")
	for want, fail := range map[string]func(*fakeStore){
		"store put failed": func(store *fakeStore) { store.putErr = leaked },
		"store remove failed": func(store *fakeStore) {
			store.removeErr = leaked
		},
		"store members failed": func(store *fakeStore) {
			store.membersErr = leaked
		},
	} {
		t.Run(want, func(t *testing.T) {
			fix := newFixture(t, memberB)
			fail(fix.store)
			code := fix.post(fix.token(t, 1, roster.Active, ownerA))
			if code != 500 {
				t.Fatalf("status = %d, want 500", code)
			}
			if !strings.Contains(fix.log.String(), want) {
				t.Errorf("log lacks %q: %s", want, fix.log)
			}
			if strings.Contains(fix.log.String(), "a@example.com") {
				t.Errorf("log holds the Store's error text: %s", fix.log)
			}
		})
	}
}
