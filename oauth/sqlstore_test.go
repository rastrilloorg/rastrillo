package oauth_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/dbtest"
	"amadan.net/rastrillo/rastrillo/oauth"
)

var schema = dbtest.FromSet(oauth.Schema)

func TestMain(suite *testing.M) {
	code := suite.Run()
	schema.Remove()
	tokenSchema.Remove()
	os.Exit(code)
}

var t0 = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

func newStore(t *testing.T) oauth.Store {
	t.Helper()
	return oauth.NewSQLStore(schema.Open(t))
}

func connection(id string) oauth.Connection {
	return oauth.Connection{
		ID: id, Subject: "ana", ClientID: "https://c.example/id",
		ClientName: "Agent", ReturnHost: "c.example",
		Resource: "https://app.example/mcp", Access: rastrillo.Write,
		Created: t0, LastUsed: t0,
	}
}

func code(hash string, connection string) oauth.Code {
	return oauth.Code{
		Hash: []byte(hash), Connection: connection,
		ClientID: "https://c.example/id", Redirect: "https://c.example/cb",
		Challenge: "chal", Resource: "https://app.example/mcp",
		Expires: t0.Add(time.Minute),
	}
}

func refresh(hash, connection string) oauth.Refresh {
	return oauth.Refresh{
		Hash: []byte(hash), Connection: connection,
		Expires: t0.Add(30 * 24 * time.Hour),
	}
}

func TestClientRoundTrip(t *testing.T) {
	store, ctx := newStore(t), context.Background()
	want := oauth.Client{
		ID: "https://c.example/id", Name: "Agent", Document: true,
		Redirects:    []string{"https://c.example/cb", "http://127.0.0.1/cb"},
		RegisteredAt: t0, UsedAt: t0.Add(time.Hour),
	}
	if err := store.SaveClient(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Client(ctx, want.ID)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, %v; want %+v", got, err, want)
	}
	want.Name, want.UsedAt = "Renamed", t0.Add(2*time.Hour)
	if err := store.SaveClient(ctx, want); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.Client(ctx, want.ID); got.Name != "Renamed" ||
		!got.UsedAt.Equal(want.UsedAt) {
		t.Fatalf("resave: %+v", got)
	}
	if _, err := store.Client(ctx, "nope"); !errors.Is(err, oauth.ErrNotFound) {
		t.Fatalf("missing client: %v", err)
	}
}

func TestPruneClients(t *testing.T) {
	store, ctx := newStore(t), context.Background()
	for id, used := range map[string]time.Time{
		"old": t0.Add(-40 * 24 * time.Hour), "new": t0,
	} {
		client := oauth.Client{
			ID:           id,
			Name:         id,
			RegisteredAt: used,
			UsedAt:       used,
		}
		if err := store.SaveClient(ctx, client); err != nil {
			t.Fatal(err)
		}
	}
	busy := oauth.Client{
		ID: "busy", Name: "busy", UsedAt: t0.Add(-40 * 24 * time.Hour),
		RegisteredAt: t0.Add(-40 * 24 * time.Hour),
	}
	if err := store.SaveClient(ctx, busy); err != nil {
		t.Fatal(err)
	}
	conn := connection("c1")
	conn.ClientID = "busy"
	if err := store.CreateConnection(ctx, conn, code("h1", "c1")); err != nil {
		t.Fatal(err)
	}
	if err := store.PruneClients(ctx, t0.Add(-30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Client(ctx, "old"); !errors.Is(err, oauth.ErrNotFound) {
		t.Fatalf("old client survived: %v", err)
	}
	if _, err := store.Client(ctx, "new"); err != nil {
		t.Fatalf("new client pruned: %v", err)
	}
	if _, err := store.Client(ctx, "busy"); err != nil {
		t.Fatalf("client with a live connection pruned: %v", err)
	}
}

func pending() oauth.Pending {
	return oauth.Pending{
		ID: "p1", Subject: "ana", ClientID: "c", ClientName: "Agent",
		Redirect: "https://c/cb", Challenge: "chal",
		Resource: "https://app/mcp", State: "st", StateSent: true,
		Return: "/oauth/authorize?x=1", Requested: rastrillo.Read,
		Expires: t0.Add(10 * time.Minute),
	}
}

func TestPendingRoundTrip(t *testing.T) {
	store, ctx := newStore(t), context.Background()
	want := pending()
	if err := store.SavePending(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Pending(ctx, "p1", "ana", t0)
	if err != nil || got != want {
		t.Fatalf("Pending: %+v, %v", got, err)
	}
	got, err = store.TakePending(ctx, "p1", "ana", t0)
	if err != nil || got != want {
		t.Fatalf("TakePending: %+v, %v", got, err)
	}
	if _, err := store.TakePending(ctx, "p1", "ana", t0); !errors.Is(
		err,
		oauth.ErrNotFound,
	) {
		t.Fatalf("second take: %v", err)
	}
	unsent := pending()
	unsent.ID, unsent.State, unsent.StateSent = "p2", "", false
	if err := store.SavePending(ctx, unsent); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Pending(ctx, "p2", "ana", t0); err != nil ||
		got != unsent {
		t.Fatalf("no state: %+v, %v", got, err)
	}
	sentEmpty := pending()
	sentEmpty.State = ""
	if err := store.SavePending(ctx, sentEmpty); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Pending(ctx, "p1", "ana", t0); err != nil ||
		got != sentEmpty {
		t.Fatalf("empty state: %+v, %v", got, err)
	}
}

func TestTakePendingOtherSubject(t *testing.T) {
	store, ctx := newStore(t), context.Background()
	if err := store.SavePending(ctx, pending()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.TakePending(ctx, "p1", "bo", t0); !errors.Is(
		err,
		oauth.ErrNotFound,
	) {
		t.Fatalf("take: %v", err)
	}
	if _, err := store.Pending(ctx, "p1", "bo", t0); !errors.Is(
		err,
		oauth.ErrNotFound,
	) {
		t.Fatalf("read: %v", err)
	}
	if _, err := store.Pending(ctx, "p1", "ana", t0); err != nil {
		t.Fatalf("a refused take must leave it: %v", err)
	}
}

func TestTakePendingExpired(t *testing.T) {
	store, ctx := newStore(t), context.Background()
	approval := pending()
	if err := store.SavePending(ctx, approval); err != nil {
		t.Fatal(err)
	}
	if _, err := store.TakePending(ctx, "p1", "ana",
		approval.Expires); !errors.Is(
		err,
		oauth.ErrNotFound,
	) {
		t.Fatalf("take: %v", err)
	}
	if _, err := store.Pending(ctx, "p1", "ana", approval.Expires); !errors.Is(
		err,
		oauth.ErrNotFound,
	) {
		t.Fatalf("read: %v", err)
	}
}

func TestConnectionRoundTrip(t *testing.T) {
	store, ctx := newStore(t), context.Background()
	want := connection("c1")
	if err := store.CreateConnection(ctx, want, code("h1", "c1")); err != nil {
		t.Fatal(err)
	}
	got, err := store.Connection(ctx, "c1")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, %v; want %+v", got, err, want)
	}
	list, err := store.ConnectionsOf(ctx, "ana", t0)
	if err != nil || len(list) != 1 || list[0].ID != "c1" {
		t.Fatalf("ConnectionsOf: %+v, %v", list, err)
	}
	if list, _ := store.ConnectionsOf(ctx, "bo", t0); len(list) != 0 {
		t.Fatalf("another subject's list: %+v", list)
	}
	if _, err := store.Connection(ctx, "nope"); !errors.Is(
		err,
		oauth.ErrNotFound,
	) {
		t.Fatalf("missing: %v", err)
	}
}

func TestCreateConnectionIsAtomic(t *testing.T) {
	store, ctx := newStore(t), context.Background()
	c1 := connection("c1")
	if err := store.CreateConnection(ctx, c1, code("h", "c1")); err != nil {
		t.Fatal(err)
	}
	err := store.CreateConnection(ctx, connection("c2"), code("h", "c2"))
	if err == nil {
		t.Fatal("duplicate code hash accepted")
	}
	if _, err := store.Connection(ctx, "c2"); !errors.Is(
		err,
		oauth.ErrNotFound,
	) {
		t.Fatalf("connection outlived its failed code: %v", err)
	}
}

func TestEndConnectionHidesFromConnectionsOf(t *testing.T) {
	store, ctx := newStore(t), context.Background()
	c1 := connection("c1")
	if err := store.CreateConnection(ctx, c1, code("h1", "c1")); err != nil {
		t.Fatal(err)
	}
	at := t0.Add(time.Hour)
	if err := store.EndConnection(ctx, "c1", at); err != nil {
		t.Fatal(err)
	}
	if err := store.EndConnection(ctx, "c1", at.Add(time.Hour)); err != nil {
		t.Fatalf("ending twice: %v", err)
	}
	if list, _ := store.ConnectionsOf(ctx, "ana", t0); len(list) != 0 {
		t.Fatalf("ended connection listed: %+v", list)
	}
	got, err := store.Connection(ctx, "c1")
	if err != nil || got.Ended == nil || !got.Ended.Equal(at) {
		t.Fatalf("Connection after end: %+v, %v", got, err)
	}
	if err := store.EndConnection(ctx, "nope", at); !errors.Is(
		err,
		oauth.ErrNotFound,
	) {
		t.Fatalf("ending a stranger: %v", err)
	}
}

func TestTakeCodeTwice(t *testing.T) {
	store, ctx := newStore(t), context.Background()
	want := code("h1", "c1")
	if err := store.CreateConnection(ctx, connection("c1"), want); err != nil {
		t.Fatal(err)
	}
	got, used, err := store.TakeCode(ctx, want.Hash, t0)
	if err != nil || used || !reflect.DeepEqual(got, want) {
		t.Fatalf("first: %+v, %v, %v", got, used, err)
	}
	got, used, err = store.TakeCode(ctx, want.Hash, t0.Add(time.Second))
	if err != nil || !used || got.Connection != "c1" {
		t.Fatalf("second: %+v, %v, %v", got, used, err)
	}
}

func TestCodeReadsWithoutTaking(t *testing.T) {
	store, ctx := newStore(t), context.Background()
	want := code("h1", "c1")
	err := store.CreateConnection(ctx, connection("c1"), want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Code(ctx, want.Hash, t0)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Code: %+v, %v", got, err)
	}
	if _, used, err := store.TakeCode(ctx, want.Hash, t0); err != nil || used {
		t.Fatalf("Code took the code: used %v, %v", used, err)
	}
	if got, err = store.Code(ctx, want.Hash, t0); err != nil ||
		got.Connection != "c1" {
		t.Fatalf("a taken code: %+v, %v", got, err)
	}
	for _, at := range []time.Time{want.Expires, t0} {
		if at.Equal(t0) {
			if err := store.EndConnection(ctx, "c1", t0); err != nil {
				t.Fatal(err)
			}
		}
		_, err := store.Code(ctx, want.Hash, at)
		if !errors.Is(err, oauth.ErrNotFound) {
			t.Fatalf("expired or ended, at %v: %v", at, err)
		}
	}
}

func TestTakeCodeExpiredOrAbsent(t *testing.T) {
	store, ctx := newStore(t), context.Background()
	issued := code("h1", "c1")
	if err := store.CreateConnection(ctx, connection("c1"),
		issued); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.TakeCode(ctx, []byte("nope"), t0); !errors.Is(
		err,
		oauth.ErrNotFound,
	) {
		t.Fatalf("absent: %v", err)
	}
	if _, _, err := store.TakeCode(ctx, issued.Hash,
		issued.Expires); !errors.Is(
		err,
		oauth.ErrNotFound,
	) {
		t.Fatalf("expired: %v", err)
	}
	if _, used, err := store.TakeCode(ctx, issued.Hash, t0); err != nil ||
		used {
		t.Fatalf(
			"an expired refusal must not spend the code: %v, %v",
			used,
			err,
		)
	}
}

func withRefresh(t *testing.T, store oauth.Store, hash string) {
	t.Helper()
	ctx := context.Background()
	c1 := connection("c1")
	if err := store.CreateConnection(ctx, c1, code("h1", "c1")); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRefresh(ctx, refresh(hash, "c1")); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshRoundTrip(t *testing.T) {
	store, ctx := newStore(t), context.Background()
	withRefresh(t, store, "r1")
	connection, retired, err := store.RefreshConnection(ctx, []byte("r1"), t0)
	if err != nil || retired || connection.ID != "c1" {
		t.Fatalf("RefreshConnection: %+v, %v, %v", connection, retired, err)
	}
	later := t0.Add(31 * 24 * time.Hour)
	if _, _, err := store.RefreshConnection(ctx, []byte("r1"),
		later); !errors.Is(
		err,
		oauth.ErrNotFound,
	) {
		t.Fatalf("expired: %v", err)
	}
	if _, _, err := store.RefreshConnection(ctx, []byte("zz"), t0); !errors.Is(
		err,
		oauth.ErrNotFound,
	) {
		t.Fatalf("absent: %v", err)
	}
	now := t0.Add(time.Hour)
	connection, reused, err := store.RotateRefresh(
		ctx,
		[]byte("r1"),
		refresh("r2", "c1"),
		now,
	)
	if err != nil || reused || !connection.LastUsed.Equal(now) {
		t.Fatalf("rotate: %+v, %v, %v", connection, reused, err)
	}
	if _, _, err := store.RefreshConnection(ctx, []byte("r2"),
		now); err != nil {
		t.Fatalf("next token: %v", err)
	}
}

func TestRotateRefreshTouchesConnection(t *testing.T) {
	store, ctx := newStore(t), context.Background()
	withRefresh(t, store, "r1")
	first := t0.Add(time.Hour)
	next := refresh("r2", "c1")
	_, _, err := store.RotateRefresh(ctx, []byte("r1"), next, first)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := store.Connection(ctx, "c1")
	if !got.LastUsed.Equal(first) {
		t.Fatalf("LastUsed = %v, want %v", got.LastUsed, first)
	}
	connection, reused, err := store.RotateRefresh(
		ctx,
		[]byte("r1"),
		refresh("r3", "c1"),
		first.Add(time.Hour),
	)
	if err != nil || !reused || connection.ID != "c1" {
		t.Fatalf("reuse: %+v, %v, %v", connection, reused, err)
	}
	got, _ = store.Connection(ctx, "c1")
	if !got.LastUsed.Equal(first) {
		t.Fatalf("a reuse moved LastUsed to %v", got.LastUsed)
	}
	if _, _, err := store.RefreshConnection(ctx, []byte("r3"),
		first); !errors.Is(
		err,
		oauth.ErrNotFound,
	) {
		t.Fatalf("a reuse stored a next token: %v", err)
	}
}

func TestRotateRefreshRefusals(t *testing.T) {
	store, ctx := newStore(t), context.Background()
	withRefresh(t, store, "r1")
	next := refresh("r2", "c1")
	if _, _, err := store.RotateRefresh(ctx, []byte("zz"), next,
		t0); !errors.Is(
		err,
		oauth.ErrNotFound,
	) {
		t.Fatalf("absent: %v", err)
	}
	late := t0.Add(31 * 24 * time.Hour)
	if _, _, err := store.RotateRefresh(ctx, []byte("r1"), next,
		late); !errors.Is(
		err,
		oauth.ErrNotFound,
	) {
		t.Fatalf("expired: %v", err)
	}
	if err := store.EndConnection(ctx, "c1", t0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.RotateRefresh(ctx, []byte("r1"), next,
		t0); !errors.Is(
		err,
		oauth.ErrNotFound,
	) {
		t.Fatalf("ended: %v", err)
	}
}

// A retired token outlives its own expiry while its connection is
// live, so a replay of it still ends the connection (RFC 9700
// §4.14.2). Ending the connection deletes it.
func TestRetiredRefreshOutlivesExpiry(t *testing.T) {
	store, ctx := newStore(t), context.Background()
	withRefresh(t, store, "r1")
	day := 24 * time.Hour
	rotations := []struct {
		old, next string
		at        time.Time
	}{
		{"r1", "r2", t0.Add(time.Hour)},
		{"r2", "r3", t0.Add(29 * day)},
		// After r1's expiry: the sweep runs here.
		{"r3", "r4", t0.Add(31 * day)},
	}
	for _, rotation := range rotations {
		next := refresh(rotation.next, "c1")
		next.Expires = rotation.at.Add(30 * day)
		_, reused, err := store.RotateRefresh(ctx, []byte(rotation.old),
			next, rotation.at)
		if err != nil || reused {
			t.Fatalf("rotating %s: reused %v, %v", rotation.old, reused, err)
		}
	}
	late := t0.Add(32 * day)
	got, retired, err := store.RefreshConnection(ctx, []byte("r1"), late)
	if err != nil || !retired || got.ID != "c1" {
		t.Fatalf("r1 after its expiry: %+v, retired %v, %v",
			got, retired, err)
	}
	_, reused, err := store.RotateRefresh(ctx, []byte("r1"),
		refresh("r5", "c1"), late)
	if err != nil || !reused {
		t.Fatalf("rotating r1 after its expiry: reused %v, %v", reused, err)
	}
	if err := store.EndConnection(ctx, "c1", late); err != nil {
		t.Fatal(err)
	}
	var left int
	err = oauth.Write(store, func(conn *sql.Conn) error {
		return conn.QueryRowContext(ctx,
			`SELECT count(*) FROM oauth_refresh`).Scan(&left)
	})
	if err != nil || left != 0 {
		t.Fatalf("%d refresh tokens left after the end, %v", left, err)
	}
}

func TestRotateRefreshRollsBack(t *testing.T) {
	store, ctx := newStore(t), context.Background()
	withRefresh(t, store, "r1")
	if err := store.SaveRefresh(ctx, refresh("r2", "c1")); err != nil {
		t.Fatal(err)
	}
	now := t0.Add(time.Hour)
	_, _, err := store.RotateRefresh(
		ctx,
		[]byte("r1"),
		refresh("r2", "c1"),
		now,
	)
	if err == nil {
		t.Fatal("duplicate next hash accepted")
	}
	if got, _ := store.Connection(ctx, "c1"); !got.LastUsed.Equal(t0) {
		t.Fatalf("LastUsed moved to %v despite the failure", got.LastUsed)
	}
	connection, reused, err := store.RotateRefresh(
		ctx,
		[]byte("r1"),
		refresh("r3", "c1"),
		now,
	)
	if err != nil || reused {
		t.Fatalf("second try: %+v, reused=%v, %v", connection, reused, err)
	}
}

// The race runs on a pool wider than the one connection the other
// tests use, so the writers really do contend for the lock.
func TestRotateRefreshRace(t *testing.T) {
	store := wideStore(t)
	withRefresh(t, store, "r1")

	const racers = 20
	var (
		wg      sync.WaitGroup
		start   = make(chan struct{})
		fresh   = make(chan int, racers)
		failure = make(chan error, racers)
	)
	for index := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			next := refresh(string(rune('A'+index)), "c1")
			_, reused, err := store.RotateRefresh(context.Background(),
				[]byte("r1"), next, t0.Add(time.Hour))
			if err != nil {
				failure <- err
			} else if !reused {
				fresh <- index
			}
		}()
	}
	close(start)
	wg.Wait()
	close(fresh)
	close(failure)
	for err := range failure {
		t.Errorf("rotation failed: %v", err)
	}
	if len(fresh) != 1 {
		t.Fatalf("%d rotations were not reuses, want exactly 1", len(fresh))
	}
}

func TestRefreshConnectionRetiredAndEnded(t *testing.T) {
	store, ctx := newStore(t), context.Background()
	withRefresh(t, store, "r1")
	now := t0.Add(time.Hour)
	next := refresh("r2", "c1")
	if _, _, err := store.RotateRefresh(ctx, []byte("r1"), next,
		now); err != nil {
		t.Fatal(err)
	}
	connection, retired, err := store.RefreshConnection(ctx, []byte("r1"), now)
	if err != nil || !retired || connection.ID != "c1" {
		t.Fatalf("rotated token: %+v, %v, %v", connection, retired, err)
	}
	if _, retired, err := store.RefreshConnection(ctx, []byte("r2"),
		now); err != nil ||
		retired {
		t.Fatalf("live token: %v, %v", retired, err)
	}
	if err := store.EndConnection(ctx, "c1", now); err != nil {
		t.Fatal(err)
	}
	for _, hash := range []string{"r1", "r2"} {
		_, _, err := store.RefreshConnection(ctx, []byte(hash), now)
		if !errors.Is(err, oauth.ErrNotFound) {
			t.Fatalf("%s after the connection ended: %v", hash, err)
		}
	}
}

func TestRotateRefreshRefusesForeignConnection(t *testing.T) {
	store, ctx := newStore(t), context.Background()
	withRefresh(t, store, "r1")
	next := refresh("r2", "other")
	if _, _, err := store.RotateRefresh(ctx, []byte("r1"), next,
		t0); err == nil {
		t.Fatal("next token for another connection accepted")
	}
	if _, retired, err := store.RefreshConnection(ctx, []byte("r1"),
		t0); err != nil ||
		retired {
		t.Fatalf(
			"a refused rotation retired the old token: %v, %v",
			retired,
			err,
		)
	}
}

func TestCreateConnectionRefusesForeignCode(t *testing.T) {
	store, ctx := newStore(t), context.Background()
	err := store.CreateConnection(ctx, connection("c1"), code("h1", "other"))
	if err == nil {
		t.Fatal("code for another connection accepted")
	}
	if _, err := store.Connection(ctx, "c1"); !errors.Is(
		err,
		oauth.ErrNotFound,
	) {
		t.Fatalf("connection stored anyway: %v", err)
	}
}

func TestCreateConnectionRefusesInvalidAccess(t *testing.T) {
	store, ctx := newStore(t), context.Background()
	found := connection("c1")
	found.Access = 0
	if err := store.CreateConnection(ctx, found, code("h1", "c1")); err == nil {
		t.Fatal("connection without access accepted")
	}
	approval := pending()
	approval.Requested = 0
	if err := store.SavePending(ctx, approval); err == nil {
		t.Fatal("pending without access accepted")
	}
}

func TestTakeCodeOfEndedConnection(t *testing.T) {
	store, ctx := newStore(t), context.Background()
	issued := code("h1", "c1")
	err := store.CreateConnection(ctx, connection("c1"), issued)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EndConnection(ctx, "c1", t0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.TakeCode(ctx, issued.Hash, t0); !errors.Is(
		err,
		oauth.ErrNotFound,
	) {
		t.Fatalf("code of an ended connection: %v", err)
	}
}

func TestTakeCodeRace(t *testing.T) {
	store := wideStore(t)
	issued := code("h1", "c1")
	if err := store.CreateConnection(context.Background(), connection("c1"),
		issued); err != nil {
		t.Fatal(err)
	}
	const racers = 20
	var (
		wg    sync.WaitGroup
		start = make(chan struct{})
		fresh = make(chan bool, racers)
		fails = make(chan error, racers)
	)
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, used, err := store.TakeCode(
				context.Background(),
				issued.Hash,
				t0,
			)
			if err != nil {
				fails <- err
			} else if !used {
				fresh <- true
			}
		}()
	}
	close(start)
	wg.Wait()
	close(fresh)
	close(fails)
	for err := range fails {
		t.Errorf("take failed: %v", err)
	}
	if len(fresh) != 1 {
		t.Fatalf("%d takes were first, want exactly 1", len(fresh))
	}
}

func TestSavePendingSweepsExpired(t *testing.T) {
	store, ctx := newStore(t), context.Background()
	old := pending()
	if err := store.SavePending(ctx, old); err != nil {
		t.Fatal(err)
	}
	fresh := pending()
	fresh.ID, fresh.Expires = "p2", old.Expires.Add(20*time.Minute)
	if err := store.SavePending(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	// Read at a time before old expired: only a deleted row is absent.
	if _, err := store.Pending(ctx, "p1", "ana", t0); !errors.Is(
		err,
		oauth.ErrNotFound,
	) {
		t.Fatalf("expired pending approval kept: %v", err)
	}
	if _, err := store.Pending(ctx, "p2", "ana", t0); err != nil {
		t.Fatalf("live pending approval swept: %v", err)
	}
}

func TestTakePendingRollsBack(t *testing.T) {
	db := schema.Open(t)
	store, ctx := oauth.NewSQLStore(db), context.Background()
	if err := store.SavePending(ctx, pending()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER no_delete BEFORE DELETE ON
oauth_pending BEGIN SELECT RAISE(ABORT, 'no'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.TakePending(ctx, "p1", "ana", t0); err == nil {
		t.Fatal("take succeeded despite the failing delete")
	}
	if _, err := db.Exec(`DROP TRIGGER no_delete`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.TakePending(ctx, "p1", "ana", t0); err != nil {
		t.Fatalf("the approval or the connection was lost: %v", err)
	}
}

func TestWriteSurvivesPanic(t *testing.T) {
	store := newStore(t)
	func() {
		defer func() { recover() }()
		oauth.Write(store, func(tx *sql.Conn) error { panic("boom") })
	}()
	// The one pooled connection must not come back inside the
	// transaction the panic left open.
	err := oauth.Write(store, func(tx *sql.Conn) error { return nil })
	if err != nil {
		t.Fatalf("write after a panic: %v", err)
	}
}

func TestConnectionsOfIdleAndOrder(t *testing.T) {
	store, ctx := newStore(t), context.Background()
	for index, found := range []oauth.Connection{
		connection("old"), connection("idle"), connection("new"),
	} {
		found.Created = t0.Add(time.Duration(index) * time.Hour)
		found.LastUsed = found.Created
		if found.ID == "idle" {
			found.LastUsed = t0.Add(-40 * 24 * time.Hour)
		}
		if err := store.CreateConnection(ctx, found, code("h"+found.ID,
			found.ID)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.ConnectionsOf(ctx, "ana", t0.Add(5*time.Hour))
	if err != nil || len(got) != 2 || got[0].ID != "new" || got[1].ID != "old" {
		t.Fatalf("got %+v, %v; want new then old, idle one hidden", got, err)
	}
}

func wideStore(t *testing.T) oauth.Store {
	t.Helper()
	db, err := sql.Open("sqlite", schema.Path(t)+
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(8)
	return oauth.NewSQLStore(db)
}
