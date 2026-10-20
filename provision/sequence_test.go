package provision_test

// These tests hold an app to the record contract (the plan's
// "Decisions"): provision keeps no records, so the rules that keep a
// tenant repairable after a crash, a retry or two refreshes at once live
// in the app. The record type, signup and advance below are a worked
// example on objrec, local to the tests because the record is the
// app's: Plans 6 and 7 copy them. A failure here means some outcome of
// Create, Ready, PublishPaths or CheckRoute no longer maps to one record
// transition; fix that, not this file.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/objrec"
	"amadan.net/rastrillo/rastrillo/provision"
	"amadan.net/rastrillo/rastrillo/provision/provisiontest"
)

// tenantRecord is the app's record of one tenant: a home keys it by
// label (users/<uid>/instances/<label>.json), a hub by slug
// (tenants/<slug>.json).
type tenantRecord struct {
	Label string `json:"label"`
	// Host is written before the first Create and kept until Create says
	// ErrHostTaken: after any other outcome the instance may exist, and a
	// record that dropped the host would orphan it.
	Host string `json:"host"`
	// State is creating, live, failed or replaced.
	State string `json:"state"`
	// Routed is a hub's: people are sent only to a routed tenant.
	Routed bool `json:"routed"`
	// Relabels is persisted because a refresh is a new request: a counter
	// in memory would start again at 0 and relabel for ever.
	Relabels  int    `json:"relabels"`
	LastError string `json:"last_error,omitempty"`
	// Created is when the record was written, which the routing deadline
	// counts from.
	Created time.Time `json:"created"`
	// Attempt is the signup's token while its first Create may still be in
	// flight; empty once the signup handed the record over or a refresh
	// took it over. It is random so that no later write can reproduce the
	// body the signup wrote, and with it the version (objrec.Dir's version
	// is a hash of the body): the signup relabels only by a conditional
	// update on that version, so after any other write it cannot.
	Attempt string `json:"attempt,omitempty"`
	// AttemptUntil is the signup's deadline. Its Create runs under it, so
	// a refresh that waits for it does not post the host beside a Create
	// still in flight.
	AttemptUntil time.Time `json:"attempt_until"`
	// ReplacedBy, with State replaced, is the label a home's signup moved
	// this record to. A replaced record names the other creation's host,
	// so nothing resumes it; a refresh only finishes the move.
	ReplacedBy string `json:"replaced_by,omitempty"`
}

const (
	stateCreating = "creating"
	stateLive     = "live"
	stateFailed   = "failed"
	stateReplaced = "replaced"
)

// flow is a central app's provisioning, one request at a time. A home
// leaves product empty; a hub sets it and publishes and checks paths.
type flow struct {
	store   objrec.Store
	ten     *provision.Console // the tenant app's instance credential
	hub     *provision.Console // the hub app's publish credential
	hc      *http.Client       // for the probes
	domain  string             // the tenant app's instance domain
	product string
	// newLabel is provision.NewLabel; a test swaps it to collide on
	// purpose.
	newLabel func(slug string) (string, error)
	now      func() time.Time
	// bound is how long a signup may run: its Creates, at most
	// MaxRelabels+1 of them, and its writes. A refresh takes a record over
	// from its signup only after it.
	bound time.Duration
	// deadline is how long a live hub tenant may stay unrouted.
	deadline time.Duration
}

// errYielded is a signup that found its record taken over by a refresh.
// The refresh resumes the host; the signup shows "getting ready" as for any
// unknown outcome.
var errYielded = errors.New("a refresh took the record over")

// signup is the one request that writes the record and makes the first
// Create, the only one without Resume, and the only one that relabels.
// first is the first label; key gives a label's record key.
func (f *flow) signup(ctx context.Context, slug, first string, key func(label string) string) (provision.Created, error) {
	// The deadline is in the record before any Create runs under it, so a
	// refresh can tell a signup still at work from one that stopped.
	ctx, cancel := context.WithTimeout(ctx, f.bound)
	defer cancel()
	rec := tenantRecord{
		Label: first, Host: first + "." + f.domain, State: stateCreating, Created: f.now(),
		Attempt: newAttempt(), AttemptUntil: f.now().Add(f.bound),
	}
	// The record before the POST: a crash after the POST and before a
	// write would leave an instance no record names.
	ver, err := objrec.CreateJSON(ctx, f.store, key(rec.Label), rec)
	if err != nil {
		return provision.Created{}, err
	}
	for {
		created, err := f.ten.Create(ctx, provision.CreateRequest{Host: rec.Host})
		if !errors.Is(err, provision.ErrHostTaken) || rec.Relabels >= provision.MaxRelabels {
			return created, f.handOver(ctx, key(rec.Label), rec, ver, err)
		}
		next, err := f.newLabel(slug)
		if err != nil {
			return provision.Created{}, err
		}
		if rec, ver, err = f.relabel(ctx, key, rec, ver, next); err != nil {
			return provision.Created{}, err
		}
	}
}

// handOver ends the signup's attempt with the one write its outcome needs:
// failed, or creating with the token cleared, so the next refresh resumes
// at once instead of waiting out AttemptUntil.
func (f *flow) handOver(ctx context.Context, key string, rec tenantRecord, ver objrec.Version, err error) error {
	if err != nil && !transient(err) {
		rec.State, rec.LastError = stateFailed, err.Error()
	}
	rec.Attempt, rec.AttemptUntil = "", time.Time{}
	_, uerr := objrec.UpdateJSON(ctx, f.store, key, rec, ver)
	if errors.Is(uerr, objrec.ErrChanged) {
		// A refresh took the record over and reaches the same outcome by
		// itself.
		uerr = nil
	}
	return errors.Join(err, uerr)
}

// relabel moves the signup's record to the label next, only by a
// conditional update on the version the signup wrote. If a refresh took the
// record over, the instance this signup read as another creation's may be
// the one the refresh made: moving away from it would orphan it, so the
// signup yields and the record keeps its host.
func (f *flow) relabel(ctx context.Context, key func(string) string, rec tenantRecord, ver objrec.Version, next string) (tenantRecord, objrec.Version, error) {
	old := key(rec.Label)
	moved := rec
	moved.Label, moved.Host, moved.Relabels = next, next+"."+f.domain, rec.Relabels+1
	if key(next) == old {
		// Keyed by slug (a hub): the new host is one conditional update.
		v, err := objrec.UpdateJSON(ctx, f.store, old, moved, ver)
		return moved, v, yielded(err)
	}
	// Keyed by label (a home): the old record is marked replaced, by that
	// conditional update, before the new one exists. Left creating, a
	// failed delete or a crash would leave it naming the collision's host,
	// and a refresh would resume it and mark the other creation's instance
	// ours.
	gone := rec
	gone.State, gone.ReplacedBy, gone.Attempt, gone.AttemptUntil = stateReplaced, next, "", time.Time{}
	if _, err := objrec.UpdateJSON(ctx, f.store, old, gone, ver); err != nil {
		return rec, ver, yielded(err)
	}
	v, err := objrec.CreateJSON(ctx, f.store, key(next), moved)
	if err != nil {
		return rec, ver, err
	}
	// Last, so there is never a moment with no record. If it fails, the
	// replaced record stays until a refresh deletes it (finishReplace).
	if err := f.store.Delete(ctx, old); err != nil {
		return rec, ver, err
	}
	return moved, v, nil
}

func yielded(err error) error {
	if errors.Is(err, objrec.ErrChanged) {
		return errYielded
	}
	return err
}

func newAttempt() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// transient is an outcome to keep the record as it is and resume on the
// next refresh.
func transient(err error) bool {
	var re *provision.RetryError
	return errors.As(err, &re) || errors.Is(err, errYielded) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// advance is one refresh of the record at key: read, decide, and write the
// one transition with the version read. It returns the record as it left
// it and, when it called Create, Create's outcome.
func (f *flow) advance(ctx context.Context, key string) (tenantRecord, provision.Created, error) {
	for {
		rec, ver, err := objrec.GetJSON[tenantRecord](ctx, f.store, key)
		if err != nil {
			return rec, provision.Created{}, err
		}
		if rec.State == stateReplaced {
			return rec, provision.Created{}, f.finishReplace(ctx, key, rec)
		}
		if rec.State == stateCreating && rec.Attempt != "" {
			if f.now().Before(rec.AttemptUntil) {
				// The signup may still be in its Create, and only it may
				// relabel: a Create from here could make the instance it
				// then reads as another creation's.
				return rec, provision.Created{}, nil
			}
			// The signup stopped. Take the record over with a conditional
			// write before any Create, so a signup that was only slow can no
			// longer relabel away from what this refresh creates. The write
			// clears the token rather than writing a new one: a refresh never
			// relabels, so there is no authority to hold, and the next
			// refresh resumes without waiting.
			rec.Attempt, rec.AttemptUntil = "", time.Time{}
			ver, err = objrec.UpdateJSON(ctx, f.store, key, rec, ver)
			if errors.Is(err, objrec.ErrChanged) {
				continue
			}
			if err != nil {
				return rec, provision.Created{}, err
			}
		}
		next, created, stepErr := f.decide(ctx, key, rec)
		if next == rec {
			return rec, created, stepErr
		}
		_, err = objrec.UpdateJSON(ctx, f.store, key, next, ver)
		if errors.Is(err, objrec.ErrChanged) {
			// Another refresh moved it first: read again and decide again,
			// so the two together advance it once.
			continue
		}
		if err != nil {
			return rec, created, err
		}
		return next, created, stepErr
	}
}

// finishReplace completes a home's relabel that stopped part way: it
// creates the replacement record if the signup stopped before writing it,
// then deletes the replaced one. It never resumes the replaced host, which
// is the other creation's. The replacement's key is the home's
// users/<uid>/instances/<label>.json beside this one.
func (f *flow) finishReplace(ctx context.Context, key string, rec tenantRecord) error {
	_, err := objrec.CreateJSON(ctx, f.store, path.Join(path.Dir(key), rec.ReplacedBy+".json"), tenantRecord{
		Label: rec.ReplacedBy, Host: rec.ReplacedBy + "." + f.domain, State: stateCreating,
		Relabels: rec.Relabels + 1, Created: rec.Created,
	})
	if err != nil && !errors.Is(err, objrec.ErrExists) {
		return err
	}
	return f.store.Delete(ctx, key)
}

// decide is the state machine: what one refresh does to rec.
func (f *flow) decide(ctx context.Context, key string, rec tenantRecord) (tenantRecord, provision.Created, error) {
	switch {
	case rec.State == stateCreating:
		// Every call after the signup's first passes Resume: the host is
		// the operation's identity, so a GET decides whether it landed.
		created, err := f.ten.Create(ctx, provision.CreateRequest{Host: rec.Host, Resume: true})
		if err != nil {
			if !transient(err) {
				rec.State, rec.LastError = stateFailed, err.Error()
			}
			return rec, created, err
		}
		ready, _, err := provision.Ready(ctx, f.hc, rec.Host)
		if err != nil || !ready {
			return rec, created, err
		}
		rec.State = stateLive
		return rec, created, nil
	case rec.State == stateLive && f.product != "" && !rec.Routed:
		err := f.route(ctx, key, rec)
		if err == nil {
			rec.Routed = true
		} else if !f.now().Before(rec.Created.Add(f.deadline)) {
			// Whatever kept it unrouted, a failed publish and a probe that
			// cannot connect included: a deadline applied only to the
			// route's own states would leave a hub whose publishes or
			// probes keep erroring showing "getting ready" for ever.
			rec.State, rec.LastError = stateFailed, err.Error()
		}
		return rec, provision.Created{}, err
	}
	return rec, provision.Created{}, nil
}

// route publishes the map and checks the tenant's path once. nil means
// Routed; otherwise the error says what stopped it.
func (f *flow) route(ctx context.Context, key string, rec tenantRecord) error {
	if _, err := f.publish(ctx); err != nil {
		return err
	}
	slug := strings.TrimSuffix(strings.TrimPrefix(key, "tenants/"), ".json")
	r, err := provision.CheckRoute(ctx, f.hc, f.product, slug, rec.Host)
	if err != nil {
		return err
	}
	return r.Err()
}

// publish sends the map of every live tenant, routed or not: a tenant
// must be in the map before CheckRoute can pass, and a creating one has
// no instance serving yet.
func (f *flow) publish(ctx context.Context) (provision.Published, error) {
	return f.hub.PublishPaths(ctx, f.product, f.build)
}

// build reads every page of tenants/ and fails if any page or record
// cannot be read: a partial listing published would drop every tenant it
// missed. The page size is 1 so the tests cross page boundaries.
func (f *flow) build(ctx context.Context) (map[string]string, error) {
	paths := map[string]string{}
	cursor := ""
	for {
		keys, next, err := f.store.List(ctx, "tenants/", cursor, 1)
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			rec, _, err := objrec.GetJSON[tenantRecord](ctx, f.store, k)
			if err != nil {
				return nil, err
			}
			if rec.State == stateLive {
				paths[strings.TrimSuffix(strings.TrimPrefix(k, "tenants/"), ".json")] = rec.Host
			}
		}
		if next == "" {
			return paths, nil
		}
		cursor = next
	}
}

// watched wraps a Store so a test can see or bend its calls.
type watched struct {
	objrec.Store
	// before runs before each Create, Update and Delete; an error fails
	// that call before it reaches the store, as a crash or a store outage
	// at that point would.
	before func(op, key string) error
	// created runs after each successful Create.
	created func(key string)
	// read runs after each Get, before it returns.
	read func(key string)
	// updated runs after each successful Update.
	updated func(key string, body []byte)
	// list runs before each List; an error fails that List.
	list func(cursor string) error
}

func (w *watched) check(op, key string) error {
	if w.before == nil {
		return nil
	}
	return w.before(op, key)
}

func (w *watched) Get(ctx context.Context, key string) ([]byte, objrec.Version, error) {
	b, v, err := w.Store.Get(ctx, key)
	if w.read != nil {
		w.read(key)
	}
	return b, v, err
}

func (w *watched) Create(ctx context.Context, key string, body []byte) (objrec.Version, error) {
	if err := w.check("create", key); err != nil {
		return "", err
	}
	v, err := w.Store.Create(ctx, key, body)
	if err == nil && w.created != nil {
		w.created(key)
	}
	return v, err
}

func (w *watched) Update(ctx context.Context, key string, body []byte, ver objrec.Version) (objrec.Version, error) {
	if err := w.check("update", key); err != nil {
		return "", err
	}
	v, err := w.Store.Update(ctx, key, body, ver)
	if err == nil && w.updated != nil {
		w.updated(key, body)
	}
	return v, err
}

func (w *watched) Delete(ctx context.Context, key string) error {
	if err := w.check("delete", key); err != nil {
		return err
	}
	return w.Store.Delete(ctx, key)
}

func (w *watched) List(ctx context.Context, prefix, cursor string, limit int) ([]string, string, error) {
	if w.list != nil {
		if err := w.list(cursor); err != nil {
			return nil, "", err
		}
	}
	return w.Store.List(ctx, prefix, cursor, limit)
}

// clock is a test's time, moved by hand once per refresh.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) tick()          { c.pass(time.Minute) }
func (c *clock) pass(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// homeFlow is a home: bdf/tenant enabled on domain, its records in a
// fresh Dir.
func homeFlow(t *testing.T) (*provisiontest.Platform, *flow, *watched, *clock) {
	t.Helper()
	p := provisiontest.New()
	p.Serve("bdf", "tenant", provisiontest.Tenant(""))
	st := &watched{Store: objrec.Dir(t.TempDir())}
	c := &clock{t: time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)}
	return p, &flow{
		store: st, ten: newConsole(t, tenantConfig(p)), hc: p.Client(), domain: domain,
		newLabel: provision.NewLabel, now: c.now, bound: signupBound,
	}, st, c
}

// signupBound is the tests' signup deadline. It is real time for the
// signup's context, far longer than any fake call, and fake time for the
// record's AttemptUntil, which only the test's clock moves past.
const signupBound = 2 * time.Minute

func homeKey(label string) string { return "users/u1/instances/" + label + ".json" }

func getRecord(t *testing.T, s objrec.Store, key string) tenantRecord {
	t.Helper()
	rec, _, err := objrec.GetJSON[tenantRecord](context.Background(), s, key)
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	return rec
}

// keys lists every record under prefix.
func keys(t *testing.T, s objrec.Store, prefix string) []string {
	t.Helper()
	ks, _, err := s.List(context.Background(), prefix, "", 0)
	if err != nil {
		t.Fatalf("List %s: %v", prefix, err)
	}
	return ks
}

// advanceUntil refreshes key up to n times until its record is in state.
func advanceUntil(t *testing.T, f *flow, key, state string, n int) tenantRecord {
	t.Helper()
	var rec tenantRecord
	for i := 0; i < n; i++ {
		var err error
		rec, _, err = f.advance(context.Background(), key)
		if rec.State == state {
			return rec
		}
		if err != nil && !transient(err) {
			t.Fatalf("refresh %d: %v (record %+v)", i+1, err, rec)
		}
	}
	t.Fatalf("after %d refreshes the record is %+v, want %s", n, rec, state)
	return rec
}

func firstPost(calls []string) int {
	for i, c := range calls {
		if strings.HasPrefix(c, "POST ") {
			return i
		}
	}
	return -1
}

func TestHomeSequenceWithRecords(t *testing.T) {
	p, f, st, _ := homeFlow(t)
	label, err := provision.NewLabel("acme")
	if err != nil {
		t.Fatal(err)
	}
	if label == "" {
		t.Fatal(`NewLabel("acme") = ""`)
	}
	callsAtWrite := -1
	st.created = func(string) { callsAtWrite = len(p.Calls()) }
	p.Unready(3)
	key := homeKey(label)
	if _, err := f.signup(context.Background(), "acme", label, homeKey); err != nil {
		t.Fatalf("signup: %v", err)
	}
	if got := getRecord(t, st, key); got.State != stateCreating || got.Host != label+"."+domain {
		t.Fatalf("after signup the record is %+v, want creating at %s.%s", got, label, domain)
	}
	if i := firstPost(p.Calls()); callsAtWrite < 0 || i < callsAtWrite {
		t.Errorf("the POST is call %d and the record was written after %d calls: write the record first", i, callsAtWrite)
	}
	// Unready(3): the first three refreshes meet the pending page and must
	// leave the record creating; the fourth sees the instance.
	var rec tenantRecord
	for i := 1; i <= 4; i++ {
		rec, _, err = f.advance(context.Background(), key)
		if err != nil {
			t.Fatalf("refresh %d: %v", i, err)
		}
		want := stateCreating
		if i == 4 {
			want = stateLive
		}
		if rec.State != want {
			t.Fatalf("refresh %d: record %+v, want %s", i, rec, want)
		}
	}
	if rec.Host != label+"."+domain || rec.LastError != "" {
		t.Errorf("live record = %+v, want the signup's host and no error", rec)
	}
	if got := p.Instances("bdf", "tenant"); !reflect.DeepEqual(got, []string{rec.Host}) {
		t.Errorf("instances = %v, want just %s", got, rec.Host)
	}
	if n := posts(p); n != 1 {
		t.Errorf("%d POSTs, want 1: every refresh resumes", n)
	}
}

func TestHomeSequenceRelabels(t *testing.T) {
	p, f, st, _ := homeFlow(t)
	ctx := context.Background()
	taken := "acme-taken." + domain
	if _, err := f.ten.Create(ctx, provision.CreateRequest{Host: taken}); err != nil {
		t.Fatalf("another creation: %v", err)
	}
	// The old record is marked replaced, then the new one is created, then
	// the old one goes. Deleting it first would leave a moment, or after a
	// crash for good, with no record at all; leaving it creating would let
	// a refresh resume the collision's host.
	oldKey, oldState := homeKey("acme-taken"), ""
	st.created = func(key string) {
		if key != oldKey {
			if rec, _, err := objrec.GetJSON[tenantRecord](ctx, st.Store, oldKey); err == nil {
				oldState = rec.State
			}
		}
	}
	created, err := f.signup(ctx, "acme", "acme-taken", homeKey)
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	if created.Host == "" || created.Host == taken {
		t.Fatalf("signup created %q, want a new host after ErrHostTaken", created.Host)
	}
	label := strings.TrimSuffix(created.Host, "."+domain)
	if got := keys(t, st, "users/u1/instances/"); !reflect.DeepEqual(got, []string{homeKey(label)}) {
		t.Fatalf("records = %v, want only the new label's (the old one deleted)", got)
	}
	if oldState != stateReplaced {
		t.Errorf("when the new record was created the old one was %q, want %q: mark it replaced, create the new one, then delete the old", oldState, stateReplaced)
	}
	st.created = nil
	for i := 0; i < 2; i++ {
		rec, _, err := f.advance(ctx, homeKey(label))
		if err != nil {
			t.Fatalf("refresh %d: %v", i+1, err)
		}
		if rec.Relabels != 1 || rec.State != stateLive {
			t.Errorf("refresh %d: record %+v, want live with Relabels 1", i+1, rec)
		}
	}

	// Every label collides: the counter in the record stops it.
	tries := []string{"globex-taken", "globex-taken1", "globex-taken2"}
	for _, l := range tries {
		if _, err := f.ten.Create(ctx, provision.CreateRequest{Host: l + "." + domain}); err != nil {
			t.Fatal(err)
		}
	}
	next := 1
	f.newLabel = func(string) (string, error) { next++; return tries[next-1], nil }
	before := posts(p)
	_, err = f.signup(ctx, "globex", tries[0], homeKey)
	if !errors.Is(err, provision.ErrHostTaken) {
		t.Fatalf("signup err = %v, want ErrHostTaken after MaxRelabels", err)
	}
	rec := getRecord(t, st, homeKey(tries[provision.MaxRelabels]))
	if rec.State != stateFailed || rec.Relabels != provision.MaxRelabels || rec.LastError == "" {
		t.Errorf("record = %+v, want failed with Relabels %d and the error's text", rec, provision.MaxRelabels)
	}
	if n := posts(p) - before; n != provision.MaxRelabels+1 {
		t.Errorf("%d POSTs, want %d: the first label and %d new ones", n, provision.MaxRelabels+1, provision.MaxRelabels)
	}
}

func TestHomeSequenceNotEnabledFails(t *testing.T) {
	p, f, st, _ := homeFlow(t)
	cfg := tenantConfig(p)
	cfg.App, cfg.Token = "dormant", p.Credential("bdf", "dormant", "instance")
	f.ten = newConsole(t, cfg)
	f.newLabel = func(string) (string, error) { t.Error("relabelled an app that is not enabled"); return "x", nil }
	_, err := f.signup(context.Background(), "acme", "acme-a1b2c3", homeKey)
	if !errors.Is(err, provision.ErrConflict) {
		t.Fatalf("signup err = %v, want ErrConflict", err)
	}
	rec := getRecord(t, st, homeKey("acme-a1b2c3"))
	if rec.State != stateFailed || !strings.Contains(rec.LastError, "instances are not enabled") {
		t.Errorf("record = %+v, want failed with the platform's sentence", rec)
	}
	if got := keys(t, st, "users/u1/instances/"); len(got) != 1 {
		t.Errorf("records = %v, want the one", got)
	}
	if n := posts(p); n != 1 {
		t.Errorf("%d POSTs, want 1", n)
	}
}

func TestHomeSequenceResumesAfterACrash(t *testing.T) {
	p, f, st, _ := homeFlow(t)
	working := f.ten
	cfg := tenantConfig(p)
	cfg.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/instances/") {
			return nil, errors.New("connection reset")
		}
		return p.RoundTrip(r)
	})}
	f.ten = newConsole(t, cfg)
	p.Script(provisiontest.Reply{Hang: true, Record: true})
	_, err := f.signup(context.Background(), "acme", "acme-a1b2c3", homeKey)
	var re *provision.RetryError
	if !errors.As(err, &re) {
		t.Fatalf("signup err = %v, want *RetryError: the reconcile could not see the write", err)
	}
	key := homeKey("acme-a1b2c3")
	host := "acme-a1b2c3." + domain
	if rec := getRecord(t, st, key); rec.State != stateCreating || rec.Host != host {
		t.Fatalf("record = %+v, want creating with host %s kept", rec, host)
	}

	f.ten = working
	rec, created, err := f.advance(context.Background(), key)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if !created.Landed {
		t.Errorf("refresh's Create = %+v, want Landed", created)
	}
	if rec.State != stateLive {
		t.Errorf("record = %+v, want live", rec)
	}
	if got := p.Instances("bdf", "tenant"); !reflect.DeepEqual(got, []string{host}) {
		t.Errorf("instances = %v, want exactly %s", got, host)
	}
}

func TestHomeSequenceAcceptedThenServes(t *testing.T) {
	p, f, st, _ := homeFlow(t)
	p.Script(provisiontest.Reply{Status: http.StatusAccepted, Record: true})
	p.Unready(2)
	created, err := f.signup(context.Background(), "acme", "acme-a1b2c3", homeKey)
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	if !created.Accepted {
		t.Fatalf("signup's Create = %+v, want Accepted", created)
	}
	key := homeKey("acme-a1b2c3")
	if rec := getRecord(t, st, key); rec.State != stateCreating {
		t.Fatalf("record = %+v, want creating until Ready", rec)
	}
	advanceUntil(t, f, key, stateLive, 4)
	if n := posts(p); n != 1 {
		t.Errorf("%d POSTs, want 1: an accepted create is not sent again", n)
	}
}

func TestHomeSequenceCrashBeforeThePost(t *testing.T) {
	p, f, st, c := homeFlow(t)
	key := homeKey("acme-a1b2c3")
	host := "acme-a1b2c3." + domain
	// The signup wrote the record, then the process stopped.
	if _, err := objrec.CreateJSON(context.Background(), st, key, tenantRecord{
		Label: "acme-a1b2c3", Host: host, State: stateCreating,
		Attempt: "signup", AttemptUntil: c.now().Add(signupBound),
	}); err != nil {
		t.Fatal(err)
	}
	// Until the signup's deadline nothing can tell a stopped signup from a
	// slow one, so a refresh leaves the record to it.
	if rec, _, err := f.advance(context.Background(), key); err != nil || rec.Attempt != "signup" {
		t.Fatalf("refresh before the deadline: %+v, %v; want the record untouched", rec, err)
	}
	if calls := p.Calls(); len(calls) != 0 {
		t.Fatalf("calls before the deadline = %v, want none", calls)
	}
	c.pass(signupBound)
	rec := advanceUntil(t, f, key, stateLive, 1)
	if rec.Host != host {
		t.Errorf("record host = %s, want %s", rec.Host, host)
	}
	calls := p.Calls()
	if i := firstPost(calls); i < 1 || !strings.HasPrefix(calls[0], "GET ") || posts(p) != 1 {
		t.Errorf("calls = %v, want a GET that finds nothing, then one POST", calls)
	}
}

func TestTwoRefreshesAdvanceOnce(t *testing.T) {
	p, f, st, _ := homeFlow(t)
	p.Script(provisiontest.Reply{Status: 502}, provisiontest.Reply{Status: 502})
	key := homeKey("acme-a1b2c3")
	if _, err := f.signup(context.Background(), "acme", "acme-a1b2c3", homeKey); !transient(err) {
		t.Fatalf("signup err = %v, want an unknown outcome", err)
	}

	// Hold the first two reads until both have happened, so the two
	// refreshes decide from the same version.
	var arrived sync.WaitGroup
	arrived.Add(2)
	var mu sync.Mutex
	reads, liveWrites := 0, 0
	st.read = func(string) {
		mu.Lock()
		reads++
		first := reads <= 2
		mu.Unlock()
		if first {
			arrived.Done()
			arrived.Wait()
		}
	}
	st.updated = func(_ string, body []byte) {
		var rec tenantRecord
		if json.Unmarshal(body, &rec) == nil && rec.State == stateLive {
			mu.Lock()
			liveWrites++
			mu.Unlock()
		}
	}

	errs := make([]error, 2)
	var done sync.WaitGroup
	for i := range errs {
		done.Add(1)
		go func() {
			defer done.Done()
			_, _, errs[i] = f.advance(context.Background(), key)
		}()
	}
	done.Wait()

	for i, err := range errs {
		if errors.Is(err, provision.ErrHostTaken) {
			t.Errorf("refresh %d: ErrHostTaken: a resumed host is the operation's own", i+1)
		} else if err != nil {
			t.Errorf("refresh %d: %v", i+1, err)
		}
	}
	if got := p.Instances("bdf", "tenant"); len(got) != 1 {
		t.Errorf("instances = %v, want exactly one", got)
	}
	if liveWrites != 1 {
		t.Errorf("%d writes set live, want exactly 1", liveWrites)
	}
	if rec := getRecord(t, st, key); rec.State != stateLive {
		t.Errorf("record = %+v, want live", rec)
	}
}

// hubFlow is a hub: bdf/hub holds product.test, bdf/tenant is enabled on
// domain, and each tenant instance answers under its own slug.
func hubFlow(t *testing.T) (*provisiontest.Platform, *flow, *watched, *clock) {
	t.Helper()
	p := provisiontest.New()
	p.Release("bdf", "hub", "v1")
	p.Release("bdf", "tenant", "v2")
	p.Serve("bdf", "hub", provisiontest.Hub())
	p.Serve("bdf", "tenant", func(host string) http.Handler {
		// The slug is the label without NewLabel's "-" and suffix.
		label, _, _ := strings.Cut(host, ".")
		return provisiontest.Tenant(label[:strings.LastIndexByte(label, '-')])(host)
	})
	st := &watched{Store: objrec.Dir(t.TempDir())}
	c := &clock{t: time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)}
	return p, &flow{
		store: st, ten: newConsole(t, tenantConfig(p)), hub: newConsole(t, hubConfig(p)),
		hc: p.Client(), domain: domain, product: product,
		newLabel: provision.NewLabel, now: c.now, bound: signupBound, deadline: 3 * time.Minute,
	}, st, c
}

func hubKey(slug string) func(string) string {
	return func(string) string { return "tenants/" + slug + ".json" }
}

// hubSignup signs slug up under a fresh label.
func hubSignup(t *testing.T, f *flow, slug string) string {
	t.Helper()
	label, err := provision.NewLabel(slug)
	if err != nil || label == "" {
		t.Fatalf("NewLabel(%q) = %q, %v", slug, label, err)
	}
	if _, err := f.signup(context.Background(), slug, label, hubKey(slug)); err != nil {
		t.Fatalf("signup %s: %v", slug, err)
	}
	return label + "." + domain
}

func TestHubSequenceWithRecords(t *testing.T) {
	p, f, st, c := hubFlow(t)
	p.Unready(1)
	hosts := map[string]string{"acme": hubSignup(t, f, "acme")}
	p.Unready(2)
	hosts["globex"] = hubSignup(t, f, "globex")

	for i := 0; i < 6; i++ {
		c.tick()
		for _, slug := range []string{"acme", "globex"} {
			if _, _, err := f.advance(context.Background(), "tenants/"+slug+".json"); err != nil &&
				!errors.Is(err, provision.ErrNotRouted) && !transient(err) {
				t.Fatalf("refresh %d of %s: %v", i+1, slug, err)
			}
		}
	}
	for slug, host := range hosts {
		rec := getRecord(t, st, "tenants/"+slug+".json")
		if rec.State != stateLive || !rec.Routed || rec.Host != host {
			t.Errorf("%s: record %+v, want live and routed at %s", slug, rec, host)
		}
	}
	if got := p.Paths(product); !reflect.DeepEqual(got, hosts) {
		t.Errorf("published map = %v, want %v", got, hosts)
	}
}

func TestHubLeavesCreatingTenantsOut(t *testing.T) {
	p, f, st, _ := hubFlow(t)
	acme := hubSignup(t, f, "acme")
	p.Unready(100)
	hubSignup(t, f, "globex")

	rec, _, err := f.advance(context.Background(), "tenants/acme.json")
	if err != nil || rec.State != stateLive {
		t.Fatalf("acme refresh: %+v, %v; want live", rec, err)
	}
	rec, _, err = f.advance(context.Background(), "tenants/acme.json")
	if err != nil || !rec.Routed {
		t.Fatalf("acme refresh: %+v, %v; want routed", rec, err)
	}
	if got := getRecord(t, st, "tenants/globex.json"); got.State != stateCreating {
		t.Fatalf("globex is %+v, want still creating", got)
	}
	if got, want := p.Paths(product), map[string]string{"acme": acme}; !reflect.DeepEqual(got, want) {
		t.Errorf("published map = %v, want %v: a creating tenant is left out", got, want)
	}
}

func TestHubBuildFailsOnALaterPage(t *testing.T) {
	p, f, st, _ := hubFlow(t)
	ctx := context.Background()
	hubSignup(t, f, "acme")
	hubSignup(t, f, "globex")
	for _, slug := range []string{"acme", "globex"} {
		advanceUntil(t, f, "tenants/"+slug+".json", stateLive, 1)
	}
	if _, err := f.publish(ctx); err != nil {
		t.Fatalf("publish: %v", err)
	}
	// A third live tenant: a build that read every page would change the
	// map, so an unchanged map below means nothing was sent.
	hubSignup(t, f, "initech")
	advanceUntil(t, f, "tenants/initech.json", stateLive, 1)
	held, putsBefore := p.Paths(product), puts(p)

	broken := errors.New("listing failed")
	st.list = func(cursor string) error {
		if cursor != "" {
			return broken
		}
		return nil
	}
	if _, err := f.publish(ctx); !errors.Is(err, broken) {
		t.Fatalf("publish err = %v, want the listing's error", err)
	}
	if n := puts(p) - putsBefore; n != 0 {
		t.Errorf("%d PUTs after a failed build, want 0", n)
	}
	if got := p.Paths(product); !reflect.DeepEqual(got, held) {
		t.Errorf("published map = %v, want it unchanged at %v", got, held)
	}
}

func TestHubSequenceMarksAFallThroughFailed(t *testing.T) {
	p, f, st, c := hubFlow(t)
	hubSignup(t, f, "acme")
	globex := hubSignup(t, f, "globex")
	p.OtherBox(globex)

	for i := 1; i <= 3; i++ {
		c.tick()
		for _, slug := range []string{"acme", "globex"} {
			f.advance(context.Background(), "tenants/"+slug+".json")
		}
		rec := getRecord(t, st, "tenants/globex.json")
		if i < 3 && rec.State != stateLive {
			t.Fatalf("refresh %d: globex is %+v, want live until the deadline", i, rec)
		}
		if i == 3 && (rec.State != stateFailed || rec.LastError != provision.ErrFellThrough.Error()) {
			t.Errorf("at the deadline globex is %+v, want failed with %q", rec, provision.ErrFellThrough)
		}
		if rec.Routed {
			t.Errorf("refresh %d: globex routed, but its path falls through", i)
		}
	}
	if rec := getRecord(t, st, "tenants/acme.json"); rec.State != stateLive || !rec.Routed {
		t.Errorf("acme = %+v, want live and routed", rec)
	}
	if got := p.Paths(product); got["acme"] == "" {
		t.Errorf("published map = %v, want acme in it", got)
	}
}

// TestHubSequenceFailsAtTheDeadlineOnErrors holds the deadline on the error
// paths too: a publish or a check that fails on every refresh must still
// end the tenant failed, or a hub whose path map or probes keep erroring
// shows "getting ready" for ever.
func TestHubSequenceFailsAtTheDeadlineOnErrors(t *testing.T) {
	refused := errors.New("connection refused")
	for _, tc := range []struct {
		name  string
		spoil func(p *provisiontest.Platform, f *flow)
	}{
		{"check", func(p *provisiontest.Platform, f *flow) {
			// The tenant's own host still answers, so it goes live; every
			// probe of the product path fails.
			f.hc = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Hostname() == product {
					return nil, refused
				}
				return p.RoundTrip(r)
			})}
		}},
		{"publish", func(p *provisiontest.Platform, _ *flow) {
			for i := 0; i < 10; i++ {
				p.ScriptPaths(provisiontest.Reply{Status: http.StatusTooManyRequests})
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, f, st, c := hubFlow(t)
			hubSignup(t, f, "acme")
			tc.spoil(p, f)
			for i := 1; i <= 3; i++ {
				c.tick()
				_, _, err := f.advance(context.Background(), "tenants/acme.json")
				rec := getRecord(t, st, "tenants/acme.json")
				switch {
				case i == 1 && rec.State != stateLive:
					t.Fatalf("refresh 1: %+v, %v; want live", rec, err)
				case i == 2 && (err == nil || rec.State != stateLive):
					t.Fatalf("refresh 2: %+v, %v; want live and an error before the deadline", rec, err)
				case i == 3 && (err == nil || rec.State != stateFailed || rec.LastError != err.Error()):
					t.Errorf("at the deadline: %+v, %v; want failed with the error's text", rec, err)
				}
				if rec.Routed {
					t.Errorf("refresh %d: routed, but nothing reached the tenant", i)
				}
			}
		})
	}
}

// pauseAfterWrite holds the signup once its record at key is written, before
// its first POST, until release is called.
func pauseAfterWrite(st *watched, key string) (paused <-chan struct{}, release func()) {
	at, gate := make(chan struct{}), make(chan struct{})
	var once sync.Once
	st.created = func(k string) {
		if k == key {
			once.Do(func() { close(at); <-gate })
		}
	}
	return at, func() { close(gate) }
}

// TestSignupPausedWhileARefreshRuns is the race the attempt token closes. A
// signup writes its record and stalls before its POST; a refresh runs.
// Before the signup's deadline the refresh leaves the record alone, since
// only the signup may relabel and a Create from the refresh would make the
// instance the signup then reads as another creation's. After it, the
// refresh takes the record over by a conditional write and creates the
// instance; the signup, released, meets that instance as a 409, and must
// yield rather than relabel away from it and orphan it.
func TestSignupPausedWhileARefreshRuns(t *testing.T) {
	for _, tc := range []struct {
		name      string
		hub, late bool
	}{
		{"home before the signup's deadline", false, false},
		{"home after the signup's deadline", false, true},
		{"hub before the signup's deadline", true, false},
		{"hub after the signup's deadline", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var (
				p   *provisiontest.Platform
				f   *flow
				st  *watched
				c   *clock
				key = homeKey
			)
			prefix := "users/u1/instances/"
			if tc.hub {
				p, f, st, c = hubFlow(t)
				key, prefix = hubKey("acme"), "tenants/"
			} else {
				p, f, st, c = homeFlow(t)
			}
			ctx := context.Background()
			first := "acme-a1b2c3"
			host, k := first+"."+domain, key(first)
			f.newLabel = func(string) (string, error) { return "acme-n3w0ne", nil }

			paused, release := pauseAfterWrite(st, k)
			done := make(chan error, 1)
			go func() {
				_, err := f.signup(ctx, "acme", first, key)
				done <- err
			}()
			<-paused

			if tc.late {
				c.pass(signupBound)
				// The refresh's instance is not serving yet, so the record
				// it leaves is creating: nothing but the takeover marks it.
				p.Unready(1)
			}
			rec, _, err := f.advance(ctx, k)
			switch {
			case err != nil:
				t.Fatalf("refresh: %v", err)
			case !tc.late && (rec.Attempt == "" || len(p.Calls()) != 0):
				t.Fatalf("refresh before the deadline: record %+v, calls %v; want it left to the signup, no calls", rec, p.Calls())
			case tc.late && (rec.State != stateCreating || rec.Attempt != "" || posts(p) != 1):
				// Not fatal: without the takeover the signup's relabel
				// below goes through, and the orphan it leaves is the
				// finding.
				t.Errorf("refresh after the deadline: record %+v, %d POSTs; want it taken over and created", rec, posts(p))
			}

			release()
			err = <-done
			if tc.late && !errors.Is(err, errYielded) {
				t.Errorf("signup err = %v, want errYielded: a signup whose record was taken over must not relabel", err)
			} else if !tc.late && err != nil {
				t.Errorf("signup: %v", err)
			}
			if got := keys(t, st, prefix); !reflect.DeepEqual(got, []string{k}) {
				t.Fatalf("records = %v, want only %s", got, k)
			}
			rec = advanceUntil(t, f, k, stateLive, 3)
			if rec.Host != host || rec.Relabels != 0 {
				t.Errorf("record = %+v, want live at %s, never relabelled", rec, host)
			}
			if got := p.Instances("bdf", "tenant"); !reflect.DeepEqual(got, []string{host}) {
				t.Errorf("instances = %v, want exactly %s: any other is an orphan", got, host)
			}
		})
	}
}

// TestHomeRelabelInterrupted stops a home's relabel part way. The old
// record names the collision's host, an instance of another creation, so
// recovery must never resume it: it deletes it once the replacement exists,
// creating the replacement first if the signup stopped before it.
func TestHomeRelabelInterrupted(t *testing.T) {
	for _, tc := range []struct{ name, op string }{
		// A failed delete leaves the store as a crash between the new
		// record and the delete would: signup stops at either.
		{"the old record's delete fails", "delete"},
		{"the process stops before the new record", "create"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, f, st, c := homeFlow(t)
			ctx := context.Background()
			taken := "acme-taken." + domain
			if _, err := f.ten.Create(ctx, provision.CreateRequest{Host: taken}); err != nil {
				t.Fatalf("another creation: %v", err)
			}
			oldKey, next := homeKey("acme-taken"), "acme-n3w0ne"
			newKey, newHost := homeKey(next), next+"."+domain
			f.newLabel = func(string) (string, error) { return next, nil }
			stopped := errors.New("the store write did not happen")
			failAt := map[string]string{"delete": oldKey, "create": newKey}[tc.op]
			st.before = func(op, key string) error {
				if op == tc.op && key == failAt {
					return stopped
				}
				return nil
			}
			if _, err := f.signup(ctx, "acme", "acme-taken", homeKey); !errors.Is(err, stopped) {
				t.Fatalf("signup err = %v, want the store's", err)
			}
			st.before = nil
			// Not fatal: the refresh below is what a record left creating
			// goes wrong in.
			if rec := getRecord(t, st, oldKey); rec.State != stateReplaced || rec.ReplacedBy != next {
				t.Errorf("old record = %+v, want replaced by %s", rec, next)
			}
			afterSignup := len(p.Calls())

			// What a home's page does: refresh every record it lists.
			rec, created, err := f.advance(ctx, oldKey)
			if err != nil || created.Landed || rec.State == stateLive {
				t.Fatalf("refresh of the old record: %+v, %+v, %v; want it finished, never resumed", rec, created, err)
			}
			if _, _, err := st.Get(ctx, oldKey); !errors.Is(err, objrec.ErrNotFound) {
				t.Errorf("the replaced record is still there after a refresh (%v)", err)
			}
			c.pass(signupBound)
			rec = advanceUntil(t, f, newKey, stateLive, 2)
			if rec.Host != newHost || rec.Relabels != 1 {
				t.Errorf("new record = %+v, want live at %s with Relabels 1", rec, newHost)
			}
			if got := keys(t, st, "users/u1/instances/"); !reflect.DeepEqual(got, []string{newKey}) {
				t.Errorf("records = %v, want only %s", got, newKey)
			}
			for _, call := range p.Calls()[afterSignup:] {
				if strings.Contains(call, taken) {
					t.Errorf("recovery asked about the collision's host: %s", call)
				}
			}
			if got := p.Instances("bdf", "tenant"); !reflect.DeepEqual(got, []string{taken, newHost}) {
				t.Errorf("instances = %v, want the other creation's and %s", got, newHost)
			}
		})
	}
}
