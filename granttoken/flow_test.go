package granttoken_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	json "github.com/go-json-experiment/json"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
	"amadan.net/rastrillo/rastrillo/apitoken"
	"amadan.net/rastrillo/rastrillo/dbtest"
	"amadan.net/rastrillo/rastrillo/granttoken"
	"amadan.net/rastrillo/rastrillo/handoff/handofftest"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/op"
	"amadan.net/rastrillo/rastrillo/roster"
)

const (
	siteOrigin = "https://site.test"
	things     = "/api/v1/things"
	widen      = "/api/v1/grants/widen"
	bigAnswer  = 200 << 10
)

var schema = dbtest.FromSet(migrate.Merge(roster.Schema, apitoken.Schema))

func TestMain(tests *testing.M) {
	code := tests.Run()
	schema.Remove()
	os.Exit(code)
}

// owner is the site's one member, holding grants.
func owner(grants ...string) roster.Member {
	return roster.Member{Subject: "owneruid", Role: "owner", Grants: grants}
}

// memberStore is the app's own member table, which grants never reach.
type memberStore struct{ members map[string]roster.Member }

func (store *memberStore) Members(context.Context) ([]roster.Member, error) {
	var members []roster.Member
	for _, member := range store.members {
		members = append(members, member)
	}
	return members, nil
}

func (store *memberStore) Put(_ context.Context, member roster.Member) error {
	store.members[member.Subject] = member
	return nil
}

func (store *memberStore) Remove(_ context.Context, subject string) error {
	delete(store.members, subject)
	return nil
}

// site is one hosted site under a root, served on a real connection:
// the tree sets write deadlines as an answer goes out, which a
// recorder cannot take.
type site struct {
	t        *testing.T
	root     *handofftest.Root
	receiver *roster.Receiver
	server   *httptest.Server

	// beforeRead runs in the read's handler before it answers.
	beforeRead func()
	// writes counts the write handlers that ran; wrote is the actor
	// the last one saw.
	writes int
	wrote  rastrillo.Actor
}

// newSite serves the site with seq 1 applied. wrap, when set, stands
// between the door and the receiver.
func newSite(
	t *testing.T, wrap func(granttoken.Grants) granttoken.Grants,
) *site {
	t.Helper()
	now := time.Now()
	clock := func() time.Time { return now }
	root := handofftest.New(t)
	root.Now = clock
	db := schema.Open(t)
	fixture := &site{t: t, root: root}
	fixture.receiver = newReceiver(t, root, db, clock)
	var grants granttoken.Grants = fixture.receiver
	if wrap != nil {
		grants = wrap(grants)
	}
	verify := newVerifier(t, root, db, grants, clock)
	tree, err := api.New(api.Config{
		Verify: verify,
		Ctx: func(*http.Request) *rastrillo.Ctx {
			return &rastrillo.Ctx{}
		},
		Authorize: op.DefaultPolicy, Logger: slog.New(slog.DiscardHandler),
		RequireAgentSession: true, Redact: verify.Redact,
	}, fixture.routes()...)
	if err != nil {
		t.Fatal(err)
	}
	fixture.server = httptest.NewUnstartedServer(tree)
	fixture.server.Config.ErrorLog = log.New(io.Discard, "", 0)
	fixture.server.Start()
	t.Cleanup(fixture.server.Close)
	fixture.post(1, roster.Active, owner("g1"))
	return fixture
}

func newReceiver(
	t *testing.T, root *handofftest.Root, db *sql.DB,
	clock func() time.Time,
) *roster.Receiver {
	t.Helper()
	setup := root.Setup()
	verifier, err := roster.NewVerifier(setup.Root, siteOrigin, setup.Keys)
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := roster.New(roster.Config{
		Verifier: verifier,
		Store:    &memberStore{members: map[string]roster.Member{}},
		DB:       db, Roles: []string{"owner"}, Now: clock,
		Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	return receiver
}

// newVerifier is what a site hands its tree: grant tokens at the door,
// the site's own tokens to its store.
func newVerifier(
	t *testing.T, root *handofftest.Root, db *sql.DB,
	grants granttoken.Grants, clock func() time.Time,
) *granttoken.Either {
	t.Helper()
	verifier, err := granttoken.NewVerifier(
		root.Origin, siteOrigin, root.Setup().Keys)
	if err != nil {
		t.Fatal(err)
	}
	door, err := granttoken.NewDoor(granttoken.DoorConfig{
		Verifier: verifier, Grants: grants, Now: clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := apitoken.New(apitoken.Config{DB: db, Prefix: "site"})
	if err != nil {
		t.Fatal(err)
	}
	return door.Or(tokens)
}

func operation(
	id string, access rastrillo.Access, humanOnly string,
) op.Descriptor {
	return op.Descriptor{
		Operation: rastrillo.Operation{
			ID: id, Resource: "things", Scoped: true, Access: access,
			HumanOnly: humanOnly,
		},
		Exposed: true,
		Input:   []byte(`{"type":"object"}`),
		Result:  []byte(`{"type":"object"}`),
	}
}

func (fixture *site) routes() []api.Route {
	return []api.Route{{
		Method: http.MethodGet, Pattern: things,
		Op:     operation("things.list", rastrillo.Read, ""),
		Handle: fixture.read,
	}, {
		Method: http.MethodPost, Pattern: things,
		Op:     operation("things.create", rastrillo.Write, ""),
		Handle: fixture.write,
	}, {
		Method: http.MethodPost, Pattern: widen,
		Op: operation("grants.widen", rastrillo.Write,
			"an agent must not widen its own grant"),
		Handle: fixture.write,
	}}
}

// read answers bigAnswer bytes, written raw so no Content-Length comes
// from api.Write.
func (fixture *site) read(
	_ context.Context, _ *rastrillo.Ctx,
	response http.ResponseWriter, _ *http.Request,
) {
	if fixture.beforeRead != nil {
		fixture.beforeRead()
	}
	_, _ = response.Write(bytes.Repeat([]byte("x"), bigAnswer))
}

func (fixture *site) write(
	ctx context.Context, _ *rastrillo.Ctx,
	response http.ResponseWriter, _ *http.Request,
) {
	fixture.writes++
	fixture.wrote, _ = rastrillo.ActorFromContext(ctx)
	api.Write(response, http.StatusCreated, map[string]string{})
}

// grantToken is the owner's token for grant g1 on audience.
func (fixture *site) grantToken(audience, access string) string {
	return fixture.root.GrantToken(fixture.t, granttoken.Claims{
		Audience: audience, Subject: "owneruid", Grant: "g1",
		Agent: "Claude", Access: access,
	})
}

func (fixture *site) snapshot(
	sequence int64, state roster.State, members ...roster.Member,
) string {
	return fixture.root.Roster(
		fixture.t, siteOrigin, sequence, state, members...)
}

// deliver posts a snapshot as the root does. It may run off the test's
// goroutine, so it only reports a refusal.
func (fixture *site) deliver(snapshot string) bool {
	request := httptest.NewRequest(http.MethodPost,
		siteOrigin+roster.Path, strings.NewReader(snapshot))
	recorder := httptest.NewRecorder()
	fixture.receiver.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		fixture.t.Errorf("snapshot answered %d, want 204", recorder.Code)
		return false
	}
	return true
}

func (fixture *site) post(
	sequence int64, state roster.State, members ...roster.Member,
) {
	fixture.t.Helper()
	if !fixture.deliver(fixture.snapshot(sequence, state, members...)) {
		fixture.t.FailNow()
	}
}

// answer is one response read off the connection.
type answer struct {
	status  int
	header  http.Header
	body    []byte
	readErr error
}

func (fixture *site) call(
	method, target, token string, header ...string,
) answer {
	fixture.t.Helper()
	request, err := http.NewRequest(method, fixture.server.URL+target,
		strings.NewReader(`{}`))
	if err != nil {
		fixture.t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	for index := 0; index+1 < len(header); index += 2 {
		request.Header.Set(header[index], header[index+1])
	}
	response, err := fixture.server.Client().Do(request)
	if err != nil {
		fixture.t.Fatalf("%s %s: %v", method, target, err)
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(response.Body)
	return answer{response.StatusCode, response.Header, body, readErr}
}

// refused fails unless the answer is the refusal named, with none of
// the read's bytes.
func (reply answer) refused(t *testing.T, status int, code string) {
	t.Helper()
	if reply.status != status {
		t.Fatalf("status = %d, want %d; body %.200s",
			reply.status, status, reply.body)
	}
	var envelope struct {
		Error api.Error `json:"error"`
	}
	if err := json.Unmarshal(reply.body, &envelope); err != nil {
		t.Fatalf("body %.200q is not an error envelope: %v", reply.body, err)
	}
	if envelope.Error.Code != code {
		t.Fatalf("code = %q, want %q", envelope.Error.Code, code)
	}
}

// countedGrants passes each question on and, once it has its answer,
// tells afterAnswer how many it has had.
type countedGrants struct {
	granttoken.Grants
	mutex       sync.Mutex
	answered    int
	afterAnswer func(answered int)
}

func (counted *countedGrants) GrantActive(
	ctx context.Context, subject, grant string,
) (bool, error) {
	active, err := counted.Grants.GrantActive(ctx, subject, grant)
	counted.mutex.Lock()
	counted.answered++
	answered := counted.answered
	counted.mutex.Unlock()
	counted.afterAnswer(answered)
	return active, err
}

func TestFlowGrantTokenReadsAndWrites(t *testing.T) {
	fixture := newSite(t, nil)
	token := fixture.grantToken(siteOrigin, granttoken.Write)
	read := fixture.call(http.MethodGet, things, token)
	if read.status != http.StatusOK || len(read.body) != bigAnswer {
		t.Fatalf("GET = %d with %d bytes, want 200 with %d",
			read.status, len(read.body), bigAnswer)
	}
	wrote := fixture.call(http.MethodPost, things, token,
		"Agent-Session", "s1")
	if wrote.status != http.StatusCreated {
		t.Fatalf("POST = %d, want 201; body %s", wrote.status, wrote.body)
	}
	want := "agent:Claude for user:owneruid grant:g1"
	if got := fixture.wrote.String(); got != want {
		t.Errorf("actor = %q, want %q", got, want)
	}
	if fixture.wrote.Session != "s1" {
		t.Errorf("Session = %q, want s1", fixture.wrote.Session)
	}
}

func TestFlowRevokedBySnapshot(t *testing.T) {
	fixture := newSite(t, nil)
	token := fixture.grantToken(siteOrigin, granttoken.Read)
	if read := fixture.call(http.MethodGet, things, token); read.status !=
		http.StatusOK {
		t.Fatalf("GET = %d, want 200 before the snapshot", read.status)
	}
	fixture.post(2, roster.Active, owner())
	fixture.call(http.MethodGet, things, token).
		refused(t, http.StatusUnauthorized, "unauthorized")
}

func TestFlowRevokedDuringTheHandler(t *testing.T) {
	fixture := newSite(t, nil)
	revocation := fixture.snapshot(2, roster.Active, owner())
	fixture.beforeRead = func() { fixture.deliver(revocation) }
	read := fixture.call(http.MethodGet, things,
		fixture.grantToken(siteOrigin, granttoken.Read))
	read.refused(t, http.StatusUnauthorized, "unauthorized")
	if bytes.Contains(read.body, []byte("xxxx")) {
		t.Fatalf("the read's bytes went out: %.200s", read.body)
	}
}

func TestFlowRevokedMidAnswer(t *testing.T) {
	counted := &countedGrants{}
	fixture := newSite(t, func(
		receiver granttoken.Grants,
	) granttoken.Grants {
		counted.Grants = receiver
		return counted
	})
	revocation := fixture.snapshot(2, roster.Active, owner())
	counted.afterAnswer = func(answered int) {
		// Verify's, then the recheck before the headers: revoking now
		// lets the headers and the first chunk go, and the next
		// recheck finds the grant gone.
		if answered == 2 {
			fixture.deliver(revocation)
		}
	}
	read := fixture.call(http.MethodGet, things,
		fixture.grantToken(siteOrigin, granttoken.Read))
	if read.status != http.StatusOK {
		t.Fatalf("status = %d, want 200 before the revocation", read.status)
	}
	length := read.header.Get("Content-Length")
	if length != strconv.Itoa(bigAnswer) {
		t.Errorf("Content-Length = %q, want %d", length, bigAnswer)
	}
	if !errors.Is(read.readErr, io.ErrUnexpectedEOF) {
		t.Fatalf("read error = %v, want an unexpected EOF", read.readErr)
	}
	if len(read.body) > 64<<10 {
		t.Fatalf("the client read %d bytes, more than one 64 KiB chunk",
			len(read.body))
	}
}

func TestFlowReadGrantCannotWrite(t *testing.T) {
	fixture := newSite(t, nil)
	fixture.call(http.MethodPost, things,
		fixture.grantToken(siteOrigin, granttoken.Read),
		"Agent-Session", "s1").
		refused(t, http.StatusForbidden, "forbidden")
}

func TestFlowWriteNeedsAgentSession(t *testing.T) {
	fixture := newSite(t, nil)
	fixture.call(http.MethodPost, things,
		fixture.grantToken(siteOrigin, granttoken.Write)).
		refused(t, http.StatusUnprocessableEntity, "invalid")
}

func TestFlowHumanOnlyRefused(t *testing.T) {
	fixture := newSite(t, nil)
	fixture.call(http.MethodPost, widen,
		fixture.grantToken(siteOrigin, granttoken.Write),
		"Agent-Session", "s1").
		refused(t, http.StatusForbidden, "human_only")
	if fixture.writes != 0 {
		t.Fatalf("the human-only handler ran %d times", fixture.writes)
	}
}

func TestFlowOtherSitesTokenRefused(t *testing.T) {
	fixture := newSite(t, nil)
	fixture.call(http.MethodGet, things,
		fixture.grantToken("https://other.test", granttoken.Read)).
		refused(t, http.StatusUnauthorized, "unauthorized")
}

func TestFlowSuspendedSite(t *testing.T) {
	fixture := newSite(t, nil)
	fixture.post(2, roster.Suspended, owner("g1"))
	fixture.call(http.MethodGet, things,
		fixture.grantToken(siteOrigin, granttoken.Read)).
		refused(t, http.StatusUnauthorized, "unauthorized")
}
