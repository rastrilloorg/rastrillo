package apitoken_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
	"amadan.net/rastrillo/rastrillo/apitoken"
)

// The tree (api.New) takes any api.Verifier; Store must be one. The
// interface is spelled out rather than named so this package's tests
// do not wait on the tree to compile.
var _ interface {
	Verify(context.Context, *http.Request) (rastrillo.Actor, error)
} = (*apitoken.Store)(nil)

var grammar = regexp.MustCompile(`^tickets_([0-9A-Za-z]{12})_([0-9A-Za-z]{43})$`)

// clock is a settable Now, so expiry and the once-a-minute last_used
// write can be tested without sleeping.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

var t0 = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

func newStore(t *testing.T, mut func(*apitoken.Config)) (*apitoken.Store, *sql.DB, *clock) {
	t.Helper()
	d := schema.Open(t)
	c := &clock{t: t0}
	cfg := apitoken.Config{DB: d, Prefix: "tickets", Now: c.now}
	if mut != nil {
		mut(&cfg)
	}
	s, err := apitoken.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, d, c
}

func mint(t *testing.T, s *apitoken.Store, subject string, in apitoken.MintInput) (string, apitoken.Token) {
	t.Helper()
	secret, tok, err := s.Mint(context.Background(), subject, in)
	if err != nil {
		t.Fatalf("Mint(%q, %+v): %v", subject, in, err)
	}
	return secret, tok
}

var claude = apitoken.MintInput{Agent: "Claude", Access: rastrillo.Write}

func bearer(token string) *http.Request {
	r := httptest.NewRequest("GET", "/api/v1/bookmarks", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

func verify(s *apitoken.Store, r *http.Request) (rastrillo.Actor, error) {
	return s.Verify(context.Background(), r)
}

func TestMintShapeAndStorage(t *testing.T) {
	s, d, _ := newStore(t, nil)
	secret, tok := mint(t, s, "alice", apitoken.MintInput{Agent: "Claude", Access: rastrillo.Read, TTL: 90 * 24 * time.Hour})
	m := grammar.FindStringSubmatch(secret)
	if m == nil {
		t.Fatalf("Mint returned %d characters that do not match %s", len(secret), grammar)
	}
	if m[1] != tok.ID {
		t.Errorf("the token's public ID is %q, but Token.ID is %q", m[1], tok.ID)
	}
	if tok.Subject != "alice" || tok.Agent != "Claude" || tok.Access != rastrillo.Read ||
		!tok.CreatedAt.Equal(t0) || tok.ExpiresAt == nil || !tok.ExpiresAt.Equal(t0.Add(90*24*time.Hour)) ||
		tok.LastUsed != nil || tok.RevokedAt != nil {
		t.Errorf("Token = %+v", tok)
	}

	var hash []byte
	if err := d.QueryRow(`SELECT secret_hash FROM api_tokens WHERE id = ?`, tok.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if want := sha256.Sum256([]byte(m[2])); string(hash) != string(want[:]) {
		t.Errorf("secret_hash = %x, want SHA-256 of the secret part %x", hash, want)
	}

	// Read every column of the row as text: neither the token nor its
	// secret part may be anywhere in it.
	rows, err := d.Query(`SELECT * FROM api_tokens`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		for i, v := range vals {
			if strings.Contains(fmt.Sprintf("%s", v), m[2]) {
				t.Errorf("column %s holds the secret", cols[i])
			}
		}
	}

	// Two mints never share an ID or a secret.
	secret2, tok2 := mint(t, s, "alice", claude)
	if tok2.ID == tok.ID || secret2 == secret {
		t.Error("two mints produced the same ID or secret")
	}
	if tok2.ExpiresAt != nil {
		t.Errorf("TTL 0 means never, but ExpiresAt = %v", tok2.ExpiresAt)
	}
}

func TestMintRefuses(t *testing.T) {
	s, _, _ := newStore(t, nil)
	for name, in := range map[string]apitoken.MintInput{
		"no agent and not for self": {Access: rastrillo.Write},
		"an agent name with <":      {Agent: "<Claude>", Access: rastrillo.Write},
		"a 41-character agent name": {Agent: strings.Repeat("a", 41), Access: rastrillo.Write},
		"zero Access":               {Agent: "Claude"},
		"an Access past Write":      {Agent: "Claude", Access: 3},
		"for self with an agent":    {ForSelf: true, Agent: "Claude", Access: rastrillo.Write},
		"a negative TTL":            {Agent: "Claude", Access: rastrillo.Write, TTL: -time.Hour},
	} {
		if secret, _, err := s.Mint(context.Background(), "alice", in); err == nil || secret != "" {
			t.Errorf("%s: Mint = %q, %v; want refused with no token", name, secret, err)
		}
	}
	if _, _, err := s.Mint(context.Background(), "", claude); err == nil {
		t.Error("Mint with no subject: want refused")
	}
}

func TestVerify(t *testing.T) {
	s, _, _ := newStore(t, nil)
	secret, tok := mint(t, s, "alice", apitoken.MintInput{Agent: " Claude ", Access: rastrillo.Read})
	want := rastrillo.Actor{Subject: "alice", Agent: "Claude", Credential: tok.ID, Access: rastrillo.Read}

	a, err := verify(s, bearer(secret))
	if err != nil || a != want {
		t.Fatalf("Verify = %+v, %v; want %+v", a, err, want)
	}
	r := httptest.NewRequest("GET", "/api/v1/bookmarks", nil)
	r.Header.Set("Authorization", "bearer "+secret)
	if a, err := verify(s, r); err != nil || a != want {
		t.Errorf("lowercase scheme: Verify = %+v, %v; want %+v", a, err, want)
	}

	// The actor comes from the row only. Headers a client sends to
	// claim another agent, person or session do not reach it here; the
	// tree copies Agent-Session and Agent-Harness itself, for agent
	// tokens only.
	r = bearer(secret)
	r.Header.Set("Agent", "Codex")
	r.Header.Set("Agent-Session", "s1")
	r.Header.Set("Agent-Harness", "h/1")
	r.Header.Set("X-Subject", "bob")
	if a, err := verify(s, r); err != nil || a != want {
		t.Errorf("with claim headers: Verify = %+v, %v; want %+v", a, err, want)
	}

	self, selfTok := mint(t, s, "alice", apitoken.MintInput{ForSelf: true, Access: rastrillo.Write})
	wantSelf := rastrillo.Actor{Subject: "alice", Agent: "", Credential: selfTok.ID, Access: rastrillo.Write}
	if a, err := verify(s, bearer(self)); err != nil || a != wantSelf {
		t.Errorf("own token: Verify = %+v, %v; want %+v", a, err, wantSelf)
	}
}

func TestVerifyRefusesAllTheSame(t *testing.T) {
	allowed := true
	s, _, c := newStore(t, func(cfg *apitoken.Config) {
		cfg.Allowed = func(_ context.Context, subject string) (bool, error) {
			return allowed || subject != "alice", nil
		}
	})
	secret, tok := mint(t, s, "alice", claude)
	m := grammar.FindStringSubmatch(secret)
	if m == nil {
		t.Fatalf("Mint returned %d characters that do not match %s", len(secret), grammar)
	}
	expiring, _ := mint(t, s, "alice", apitoken.MintInput{Agent: "Claude", Access: rastrillo.Write, TTL: time.Hour})
	revoked, revokedTok := mint(t, s, "alice", claude)
	if err := s.Revoke(context.Background(), "alice", revokedTok.ID); err != nil {
		t.Fatal(err)
	}
	// The real secret with its last character changed.
	flip := "a"
	if m[2][42] == 'a' {
		flip = "b"
	}
	wrongSecret := m[2][:42] + flip

	header := func(v string) *http.Request {
		r := httptest.NewRequest("GET", "/api/v1/bookmarks", nil)
		if v != "" {
			r.Header.Set("Authorization", v)
		}
		return r
	}
	cases := map[string]*http.Request{
		"no header":             header(""),
		"Basic":                 header("Basic x"),
		"Bearer and nothing":    header("Bearer "),
		"malformed":             bearer("tickets_short_x"),
		"a 44-character secret": bearer(secret + "a"),
		"a trailing word":       bearer(secret + " extra"),
		"wrong prefix":          bearer(shaped("other", m[1], m[2])),
		"unknown ID":            bearer(shaped("tickets", exampleID, m[2])),
		"right ID wrong secret": bearer(shaped("tickets", tok.ID, wrongSecret)),
		"revoked":               bearer(revoked),
		"token in the query":    httptest.NewRequest("GET", "/api/v1/bookmarks?access_token="+secret, nil),
	}
	for name, r := range cases {
		if _, err := verify(s, r); !errors.Is(err, api.ErrUnauthorized) {
			t.Errorf("%s: Verify err = %v, want api.ErrUnauthorized", name, err)
		}
	}

	c.t = t0.Add(time.Hour)
	if _, err := verify(s, bearer(expiring)); !errors.Is(err, api.ErrUnauthorized) {
		t.Errorf("expired: Verify err = %v, want api.ErrUnauthorized", err)
	}
	c.t = t0

	allowed = false
	if _, err := verify(s, bearer(secret)); !errors.Is(err, api.ErrUnauthorized) {
		t.Errorf("Allowed false: Verify err = %v, want api.ErrUnauthorized", err)
	}
	allowed = true
	if _, err := verify(s, bearer(secret)); err != nil {
		t.Errorf("the same token once Allowed: %v", err)
	}
}

func TestVerifyStoreErrorsAreUnavailable(t *testing.T) {
	boom := errors.New("directory down")
	s, d, _ := newStore(t, func(cfg *apitoken.Config) {
		cfg.Allowed = func(context.Context, string) (bool, error) { return false, boom }
	})
	secret, _ := mint(t, s, "alice", claude)
	_, err := verify(s, bearer(secret))
	if !errors.Is(err, api.ErrUnavailable) || errors.Is(err, api.ErrUnauthorized) {
		t.Errorf("Allowed error: Verify err = %v, want api.ErrUnavailable only", err)
	}

	d.Close()
	_, err = verify(s, bearer(secret))
	if !errors.Is(err, api.ErrUnavailable) || errors.Is(err, api.ErrUnauthorized) {
		t.Errorf("closed DB: Verify err = %v, want api.ErrUnavailable only", err)
	}
	// A malformed token is refused before the database is asked, so even
	// with the database gone it is a 401, not a 503.
	if _, err := verify(s, bearer("tickets_short_x")); !errors.Is(err, api.ErrUnauthorized) {
		t.Errorf("malformed with a closed DB: Verify err = %v, want api.ErrUnauthorized", err)
	}
}

func TestLastUsedAtMostOnceAMinute(t *testing.T) {
	s, d, c := newStore(t, nil)
	secret, tok := mint(t, s, "alice", claude)
	lastUsed := func() string {
		t.Helper()
		var v sql.NullString
		if err := d.QueryRow(`SELECT last_used FROM api_tokens WHERE id = ?`, tok.ID).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v.String
	}
	if got := lastUsed(); got != "" {
		t.Fatalf("last_used before any use = %q", got)
	}
	use := func(at time.Time) {
		t.Helper()
		c.t = at
		if _, err := verify(s, bearer(secret)); err != nil {
			t.Fatal(err)
		}
	}
	use(t0)
	first := lastUsed()
	if first == "" {
		t.Fatal("last_used not written on first verify")
	}
	use(t0.Add(59 * time.Second))
	if got := lastUsed(); got != first {
		t.Errorf("last_used rewritten after 59s: %q, was %q", got, first)
	}
	use(t0.Add(61 * time.Second))
	if got := lastUsed(); got == first {
		t.Errorf("last_used not rewritten after 61s: still %q", got)
	}
	toks, err := s.List(context.Background(), "alice")
	if err != nil || len(toks) != 1 || toks[0].LastUsed == nil || !toks[0].LastUsed.Equal(t0.Add(61*time.Second)) {
		t.Errorf("List = %+v, %v; want LastUsed %v", toks, err, t0.Add(61*time.Second))
	}
}

func TestManagementIsBoundToTheOwner(t *testing.T) {
	s, _, c := newStore(t, nil)
	ctx := context.Background()
	bobSecret, bobTok := mint(t, s, "bob", claude)
	c.t = t0.Add(time.Second)
	_, aliceTok := mint(t, s, "alice", claude)
	c.t = t0.Add(2 * time.Second)
	_, aliceOwn := mint(t, s, "alice", apitoken.MintInput{ForSelf: true, Access: rastrillo.Read})

	if err := s.Revoke(ctx, "alice", bobTok.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("alice revoking bob's token: err = %v, want sql.ErrNoRows", err)
	}
	if _, err := verify(s, bearer(bobSecret)); err != nil {
		t.Errorf("bob's token after alice tried to revoke it: %v", err)
	}
	if err := s.Revoke(ctx, "alice", "nosuchtoken0"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("revoking an unknown ID: err = %v, want sql.ErrNoRows", err)
	}

	list, err := s.List(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, tk := range list {
		ids = append(ids, tk.ID)
		if tk.Subject != "alice" {
			t.Errorf("List(alice) holds %s's token %s", tk.Subject, tk.ID)
		}
	}
	// Newest first: created_at DESC, id DESC.
	if len(ids) != 2 || ids[0] != aliceOwn.ID || ids[1] != aliceTok.ID {
		t.Errorf("List(alice) IDs = %v, want [%s %s]", ids, aliceOwn.ID, aliceTok.ID)
	}

	if n, err := s.RevokeAgent(ctx, "alice", "Claude"); err != nil || n != 1 {
		t.Errorf("RevokeAgent(alice, Claude) = %d, %v; want 1", n, err)
	}
	if _, err := verify(s, bearer(bobSecret)); err != nil {
		t.Errorf("bob's Claude after alice revoked her Claude: %v", err)
	}
	if n, err := s.RevokeAll(ctx, "alice"); err != nil || n != 1 {
		t.Errorf("RevokeAll(alice) = %d, %v; want 1 (her own token; Claude was already revoked)", n, err)
	}
	if _, err := verify(s, bearer(bobSecret)); err != nil {
		t.Errorf("bob's token after alice revoked all of hers: %v", err)
	}
	list, _ = s.List(ctx, "alice")
	for _, tk := range list {
		if tk.RevokedAt == nil {
			t.Errorf("alice's %s survived RevokeAll", tk.ID)
		}
	}

	// Revoking twice is not an error and does not move the time.
	if err := s.Revoke(ctx, "bob", bobTok.ID); err != nil {
		t.Fatal(err)
	}
	c.t = t0.Add(time.Hour)
	if err := s.Revoke(ctx, "bob", bobTok.ID); err != nil {
		t.Errorf("second revoke: %v", err)
	}
	list, _ = s.List(ctx, "bob")
	if len(list) != 1 || list[0].RevokedAt == nil || !list[0].RevokedAt.Equal(t0.Add(2*time.Second)) {
		t.Errorf("List(bob) = %+v; want revoked at %v", list, t0.Add(2*time.Second))
	}
	if _, err := verify(s, bearer(bobSecret)); !errors.Is(err, api.ErrUnauthorized) {
		t.Errorf("bob's revoked token: err = %v, want api.ErrUnauthorized", err)
	}
}

// countLastUsedWrites installs a trigger that records every UPDATE that
// sets last_used, so a test can count the writes that really happened
// rather than infer them from the final value.
func countLastUsedWrites(t *testing.T, d *sql.DB) func() int {
	t.Helper()
	for _, q := range []string{
		`CREATE TABLE last_used_writes (at TEXT)`,
		`CREATE TRIGGER last_used_written AFTER UPDATE OF last_used ON api_tokens
		 BEGIN INSERT INTO last_used_writes VALUES (NEW.last_used); END`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return func() int {
		t.Helper()
		var n int
		if err := d.QueryRow(`SELECT count(*) FROM last_used_writes`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
}

// TestLastUsedConcurrentVerifiesWriteOnce holds n verifications of one
// token in Allowed until all n have read the row, so every one of them
// saw last_used empty. Only one may then write it: the once-a-minute
// rule has to hold in the database, not only in each request's copy.
func TestLastUsedConcurrentVerifiesWriteOnce(t *testing.T) {
	const n = 8
	var arrived sync.WaitGroup
	arrived.Add(n)
	release := make(chan struct{})
	s, d, _ := newStore(t, func(cfg *apitoken.Config) {
		cfg.Allowed = func(context.Context, string) (bool, error) {
			arrived.Done()
			<-release
			return true, nil
		}
	})
	secret, _ := mint(t, s, "alice", claude)
	writes := countLastUsedWrites(t, d)

	errs := make(chan error, n)
	for range n {
		go func() {
			_, err := verify(s, bearer(secret))
			errs <- err
		}()
	}
	arrived.Wait()
	close(release)
	for range n {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if got := writes(); got != 1 {
		t.Errorf("%d concurrent verifies wrote last_used %d times, want 1", n, got)
	}
}

// TestLastUsedNeverGoesBackwards has a slow request, which read the row
// at t0+61s, write after a later one already recorded t0+120s. The slow
// one must not overwrite the newer time.
func TestLastUsedNeverGoesBackwards(t *testing.T) {
	d := schema.Open(t)
	slowRead := make(chan struct{})
	fastDone := make(chan struct{})
	open := func(at time.Time, allowed func()) *apitoken.Store {
		s, err := apitoken.New(apitoken.Config{
			DB: d, Prefix: "tickets",
			Now:     func() time.Time { return at },
			Allowed: func(context.Context, string) (bool, error) { allowed(); return true, nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	minter := open(t0, func() {})
	secret, tok := mint(t, minter, "alice", claude)
	if _, err := verify(minter, bearer(secret)); err != nil {
		t.Fatal(err)
	}
	// slow reads the row (last_used t0), then waits for fast to finish.
	slow := open(t0.Add(61*time.Second), func() { close(slowRead); <-fastDone })
	// fast waits until slow has read, so both saw last_used = t0.
	fast := open(t0.Add(120*time.Second), func() { <-slowRead })

	slowErr := make(chan error, 1)
	go func() {
		_, err := verify(slow, bearer(secret))
		slowErr <- err
	}()
	if _, err := verify(fast, bearer(secret)); err != nil {
		t.Fatal(err)
	}
	close(fastDone)
	if err := <-slowErr; err != nil {
		t.Fatal(err)
	}
	var got string
	if err := d.QueryRow(`SELECT last_used FROM api_tokens WHERE id = ?`, tok.ID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if want := t0.Add(120 * time.Second).Format(time.RFC3339); got != want {
		t.Errorf("last_used = %s, want %s: a slower request moved it backwards", got, want)
	}
}

// TestCorruptRowIsUnavailable: a row this package could not have
// written is a storage failure. It must answer 503, not 401: a 401
// tells the agent its token is dead, and List must not hand the page a
// zero Access as if it were real.
func TestCorruptRowIsUnavailable(t *testing.T) {
	for _, c := range []struct{ name, column, value string }{
		{"unreadable expires_at", "expires_at", "soon"},
		{"expires_at in another zone", "expires_at", "2027-01-01T13:00:00+01:00"},
		{"unreadable last_used", "last_used", "yesterday"},
		{"last_used in another zone", "last_used", "2026-10-05T13:00:00+01:00"},
		{"unknown access", "access", "admin"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, d, _ := newStore(t, nil)
			secret, tok := mint(t, s, "alice", claude)
			if _, err := d.Exec(`UPDATE api_tokens SET `+c.column+` = ? WHERE id = ?`, c.value, tok.ID); err != nil {
				t.Fatal(err)
			}
			_, err := verify(s, bearer(secret))
			if !errors.Is(err, api.ErrUnavailable) || errors.Is(err, api.ErrUnauthorized) {
				t.Errorf("Verify err = %v, want api.ErrUnavailable only", err)
			}
			var after string
			if err := d.QueryRow(`SELECT COALESCE(`+c.column+`, '') FROM api_tokens WHERE id = ?`, tok.ID).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if after != c.value {
				t.Errorf("Verify rewrote %s from %q to %q", c.column, c.value, after)
			}
			if list, err := s.List(context.Background(), "alice"); err == nil {
				t.Errorf("List = %+v, nil; want an error for the corrupt row", list)
			}
		})
	}
}
