package auth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"
	// The DST tests need Europe/Dublin. Embedding the zone data makes
	// them run on a runner with no /usr/share/zoneinfo, where a skip
	// would let the bug they guard come back unnoticed.
	_ "time/tzdata"

	"github.com/keymaildev/signin"

	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/objrec"
)

func newRecordLinks(t *testing.T) (*recordLinks, *objrec.DirStore, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	store := objrec.Dir(t.TempDir())
	return &recordLinks{store: store, pepper: bytes.Repeat([]byte{7}, 32),
		seal: crypto.Derive([]byte("test-instance-key"), "rastrillo/auth/link-address/v1"),
		now:  func() time.Time { return now }, log: slog.Default()}, store, &now
}

func put(t *testing.T, l *recordLinks, hash, addr string) {
	t.Helper()
	if err := l.PutLink(context.Background(), hash, addr, signin.PurposeSignin, l.now().Add(LinkTTL)); err != nil {
		t.Fatal(err)
	}
}

// allKeys returns every key in s, paging List until next is "".
func allKeys(t *testing.T, s objrec.Store) []string {
	t.Helper()
	var all []string
	cursor := ""
	for {
		keys, next, err := s.List(context.Background(), "", cursor, 0)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, keys...)
		if next == "" {
			return all
		}
		cursor = next
	}
}

func TestRecordLinksAreSingleUse(t *testing.T) {
	l, _, _ := newRecordLinks(t)
	h := signin.HashToken("tok")
	put(t, l, h, "ana@example.com")
	if a, ok, err := l.TakeLink(context.Background(), h, signin.PurposeSignin); !ok || err != nil || a != "ana@example.com" {
		t.Fatalf("first take: %q %v %v", a, ok, err)
	}
	if _, ok, err := l.TakeLink(context.Background(), h, signin.PurposeSignin); ok || err != nil {
		t.Fatalf("second take: ok %v, err %v; want a plain refusal", ok, err)
	}
}

func TestRecordLinksRaceHasOneWinner(t *testing.T) {
	l, _, _ := newRecordLinks(t)
	h := signin.HashToken("tok")
	put(t, l, h, "ana@example.com")
	const n = 16
	var (
		wg    sync.WaitGroup
		start = make(chan struct{})
		oks   = make(chan bool, n)
		errs  = make(chan error, n)
	)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, ok, err := l.TakeLink(context.Background(), h, signin.PurposeSignin)
			oks <- ok
			if err != nil {
				errs <- err
			}
		}()
	}
	close(start)
	wg.Wait()
	close(oks)
	close(errs)
	for err := range errs {
		t.Errorf("a racing take failed: %v", err)
	}
	won := 0
	for ok := range oks {
		if ok {
			won++
		}
	}
	if won != 1 {
		t.Fatalf("%d of %d racing takes redeemed one link; want exactly 1", won, n)
	}
}

func TestRecordLinksExpiredIsSpent(t *testing.T) {
	l, _, now := newRecordLinks(t)
	h := signin.HashToken("tok")
	put(t, l, h, "ana@example.com")
	sent := *now
	*now = sent.Add(LinkTTL + time.Second)
	if _, ok, err := l.TakeLink(context.Background(), h, signin.PurposeSignin); ok || err != nil {
		t.Fatalf("expired take: ok %v, err %v; want a plain refusal", ok, err)
	}
	*now = sent
	if _, ok, err := l.TakeLink(context.Background(), h, signin.PurposeSignin); ok || err != nil {
		t.Fatalf("an expired link was not spent: once the clock went back, ok %v, err %v", ok, err)
	}
}

func TestRecordLinksSurviveMidnight(t *testing.T) {
	l, _, now := newRecordLinks(t)
	*now = time.Date(2026, 10, 17, 23, 58, 0, 0, time.UTC)
	h := signin.HashToken("late")
	put(t, l, h, "ana@example.com")
	*now = time.Date(2026, 10, 18, 0, 5, 0, 0, time.UTC)
	if _, ok, _ := l.TakeLink(context.Background(), h, signin.PurposeSignin); !ok {
		t.Fatal("a link sent at 23:58 UTC was lost at 00:05")
	}
}

func TestRecordLinksWrongPurposeIsNotConsumed(t *testing.T) {
	l, _, _ := newRecordLinks(t)
	h := signin.HashToken("tok")
	put(t, l, h, "ana@example.com")
	if _, ok, err := l.TakeLink(context.Background(), h, "other"); ok || err != nil {
		t.Fatalf("take for another purpose: ok %v, err %v; want a plain refusal", ok, err)
	}
	if a, ok, err := l.TakeLink(context.Background(), h, signin.PurposeSignin); !ok || err != nil || a != "ana@example.com" {
		t.Fatalf("a take with the wrong purpose consumed the link: %q %v %v", a, ok, err)
	}
}

func TestRecordLinksUnknownTokenWritesNothing(t *testing.T) {
	l, store, _ := newRecordLinks(t)
	if _, ok, err := l.TakeLink(context.Background(), signin.HashToken("never-sent"), signin.PurposeSignin); ok || err != nil {
		t.Fatalf("unknown token: ok %v, err %v; want a plain refusal", ok, err)
	}
	if keys := allKeys(t, store); len(keys) != 0 {
		t.Fatalf("an unknown token wrote %v", keys)
	}
}

func TestRecordLinksHoldNoReadableAddress(t *testing.T) {
	l, store, _ := newRecordLinks(t)
	put(t, l, signin.HashToken("tok"), "Ana@Example.com")
	keys := allKeys(t, store)
	if len(keys) == 0 {
		t.Fatal("PutLink wrote nothing, so there was nothing to inspect")
	}
	for _, k := range keys {
		b, _, _ := store.Get(context.Background(), k)
		if bytes.Contains(bytes.ToLower(b), []byte("example.com")) {
			t.Fatalf("%s holds the address: %s", k, b)
		}
		// addr is base64 on the wire, which hides a plaintext address
		// from the search above as well as a sealed one does. Decode it
		// so storing the address merely encoded fails here too.
		var rec struct{ Addr []byte }
		if err := json.Unmarshal(b, &rec); err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		if bytes.Contains(bytes.ToLower(rec.Addr), []byte("example.com")) {
			t.Fatalf("%s holds the address, base64 but not sealed: %s", k, rec.Addr)
		}
	}
}

// People are looked up by eh, so a hash that varied with the
// address's case would split one person into two.
func TestRecordLinksStoreEmailHash(t *testing.T) {
	l, store, _ := newRecordLinks(t)
	h := signin.HashToken("tok")
	put(t, l, h, "Ana@Example.com")
	mac := hmac.New(sha256.New, l.pepper)
	mac.Write([]byte("ana@example.com"))
	want := hex.EncodeToString(mac.Sum(nil))
	if got := emailHash(l.pepper, "Ana@Example.com"); got != want {
		t.Fatalf("emailHash = %q, want %q", got, want)
	}
	rec, _, err := objrec.GetJSON[map[string]any](context.Background(), store, "links/2026-10-17/"+h+".json")
	if err != nil {
		t.Fatal(err)
	}
	if rec["eh"] != want || rec["purpose"] != signin.PurposeSignin {
		t.Fatalf("link record = %v, want eh %s and purpose signin", rec, want)
	}
}

func TestRecordLinksSweepKeepsYesterday(t *testing.T) {
	l, store, now := newRecordLinks(t)
	for _, day := range []int{15, 16, 17} {
		*now = time.Date(2026, 10, day, 12, 0, 0, 0, time.UTC)
		sent := signin.HashToken(fmt.Sprintf("taken-%d", day))
		put(t, l, sent, "ana@example.com")
		if _, ok, err := l.TakeLink(context.Background(), sent, signin.PurposeSignin); !ok || err != nil {
			t.Fatalf("take on the %dth: %v %v", day, ok, err)
		}
		// An unclicked link too: TakeLink deletes the link record of a
		// taken one, so without this the sweep of links/ goes untested.
		put(t, l, signin.HashToken(fmt.Sprintf("unclicked-%d", day)), "ana@example.com")
	}
	*now = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	if err := l.sweep(context.Background(), *now); err != nil {
		t.Fatal(err)
	}
	keys := allKeys(t, store)
	var want []string
	for _, day := range []int{16, 17} {
		d := time.Date(2026, 10, day, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
		want = append(want,
			"links-used/"+d+"/"+signin.HashToken(fmt.Sprintf("taken-%d", day)),
			"links/"+d+"/"+signin.HashToken(fmt.Sprintf("unclicked-%d", day))+".json")
	}
	slices.Sort(want)
	if !slices.Equal(keys, want) {
		t.Fatalf("after the sweep the store holds\n%v\nwant\n%v", keys, want)
	}
}

func dublin(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Dublin")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// The clock auth passes in may be local time. Dublin leaves summer time
// on 25 October 2026, so one local day back from 26 October 00:05 is 24
// October 23:05 UTC: subtracting the day before converting to UTC would
// read the 24th and miss a live link filed under the 25th.
func TestRecordLinksSurviveMidnightInDST(t *testing.T) {
	l, _, now := newRecordLinks(t)
	loc := dublin(t)
	*now = time.Date(2026, 10, 25, 23, 58, 0, 0, loc)
	h := signin.HashToken("late")
	put(t, l, h, "ana@example.com")
	*now = time.Date(2026, 10, 26, 0, 5, 0, 0, loc)
	if _, ok, err := l.TakeLink(context.Background(), h, signin.PurposeSignin); !ok || err != nil {
		t.Fatalf("a link sent at 23:58 on the night summer time ended was lost at 00:05: ok %v, err %v", ok, err)
	}
}

// The sweep's "yesterday" is a UTC day too. In autumn a local day back
// overshoots by an hour and keeps one day too many; in spring
// (summer time starts 29 March 2026) it falls short and deletes
// yesterday's markers while yesterday's links can still be read.
func TestRecordLinksSweepCutoffInDST(t *testing.T) {
	loc := dublin(t)
	for _, c := range []struct {
		name string
		at   time.Time // the sweep's clock, in Dublin
		days []string  // UTC days with one link and one marker each
		keep []string  // UTC days the sweep must keep
	}{
		{"autumn", time.Date(2026, 10, 26, 0, 5, 0, 0, loc),
			[]string{"2026-10-24", "2026-10-25", "2026-10-26"}, []string{"2026-10-25", "2026-10-26"}},
		{"spring", time.Date(2026, 3, 30, 0, 30, 0, 0, loc),
			[]string{"2026-03-27", "2026-03-28", "2026-03-29"}, []string{"2026-03-28", "2026-03-29"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			l, store, now := newRecordLinks(t)
			for _, d := range c.days {
				day, err := time.Parse(time.DateOnly, d)
				if err != nil {
					t.Fatal(err)
				}
				*now = day.Add(12 * time.Hour)
				taken := signin.HashToken("taken-" + d)
				put(t, l, taken, "ana@example.com")
				if _, ok, err := l.TakeLink(context.Background(), taken, signin.PurposeSignin); !ok || err != nil {
					t.Fatalf("take on %s: %v %v", d, ok, err)
				}
				put(t, l, signin.HashToken("unclicked-"+d), "ana@example.com")
			}
			*now = c.at
			if err := l.sweep(context.Background(), c.at); err != nil {
				t.Fatal(err)
			}
			var want []string
			for _, d := range c.keep {
				want = append(want, "links-used/"+d+"/"+signin.HashToken("taken-"+d),
					"links/"+d+"/"+signin.HashToken("unclicked-"+d)+".json")
			}
			slices.Sort(want)
			if keys := allKeys(t, store); !slices.Equal(keys, want) {
				t.Fatalf("sweep at %v (UTC %s) left\n%v\nwant\n%v", c.at, c.at.UTC().Format(time.DateTime), keys, want)
			}
		})
	}
}
