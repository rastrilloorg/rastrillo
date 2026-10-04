package pow

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

const scope = "test/scope"

func TestAdmitAcceptsAGoodSubmissionAndWritesNothing(t *testing.T) {
	g := newTestGuard(t, nil)
	f := g.Form(t0, scope)
	a := g.Admit(post(t, f, "", nil), Want{Scope: scope})
	if !a.OK {
		t.Fatalf("Admit refused a good submission: %s %v", a.Reason, a.Also)
	}
	if spent, _ := g.nonces.Spent(context.Background(), f.Nonce); spent {
		t.Fatal("Admit spent the nonce: a validation error afterwards would burn the visitor's solve")
	}
}

func TestCommitSpendsOnceAndAReplayIsRefused(t *testing.T) {
	g := newTestGuard(t, nil)
	f := g.Form(t0, scope)
	a := g.Admit(post(t, f, "", nil), Want{Scope: scope})
	if err := a.Commit(context.Background(), nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := a.Commit(context.Background(), nil); !errors.Is(err, ErrSpent) {
		t.Fatalf("second Commit = %v, want ErrSpent", err)
	}
	if b := g.Admit(post(t, f, "", nil), Want{Scope: scope}); b.OK || b.Reason != ReasonSpent {
		t.Fatalf("replay = %v %s, want refused as spent", b.OK, b.Reason)
	}
}

func TestCommitOfARefusalIsAnError(t *testing.T) {
	g := newTestGuard(t, nil)
	a := g.Admit(post(t, g.Form(t0, scope), "", nil), Want{Scope: "other"})
	if err := a.Commit(context.Background(), nil); !errors.Is(err, ErrNotAdmitted) {
		t.Fatalf("Commit of a refusal = %v, want ErrNotAdmitted", err)
	}
}

func TestScopeMismatchIsSealInvalid(t *testing.T) {
	g := newTestGuard(t, nil)
	a := g.Admit(post(t, g.Form(t0, "apply:41"), "", nil), Want{Scope: "apply:42"})
	if a.OK || a.Reason != ReasonSealInvalid {
		t.Fatalf("token for event 41 on event 42 = %v %s", a.OK, a.Reason)
	}
}

func TestMissingFieldsAreReasonMissing(t *testing.T) {
	g := newTestGuard(t, nil)
	r := post(t, g.Form(t0, scope), "", func(v url.Values) {
		for k := range v {
			if strings.HasPrefix(k, "pow_") {
				v.Del(k)
			}
		}
	})
	if a := g.Admit(r, Want{Scope: scope}); a.Reason != ReasonMissing {
		t.Fatalf("no challenge = %s, want missing", a.Reason)
	}
}

func TestHoneypotFirstAndCheapFailuresInAlso(t *testing.T) {
	g := newTestGuard(t, nil)
	g.now = func() time.Time { return t0 } // too fast as well
	r := post(t, g.Form(t0, scope), "", func(v url.Values) {
		v.Set(fieldHoneypot, "https://example.com")
		v.Set(fieldCounter, "") // an empty counter never verifies; "0" would, one time in 1024
	})
	a := g.Admit(r, Want{Scope: scope})
	if a.Reason != ReasonHoneypot {
		t.Fatalf("first reason = %s, want honeypot", a.Reason)
	}
	for _, want := range []Reason{ReasonTooFast, ReasonShort} {
		found := false
		for _, r := range a.Also {
			found = found || r == want
		}
		if !found {
			t.Errorf("Also = %v, missing %s", a.Also, want)
		}
	}
}

func TestMinimumAgeIsMilliseconds(t *testing.T) {
	g := newTestGuard(t, func(c *Config) { c.MinAge = 500 * time.Millisecond })
	issued := time.UnixMilli(t0.UnixMilli()/1000*1000 + 999) // late in a second
	f := g.Form(issued, scope)
	g.now = func() time.Time { return issued.Add(400 * time.Millisecond) }
	if a := g.Admit(post(t, f, "", nil), Want{Scope: scope}); a.Reason != ReasonTooFast {
		t.Fatalf("400ms after issue = %s, want too_fast", a.Reason)
	}
	g.now = func() time.Time { return issued.Add(600 * time.Millisecond) }
	if a := g.Admit(post(t, f, "", nil), Want{Scope: scope}); !a.OK {
		t.Fatalf("600ms after issue refused: %s", a.Reason)
	}
}

func TestExpiryIsSealedAndCanOnlyShorten(t *testing.T) {
	short := newTestGuard(t, func(c *Config) { c.MaxAge = time.Minute })
	f := short.Form(t0, scope)
	long := newTestGuard(t, func(c *Config) { c.MaxAge = 2 * time.Hour })
	long.now = func() time.Time { return t0.Add(10 * time.Minute) }
	if a := long.Admit(post(t, f, "", nil), Want{Scope: scope}); a.Reason != ReasonTooOld {
		t.Fatalf("a 1-minute token at 10 minutes under a 2h guard = %s, want too_old: a longer MaxAge revived it", a.Reason)
	}
	g := newTestGuard(t, func(c *Config) { c.MaxAge = 2 * time.Hour })
	tokenLong := g.Form(t0, scope)
	shortNow := newTestGuard(t, func(c *Config) { c.MaxAge = time.Minute })
	if a := shortNow.Admit(post(t, tokenLong, "", nil), Want{Scope: scope}); a.Reason != ReasonTooOld {
		t.Fatalf("a 2h token under a 1-minute guard = %s, want too_old", a.Reason)
	}
}

func TestALowerDifficultyTokenIsRefused(t *testing.T) {
	easy := newTestGuard(t, func(c *Config) { c.Difficulty = NoProof })
	hard := newTestGuard(t, nil)
	if a := hard.Admit(post(t, easy.Form(t0, scope), "", nil), Want{Scope: scope}); a.Reason != ReasonSealInvalid {
		t.Fatalf("NoProof token on a proof guard = %s, want seal_invalid", a.Reason)
	}
}

func TestBoundGuardsNeedTheBinding(t *testing.T) {
	g := newTestGuard(t, func(c *Config) { c.Bind = true })
	f := g.Form(t0, scope)
	if a := g.Admit(post(t, f, "a@example.com", nil), Want{Scope: scope, Binding: "A@Example.com "}); !a.OK {
		t.Fatalf("bound submission refused: %s", a.Reason)
	}
	// A counter solved for a@ also satisfies b@ about one time in 1024
	// at 10 bits; pick a challenge where it does not, so the test cannot
	// pass or fail by luck.
	var f2 Form
	var counter string
	for {
		f2 = g.Form(t0, scope)
		counter = solveFor(t, f2.Nonce, "a@example.com", f2.Difficulty)
		if !Verify(f2.Nonce, "b@example.com", counter, f2.Difficulty) {
			break
		}
	}
	r := post(t, f2, "a@example.com", func(v url.Values) { v.Set(fieldCounter, counter) })
	if a := g.Admit(r, Want{Scope: scope, Binding: "b@example.com"}); a.Reason != ReasonShort {
		t.Fatalf("proof for another address = %s, want pow_short", a.Reason)
	}
	// A bound Guard handed no binding refuses rather than quietly
	// checking the work against the empty string: a caller that forgot
	// Want.Binding has an unbound form it believes is bound.
	f3 := g.Form(t0, scope)
	if a := g.Admit(post(t, f3, "", nil), Want{Scope: scope}); a.Reason != ReasonShort {
		t.Fatalf("bound guard without Want.Binding = %s, want pow_short", a.Reason)
	}
	unbound := newTestGuard(t, nil)
	if a := unbound.Admit(post(t, f2, "a@example.com", nil), Want{Scope: scope}); a.Reason != ReasonSealInvalid {
		t.Fatalf("bound token on an unbound guard = %s, want seal_invalid", a.Reason)
	}
}

func TestAttemptsAreCapped(t *testing.T) {
	// The rollback case, which is the one that matters, is
	// TestAttemptsSurviveCommitAndRollback in store_test.go: it needs a
	// real transaction.
	g := newTestGuard(t, func(c *Config) { c.Attempts = 3 })
	f := g.Form(t0, scope)
	for i := 0; i < 3; i++ {
		if a := g.Admit(post(t, f, "", nil), Want{Scope: scope}); !a.OK {
			t.Fatalf("attempt %d refused: %s", i+1, a.Reason)
		}
	}
	if a := g.Admit(post(t, f, "", nil), Want{Scope: scope}); a.Reason != ReasonAttempts {
		t.Fatalf("fourth attempt = %s, want attempts", a.Reason)
	}
}

func TestCapacityRefusesNewTokensAndNeverEvicts(t *testing.T) {
	g := newTestGuard(t, func(c *Config) { c.Tracked = 2; c.Attempts = 5 })
	a, b, c := g.Form(t0, scope), g.Form(t0, scope), g.Form(t0, scope)
	for _, f := range []Form{a, b} {
		if adm := g.Admit(post(t, f, "", nil), Want{Scope: scope}); !adm.OK {
			t.Fatalf("filling: %s", adm.Reason)
		}
	}
	if adm := g.Admit(post(t, c, "", nil), Want{Scope: scope}); adm.Reason != ReasonBusy {
		t.Fatalf("third token at capacity = %s, want busy", adm.Reason)
	}
	if adm := g.Admit(post(t, a, "", nil), Want{Scope: scope}); !adm.OK {
		t.Fatalf("a tracked token was refused at capacity: %s", adm.Reason)
	}
}

func TestCheckBypassesTheAttemptMap(t *testing.T) {
	g := newTestGuard(t, func(c *Config) { c.Tracked = 1 })
	if adm := g.Admit(post(t, g.Form(t0, "other"), "", nil), Want{Scope: "other"}); !adm.OK {
		t.Fatal(adm.Reason)
	}
	f := g.Form(t0, scope)
	if adm := g.Check(post(t, f, "", nil), Want{Scope: scope}); !adm.OK {
		t.Fatalf("Check answered %s while another scope filled the map", adm.Reason)
	}
	if spent, _ := g.nonces.Spent(context.Background(), f.Nonce); !spent {
		t.Fatal("Check did not spend")
	}
	if adm := g.Check(post(t, f, "", nil), Want{Scope: scope}); adm.Reason != ReasonSpent {
		t.Fatalf("second Check = %s, want spent", adm.Reason)
	}
}

func TestRecoveryIsTraplessAndAdmissibleAtOnce(t *testing.T) {
	g := newTestGuard(t, func(c *Config) { c.Difficulty = NoProof; c.MinAge = 3 * time.Second })
	g.now = func() time.Time { return t0 }
	first := g.Form(t0, scope)
	refused := g.Admit(post(t, first, "", func(v url.Values) { v.Set(fieldHoneypot, "x") }), Want{Scope: scope})
	if refused.OK || refused.Recovered() {
		t.Fatal("an original token reported recovered")
	}
	rec := g.Recovery(t0, scope)
	if strings.Contains(string(rec.Fields()), `name="hp"`) {
		t.Fatal("Recovery rendered the trap")
	}
	a := g.Admit(post(t, rec, "", func(v url.Values) { v.Set(fieldHoneypot, "filled anyway") }), Want{Scope: scope})
	if !a.OK {
		t.Fatalf("recovery refused at once: %s %v", a.Reason, a.Also)
	}
	if !a.Recovered() {
		t.Fatal("Recovered() false on a recovery token")
	}
	refusedRec := g.Admit(post(t, g.Recovery(t0, scope), "", nil), Want{Scope: "other"})
	if refusedRec.Recovered() {
		t.Fatal("Recovered() true before the seal verified")
	}
}

func TestFollowOnIsAdmissibleAtOnceAndKeepsTheTrap(t *testing.T) {
	g := newTestGuard(t, func(c *Config) { c.MinAge = 3 * time.Second })
	g.now = func() time.Time { return t0 }
	f := g.FollowOn(t0, scope)
	if !strings.Contains(string(f.Fields()), `name="hp"`) {
		t.Fatal("FollowOn dropped the trap; only Recovery may")
	}
	if a := g.Admit(post(t, f, "", nil), Want{Scope: scope}); !a.OK {
		t.Fatalf("FollowOn refused at once: %s", a.Reason)
	}
}

func TestVerifyNeitherCountsNorSpendsAndReturnsTheScope(t *testing.T) {
	minter := newTestGuard(t, func(c *Config) { c.Difficulty = 12 })
	other := newTestGuard(t, func(c *Config) { c.Difficulty = NoProof; c.Tracked = 1 })
	f := minter.Form(t0, "apply:41")
	for i := 0; i < 3; i++ {
		p, reason, ok := other.Verify(post(t, f, "", nil))
		if !ok {
			t.Fatalf("Verify by another guard sharing the key refused: %s", reason)
		}
		if p.Scope != "apply:41" || p.Nonce != f.Nonce || p.Difficulty != 12 {
			t.Fatalf("Parent = %+v", p)
		}
	}
	if spent, _ := minter.nonces.Spent(context.Background(), f.Nonce); spent {
		t.Fatal("Verify spent the parent")
	}
	bound := newTestGuard(t, func(c *Config) { c.Bind = true })
	if _, reason, ok := other.Verify(post(t, bound.Form(t0, "x"), "a@b.c", nil)); ok || reason != ReasonSealInvalid {
		t.Fatalf("Verify of a bound token = %v %s, want seal_invalid", ok, reason)
	}
}

func TestRotatedKeyRefusesThenRecovers(t *testing.T) {
	// Review focus 3.
	old := newTestGuard(t, func(c *Config) { c.InstanceKey = "old" })
	rotated := newTestGuard(t, func(c *Config) { c.InstanceKey = "new" })
	if a := rotated.Admit(post(t, old.Form(t0, scope), "", nil), Want{Scope: scope}); a.Reason != ReasonSealInvalid {
		t.Fatalf("old key's token = %s, want seal_invalid", a.Reason)
	}
	if a := rotated.Admit(post(t, rotated.Recovery(t0.Add(time.Minute), scope), "", nil), Want{Scope: scope}); !a.OK {
		t.Fatalf("recovery under the new key refused: %s", a.Reason)
	}
}

func TestTwoTabsBothSucceed(t *testing.T) {
	// Review focus 5: each render mints its own token.
	g := newTestGuard(t, nil)
	a, b := g.Form(t0, scope), g.Form(t0, scope)
	for i, f := range []Form{a, b} {
		if adm := g.Check(post(t, f, "", nil), Want{Scope: scope}); !adm.OK {
			t.Fatalf("tab %d refused: %s", i+1, adm.Reason)
		}
	}
}

func TestNewRefusesWhatCannotWork(t *testing.T) {
	base := Config{InstanceKey: "k", Nonces: MemoryNonces(), ScriptURL: "/a.js", WorkerURL: "/w.js"}
	cases := map[string]struct {
		mut  func(*Config)
		want error
	}{
		"no key":           {func(c *Config) { c.InstanceKey = "" }, ErrEmptyInstanceKey},
		"no store":         {func(c *Config) { c.Nonces = nil }, ErrNoNonceStore},
		"no script":        {func(c *Config) { c.ScriptURL = "" }, ErrNoAssets},
		"no worker":        {func(c *Config) { c.WorkerURL = "" }, ErrNoAssets},
		"bind and noproof": {func(c *Config) { c.Bind = true; c.Difficulty = NoProof }, ErrBindNeedsProof},
	}
	for name, tc := range cases {
		cfg := base
		tc.mut(&cfg)
		if _, err := New(cfg); !errors.Is(err, tc.want) {
			t.Errorf("%s: New = %v, want %v", name, err, tc.want)
		}
	}
	np := base
	np.Difficulty, np.ScriptURL, np.WorkerURL = NoProof, "", ""
	if _, err := New(np); err != nil {
		t.Errorf("NoProof without assets refused: %v", err)
	}
}

func TestFormRendering(t *testing.T) {
	g := newTestGuard(t, func(c *Config) { c.MinAge = 3 * time.Second })
	f := g.Form(t0, scope)
	attrs := string(f.Attrs())
	for _, want := range []string{`data-pow-form`, `data-pow-nonce="` + f.Nonce + `"`, `data-pow-worker="/pow/pow-worker.js"`, `data-pow-min-age="3000"`} {
		if !strings.Contains(attrs, want) {
			t.Errorf("Attrs = %s, missing %s", attrs, want)
		}
	}
	if fo := g.FollowOn(t0, scope); !strings.Contains(string(fo.Attrs()), `data-pow-min-age="0"`) {
		t.Errorf("FollowOn Attrs = %s, want min age 0: a follow-on form must never be held", fo.Attrs())
	}
	if s := string(f.Script()); s != `<script type="module" src="/pow/pow.js"></script>` {
		t.Errorf("Script = %s", s)
	}
	if s := string(f.StatusLine("Reload <now>")); s != `<p data-pow-status>Reload &lt;now&gt;</p>` {
		t.Errorf("StatusLine = %s", s)
	}
	np := newTestGuard(t, func(c *Config) { c.Difficulty = NoProof })
	nf := np.Form(t0, scope)
	if nf.NeedsScript() || nf.Attrs() != "" || nf.Script() != "" || nf.StatusLine("x") != "" {
		t.Error("a NoProof form renders script machinery: its submit would wait for a module that never comes")
	}
	if !strings.Contains(string(nf.Fields()), `name="pow_seal"`) {
		t.Error("a NoProof form lost its sealed token")
	}
}
