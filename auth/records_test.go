package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/mail"
	"amadan.net/rastrillo/rastrillo/objrec"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// newRecordsAuth is newTestAuth for a centre with no database: a Dir
// store, a fresh signing key, a 32-byte pepper, ProofOff, origin
// https://ppl.test.
func newRecordsAuth(t *testing.T, mut func(*Config)) (*Auth, *captureMailer, *objrec.DirStore) {
	t.Helper()
	k, err := sessions.NewSigningKey("k1")
	if err != nil {
		t.Fatal(err)
	}
	store := objrec.Dir(t.TempDir())
	m := &captureMailer{}
	cfg := Config{
		Origin: "https://ppl.test", InstanceKey: "test-instance-key", ProofOff: true, Mailer: m,
		Records: store, Pepper: bytes.Repeat([]byte{7}, 32), Signed: &sessions.Signed{Name: "ppl", Key: k},
	}
	if mut != nil {
		mut(&cfg)
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a, m, store
}

var recordsLinkRE = regexp.MustCompile(`https://ppl\.test/auth/verify\?token=[A-Za-z0-9_-]+`)

// signInAt runs beginSignin, follows the emailed link through GET and
// POST Verify, and returns the session cookie set.
func signInAt(t *testing.T, a *Auth, m *captureMailer, address string) *http.Cookie {
	t.Helper()
	return signInSeeing(t, a, m, address, func(*httptest.ResponseRecorder) {})
}

// signInSeeing is signInAt handing every response to see, for a test
// that inspects what each one set.
func signInSeeing(t *testing.T, a *Auth, m *captureMailer, address string, see func(*httptest.ResponseRecorder)) *http.Cookie {
	t.Helper()
	m.forget()
	see(beginSignin(t, a, address))
	link := recordsLinkRE.FindString(m.sentBody())
	if link == "" {
		t.Fatalf("no link in the mail to %s: %q", address, m.sentBody())
	}
	w := httptest.NewRecorder()
	a.Verify(w, httptest.NewRequest("GET", link, nil))
	see(w)
	if w.Code != http.StatusOK {
		t.Fatalf("GET Verify: %d", w.Code)
	}
	w = httptest.NewRecorder()
	a.Verify(w, redeemRequest(t, link))
	see(w)
	for _, c := range w.Result().Cookies() {
		if c.Name == a.SessionCookie() && c.Value != "" {
			return c
		}
	}
	t.Fatalf("POST Verify set no session cookie: %d %q", w.Code, w.Header().Get("Location"))
	return nil
}

// subjectOf is a.SessionFrom on a request carrying c.
func subjectOf(t *testing.T, a *Auth, c *http.Cookie) string {
	t.Helper()
	r := httptest.NewRequest("GET", "https://ppl.test/", nil)
	r.AddCookie(c)
	id, ok := a.SessionFrom(r)
	if !ok {
		t.Fatal("the cookie holds no valid session")
	}
	return id.Address
}

// assertNoAddress reads every object and fails if any lowercased body
// contains needle.
func assertNoAddress(t *testing.T, store objrec.Store, needle string) {
	t.Helper()
	for _, k := range allKeys(t, store) {
		body, _, err := store.Get(context.Background(), k)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(string(body)), needle) {
			t.Errorf("%s holds %q: %s", k, needle, body)
		}
	}
}

// keysUnder counts the keys under prefix.
func keysUnder(t *testing.T, store objrec.Store, prefix string) int {
	t.Helper()
	n := 0
	for _, k := range allKeys(t, store) {
		if strings.HasPrefix(k, prefix) {
			n++
		}
	}
	return n
}

// refreshWith calls a.Refresh for GET /_carlos/refresh?next=/ carrying c.
func refreshWith(a *Auth, c *http.Cookie) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "https://ppl.test/_carlos/refresh?next=/", nil)
	r.AddCookie(c)
	a.Refresh(w, r)
	return w
}

// postWith is a same-origin POST to target carrying c (if any).
func postWith(h http.HandlerFunc, target string, c *http.Cookie) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "https://ppl.test"+target, nil)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if c != nil {
		r.AddCookie(c)
	}
	h(w, r)
	return w
}

// sessionCookieSet is the session cookie a response set, nil if none.
func sessionCookieSet(a *Auth, w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == a.SessionCookie() {
			return c
		}
	}
	return nil
}

func TestRecordsNewRefusesHalfAMode(t *testing.T) {
	k, _ := sessions.NewSigningKey("k1")
	pepper := bytes.Repeat([]byte{1}, 32)
	store := objrec.Dir(t.TempDir())
	base := func() Config {
		return Config{Origin: "https://ppl.test", InstanceKey: "ik", ProofOff: true, Records: store, Pepper: pepper,
			Signed: &sessions.Signed{Name: "ppl", Key: k}}
	}
	// The cases below prove nothing unless the configurations they
	// break are accepted.
	good := base()
	if _, err := New(good); err != nil {
		t.Fatalf("the records base is refused: %v", err)
	}
	if good.Signed.Epoch != nil {
		t.Fatal("New wrote Epoch into the caller's Signed")
	}
	if _, err := New(Config{Origin: "https://ppl.test", InstanceKey: "ik", ProofOff: true, DB: newTestAuthDB(t), Mailer: &captureMailer{}}); err != nil {
		t.Fatalf("the row-mode base is refused: %v", err)
	}
	for name, mut := range map[string]func(*Config){
		"no Signed": func(c *Config) { c.Signed = nil },
		"verify-only Signed": func(c *Config) {
			keys, _ := sessions.ParsePublicKeys(k.Public())
			c.Signed = &sessions.Signed{Name: "ppl", Keys: keys}
		},
		"Epoch set":    func(c *Config) { c.Signed.Epoch = func(context.Context, string) (int64, error) { return 0, nil } },
		"short pepper": func(c *Config) { c.Pepper = pepper[:31] },
		"DB too":       func(c *Config) { c.DB = newTestAuthDB(t) },
		"SubjectFor":   func(c *Config) { c.SubjectFor = func(a string) (string, error) { return a, nil } },
		// "Blind, no Records" carries a working Mailer, so only the Records rule can refuse it.
		"Blind, no Records": func(c *Config) {
			c.Records, c.Signed, c.Pepper, c.DB, c.Blind, c.Mailer = nil, nil, nil, newTestAuthDB(t), true, &captureMailer{}
		},
		"Pepper, no Records": func(c *Config) { c.Records, c.Signed, c.DB = nil, nil, newTestAuthDB(t) },
		"Signed, no Records": func(c *Config) { c.Records, c.Pepper, c.DB = nil, nil, newTestAuthDB(t) },
		"Blind, no Mailer":   func(c *Config) { c.Blind, c.Mailer = true, nil },
		"Blind, log mailer":  func(c *Config) { c.Blind, c.Mailer = true, mail.Logged(slog.Default()) },
	} {
		c := base()
		mut(&c)
		if _, err := New(c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestRecordsFirstSigninCreatesTheUserAndItsIndex(t *testing.T) {
	a, m, store := newRecordsAuth(t, nil)
	c := signInAt(t, a, m, "Ana@Example.com")
	r := httptest.NewRequest("GET", "https://ppl.test/", nil)
	r.AddCookie(c)
	id, ok := a.SessionFrom(r)
	if !ok || !regexp.MustCompile(`^[a-z2-7]{26}$`).MatchString(id.Address) {
		t.Fatalf("subject %q: want the 26-char user id", id.Address)
	}
	u, err := a.User(context.Background(), id.Address)
	if err != nil || !strings.EqualFold(u.Email, "ana@example.com") || u.EH != emailHash(a.cfg.Pepper, "ana@example.com") {
		t.Fatalf("%+v %v", u, err)
	}
	if keys := allKeys(t, store); !slices.Contains(keys, "emails/"+u.EH+".json") || !slices.Contains(keys, "users/"+u.ID+".json") {
		t.Fatalf("keys: %v", keys)
	}
}

func TestRecordsSecondSigninIsTheSameUser(t *testing.T) {
	a, m, store := newRecordsAuth(t, nil)
	first := subjectOf(t, a, signInAt(t, a, m, "ana@example.com"))
	second := subjectOf(t, a, signInAt(t, a, m, "ANA@example.com"))
	if first != second {
		t.Fatalf("subjects %q and %q: one address, two users", first, second)
	}
	if u, e := keysUnder(t, store, "users/"), keysUnder(t, store, "emails/"); u != 1 || e != 1 {
		t.Fatalf("%d users/ and %d emails/ records, want 1 each", u, e)
	}
}

func TestRecordsTwoDevicesAtOnceShareOneUser(t *testing.T) {
	a, _, store := newRecordsAuth(t, nil)
	var wg sync.WaitGroup
	ids := make([]string, 16)
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u, err := a.users.ensure(context.Background(), "new@example.com")
			if err != nil {
				t.Error(err)
			}
			ids[i] = u.ID
		}()
	}
	wg.Wait()
	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("two users for one address: %v", ids)
		}
	}
	if n := keysUnder(t, store, "users/"); n != 1 {
		t.Fatalf("%d users/ records, want 1", n)
	}
}

func TestRecordsRepairsAMissingUserRecord(t *testing.T) {
	a, m, store := newRecordsAuth(t, nil)
	const uid = "aaaaaaaaaaaaaaaaaaaaaaaaaa"
	eh := emailHash(a.cfg.Pepper, "ana@example.com")
	if _, err := objrec.CreateJSON(context.Background(), store, "emails/"+eh+".json", map[string]string{"uid": uid}); err != nil {
		t.Fatal(err)
	}
	if got := subjectOf(t, a, signInAt(t, a, m, "ana@example.com")); got != uid {
		t.Fatalf("subject %q, want the claimed uid %q", got, uid)
	}
	u, err := a.User(context.Background(), uid)
	if err != nil || u.ID != uid || u.EH != eh {
		t.Fatalf("users/%s.json after the repair: %+v %v", uid, u, err)
	}
	if n := keysUnder(t, store, "users/"); n != 1 {
		t.Fatalf("%d users/ records, want the one repaired", n)
	}
}

func TestRecordsRecreatedUserDoesNotReviveOldSessions(t *testing.T) {
	a, m, store := newRecordsAuth(t, nil)
	frozen := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return frozen } // one unchanged clock: a time-based epoch would repeat
	old := signInAt(t, a, m, "ana@example.com")
	uid := subjectOf(t, a, old)
	if err := store.Delete(context.Background(), "users/"+uid+".json"); err != nil {
		t.Fatal(err)
	}
	signInAt(t, a, m, "ana@example.com") // the repair writes a new record, and a new epoch
	a.now = func() time.Time { return frozen.Add(16 * time.Minute) }
	w := refreshWith(a, old)
	if !strings.HasPrefix(w.Header().Get("Location"), "/signin") {
		t.Fatalf("a cookie from before the deletion refreshed: %q", w.Header().Get("Location"))
	}
}

func TestRecordsBlindStoresNoAddress(t *testing.T) {
	a, m, store := newRecordsAuth(t, func(c *Config) { c.Blind = true })
	beginSignin(t, a, "Ana@Example.com")
	if keysUnder(t, store, "links/") != 1 {
		t.Fatal("no link waiting: the check below would read nothing")
	}
	assertNoAddress(t, store, "example.com") // while the link waits
	uid := subjectOf(t, a, signInAt(t, a, m, "Ana@Example.com"))
	assertNoAddress(t, store, "example.com") // after sign-in
	body, _, err := store.Get(context.Background(), "users/"+uid+".json")
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal(body, &rec); err != nil {
		t.Fatal(err)
	}
	if _, has := rec["email"]; has || rec["eh"] != emailHash(a.cfg.Pepper, "ana@example.com") {
		t.Fatalf("users/%s.json: %s; want eh set and no email member", uid, body)
	}
}

// failOnceMailer fails its first send with the recipient in the error,
// as an SMTP error does, then sends as captureMailer.
type failOnceMailer struct {
	captureMailer
	failed bool
}

func (m *failOnceMailer) Send(ctx context.Context, to, subject, body string) error {
	m.mu.Lock()
	first := !m.failed
	m.failed = true
	m.mu.Unlock()
	if first {
		return fmt.Errorf("send to %s: refused", to)
	}
	return m.captureMailer.Send(ctx, to, subject, body)
}

// readings is s and every decoding of it that applies: base64url,
// base64 (padded or not) and URL escaping, of the whole value and of
// each dot-separated part. The unsealed pending blob is base64 JSON
// and would pass a literal check.
func readings(s string) []string {
	out := []string{s}
	parts := append([]string{s}, strings.Split(s, ".")...)
	for _, p := range parts {
		for _, enc := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.StdEncoding, base64.RawStdEncoding} {
			if b, err := enc.DecodeString(p); err == nil {
				out = append(out, string(b))
			}
		}
		if u, err := url.QueryUnescape(p); err == nil {
			out = append(out, u)
		}
	}
	return out
}

// Every cookie reaches every tenant on the shared origin, and the logs
// are the operator's: neither may hold the address in blind mode.
func TestRecordsBlindLeavesNoAddressInCookiesOrLogs(t *testing.T) {
	for _, screen := range []bool{false, true} {
		t.Run(fmt.Sprintf("SigninScreen=%v", screen), func(t *testing.T) {
			var logs bytes.Buffer
			fm := &failOnceMailer{}
			a, _, _ := newRecordsAuth(t, func(c *Config) {
				c.Blind, c.Mailer, c.SigninScreen = true, fm, screen
				c.Logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			})
			var cookies []*http.Cookie
			see := func(w *httptest.ResponseRecorder) { cookies = append(cookies, w.Result().Cookies()...) }

			// A magic-link Begin whose send fails: the error names the recipient.
			see(beginSignin(t, a, "Ana@Example.com"))
			if !strings.Contains(logs.String(), "begin sign-in") {
				t.Fatalf("the failed send was not logged, so the log check proves nothing: %s", logs.String())
			}
			// A magic-link sign-in.
			subjectOf(t, a, signInSeeing(t, a, &fm.captureMailer, "Ana@Example.com", see))

			// A keymail sign-in.
			f := newKeymailFake()
			f.servers["keymail.test"] = true
			f.delegate["example.com"] = "keymail.test"
			f.claimed["kim@example.com"] = true
			f.address = "kim@example.com"
			wireKeymail(a, f)
			b := newBrowser()
			do := func(h http.HandlerFunc, method, target string, form url.Values) *httptest.ResponseRecorder {
				w := httptest.NewRecorder()
				r := b.request(method, target, form)
				r.URL.Scheme, r.URL.Host, r.Host = "https", "ppl.test", "ppl.test"
				h(w, r)
				b.apply(w)
				see(w)
				return w
			}
			w := do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"kim@example.com"}})
			pending := b.cookie(a.pendingCookie())
			if pending == nil || !strings.HasPrefix(pending.Value, "v1.") {
				t.Fatalf("pending cookie %v: want a sealedcookie envelope", pending)
			}
			authorize := w.Header().Get("Location")
			if screen {
				// The Continue page resolves the authorize URL through
				// continuationFor, which must open the sealed pending
				// cookie to compare it.
				r := b.request(http.MethodGet, authorize, nil)
				st := a.SigninState(r)
				if st.ContinueURL == "" {
					t.Fatalf("Begin → %q: the continuation did not resolve", authorize)
				}
				authorize = st.ContinueURL
			}
			cb := do(a.Callback, http.MethodGet, "/auth/callback?code=c&state="+stateOf(t, authorize), nil)
			if sessionCookieSet(a, cb) == nil || sessionCookieSet(a, cb).Value == "" {
				t.Fatalf("keymail callback: %d %q, no session", cb.Code, cb.Header().Get("Location"))
			}

			for _, c := range cookies {
				for _, s := range readings(c.Value) {
					if strings.Contains(strings.ToLower(s), "example.com") {
						t.Errorf("cookie %s carries the address: %q", c.Name, s)
					}
				}
			}
			if strings.Contains(strings.ToLower(logs.String()), "example.com") {
				t.Errorf("the log carries the address:\n%s", logs.String())
			}
		})
	}
}

// hookStore runs after once, the first time a create under prefix
// succeeds, before that create returns.
type hookStore struct {
	objrec.Store
	prefix string
	once   sync.Once
	after  func()
}

func (h *hookStore) Create(ctx context.Context, key string, body []byte) (objrec.Version, error) {
	v, err := h.Store.Create(ctx, key, body)
	if err == nil && strings.HasPrefix(key, h.prefix) {
		h.once.Do(h.after)
	}
	return v, err
}

func TestRecordsWinnerOfTheClaimStillSignsIn(t *testing.T) {
	a, _, store := newRecordsAuth(t, nil)
	var (
		second    User
		secondErr error
	)
	// Right after the first ensure's claim lands, a second device runs a
	// whole ensure: it finds the claim, finds no user, and repairs it,
	// so the first device's own user create then meets ErrExists.
	a.users.store = &hookStore{Store: store, prefix: "emails/", after: func() {
		second, secondErr = a.users.ensure(context.Background(), "ana@example.com")
	}}
	first, err := a.users.ensure(context.Background(), "ana@example.com")
	if err != nil || secondErr != nil {
		t.Fatalf("first: %v, second: %v", err, secondErr)
	}
	if first.ID == "" || first.ID != second.ID || first.Epoch != second.Epoch || !first.Created.Equal(second.Created) {
		t.Fatalf("first %+v, second %+v: want the same user", first, second)
	}
	if n := keysUnder(t, store, "users/"); n != 1 {
		t.Fatalf("%d users/ records, want 1", n)
	}
}

func TestEndSessionsOnADeletedUserWritesNothing(t *testing.T) {
	a, m, store := newRecordsAuth(t, nil)
	c := signInAt(t, a, m, "ana@example.com")
	uid := subjectOf(t, a, c)
	if err := store.Delete(context.Background(), "users/"+uid+".json"); err != nil {
		t.Fatal(err)
	}
	if err := a.EndSessions(context.Background(), uid); !errors.Is(err, sessions.ErrNoUser) {
		t.Fatalf("EndSessions on a deleted user: %v, want sessions.ErrNoUser", err)
	}
	if n := keysUnder(t, store, "users/"); n != 0 {
		t.Fatalf("EndSessions re-created %d users/ records", n)
	}
	if err := a.EndSessions(context.Background(), "../emails/x"); !errors.Is(err, sessions.ErrNoUser) {
		t.Fatalf("EndSessions on a malformed uid: %v, want sessions.ErrNoUser", err)
	}
	w := postWith(a.SignoutEverywhere, "/signout-everywhere", c)
	if sc := sessionCookieSet(a, w); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/signin" || sc == nil || sc.MaxAge >= 0 {
		t.Fatalf("SignoutEverywhere: %d %q, cookie %v; want 303 to /signin with the cookie cleared", w.Code, w.Header().Get("Location"), sc)
	}
	if n := keysUnder(t, store, "users/"); n != 0 {
		t.Fatalf("SignoutEverywhere re-created %d users/ records", n)
	}
}

// Pins what the docs promise: ordinary sign-out ends this browser only.
func TestSignoutLeavesACopyRefreshableUntilEverywhere(t *testing.T) {
	a, m, _ := newRecordsAuth(t, nil)
	frozen := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return frozen }
	c := signInAt(t, a, m, "ana@example.com")
	copied := *c

	w := postWith(a.Signout, "/signout", c)
	if sc := sessionCookieSet(a, w); sc == nil || sc.MaxAge >= 0 {
		t.Fatalf("Signout did not clear the cookie: %v", sc)
	}

	a.now = func() time.Time { return frozen.Add(16 * time.Minute) }
	w = refreshWith(a, &copied)
	fresh := sessionCookieSet(a, w)
	if w.Header().Get("Location") != "/" || fresh == nil || fresh.Value == "" {
		t.Fatalf("Refresh with the copy after Signout: %q, cookie %v; want it reissued", w.Header().Get("Location"), fresh)
	}

	if w := postWith(a.SignoutEverywhere, "/signout-everywhere", fresh); w.Code != http.StatusSeeOther {
		t.Fatalf("SignoutEverywhere: %d", w.Code)
	}
	a.now = func() time.Time { return frozen.Add(32 * time.Minute) }
	if loc := refreshWith(a, &copied).Header().Get("Location"); !strings.HasPrefix(loc, "/signin") {
		t.Fatalf("the copy refreshed after SignoutEverywhere: %q", loc)
	}
}

// failUpdates fails every Update, as a store outage would.
type failUpdates struct{ objrec.Store }

func (failUpdates) Update(context.Context, string, []byte, objrec.Version) (objrec.Version, error) {
	return "", errors.New("store down")
}

func TestSignoutEverywhereOnAStoreErrorSaysSo(t *testing.T) {
	a, m, store := newRecordsAuth(t, nil)
	c := signInAt(t, a, m, "ana@example.com")
	uid := subjectOf(t, a, c)
	before, _ := a.User(context.Background(), uid)
	a.users.store = failUpdates{store}
	w := postWith(a.SignoutEverywhere, "/signout-everywhere", c)
	if w.Code != http.StatusServiceUnavailable || len(w.Result().Cookies()) != 0 {
		t.Fatalf("%d, cookies %v: want 503 and no cookie set", w.Code, w.Result().Cookies())
	}
	if after, _ := a.User(context.Background(), uid); after.Epoch != before.Epoch {
		t.Fatalf("epoch %d to %d through a failing store", before.Epoch, after.Epoch)
	}
}

func TestRecordsRefusedAddressWritesNoUser(t *testing.T) {
	a, m, store := newRecordsAuth(t, func(c *Config) { c.Authorize = func(string) bool { return false } })
	beginSignin(t, a, "ana@example.com")
	link := recordsLinkRE.FindString(m.sentBody())
	if link == "" {
		t.Fatal("no link sent")
	}
	w := httptest.NewRecorder()
	a.Verify(w, redeemRequest(t, link))
	if w.Code != http.StatusForbidden {
		t.Fatalf("redeem: %d, want the 403 refusal", w.Code)
	}
	if u, e := keysUnder(t, store, "users/"), keysUnder(t, store, "emails/"); u != 0 || e != 0 {
		t.Fatalf("a refused address wrote %d users/ and %d emails/ records", u, e)
	}
}

func TestSignoutEverywhereEndsEveryCopyOfTheSession(t *testing.T) {
	a, m, _ := newRecordsAuth(t, nil)
	frozen := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return frozen }
	c := signInAt(t, a, m, "ana@example.com")
	copied := *c
	uid := subjectOf(t, a, c)
	before, err := a.User(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	w := postWith(a.SignoutEverywhere, "/signout-everywhere", c)
	if sc := sessionCookieSet(a, w); w.Code != http.StatusSeeOther || w.Header().Get("Location") != a.cfg.SigninPath || sc == nil || sc.MaxAge >= 0 {
		t.Fatalf("%d %q, cookie %v: want 303 to sign-in with the cookie cleared", w.Code, w.Header().Get("Location"), sc)
	}
	if after, _ := a.User(context.Background(), uid); after.Epoch != before.Epoch+1 {
		t.Fatalf("epoch %d to %d, want up by one", before.Epoch, after.Epoch)
	}
	a.now = func() time.Time { return frozen.Add(16 * time.Minute) }
	if loc := refreshWith(a, &copied).Header().Get("Location"); !strings.HasPrefix(loc, "/signin") {
		t.Fatalf("a copy refreshed after SignoutEverywhere: %q", loc)
	}
}

// A POST with no valid session must change nothing: a 303 to sign-in that
// has quietly ended no one's sessions is a false success.
func TestSignoutEverywhereWithNoSessionEndsNothing(t *testing.T) {
	a, m, _ := newRecordsAuth(t, nil)
	uid := subjectOf(t, a, signInAt(t, a, m, "ana@example.com"))
	before, _ := a.User(context.Background(), uid)
	r := httptest.NewRequest("POST", "https://ppl.test/signout-everywhere", nil)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	w := httptest.NewRecorder()
	a.SignoutEverywhere(w, r) // no cookie
	after, _ := a.User(context.Background(), uid)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != a.cfg.SigninPath || after.Epoch != before.Epoch {
		t.Fatalf("%d %q, epoch %d to %d: want 303 to sign-in and no epoch change", w.Code, w.Header().Get("Location"), before.Epoch, after.Epoch)
	}
}

// A page idle past 15 minutes holds an expired cookie. It must be refreshed
// first (the POST is repeated after the 307), not answered as signed out.
func TestSignoutEverywhereOnAnExpiredCookieRefreshesFirst(t *testing.T) {
	a, m, _ := newRecordsAuth(t, nil)
	frozen := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return frozen }
	c := signInAt(t, a, m, "ana@example.com")
	uid := subjectOf(t, a, c)
	before, _ := a.User(context.Background(), uid)
	a.now = func() time.Time { return frozen.Add(16 * time.Minute) }
	r := httptest.NewRequest("POST", "https://ppl.test/signout-everywhere", nil)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.AddCookie(c)
	w := httptest.NewRecorder()
	a.SignoutEverywhere(w, r)
	after, _ := a.User(context.Background(), uid)
	want := "https://ppl.test/_carlos/refresh?next=" + url.QueryEscape("/signout-everywhere")
	if w.Code != http.StatusTemporaryRedirect || w.Header().Get("Location") != want || after.Epoch != before.Epoch {
		t.Fatalf("%d %q, epoch %d to %d: want 307 to %q with the epoch unchanged", w.Code, w.Header().Get("Location"), before.Epoch, after.Epoch, want)
	}
}

func TestRecordsSpendLinksSaysUnavailable(t *testing.T) {
	a, _, _ := newRecordsAuth(t, nil)
	if _, err := a.SpendLinks(context.Background(), "ana@example.com"); !errors.Is(err, ErrRecords) {
		t.Fatalf("got %v", err)
	}
}

// An expired cookie on the centre refreshes like one on a tenant,
// through either gate, instead of being sent to sign in.
func TestRecordsRequireSessionRefreshesAnExpiredCookie(t *testing.T) {
	a, m, _ := newRecordsAuth(t, nil)
	frozen := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return frozen }
	c := signInAt(t, a, m, "ana@example.com")
	a.now = func() time.Time { return frozen.Add(16 * time.Minute) }
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	for name, h := range map[string]http.Handler{
		"RequireSession":      a.RequireSession(ok),
		"RequireFreshSession": a.RequireFreshSession(time.Hour)(ok),
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "https://ppl.test/account", nil)
		r.AddCookie(c)
		h.ServeHTTP(w, r)
		if want := "https://ppl.test/_carlos/refresh?next=%2Faccount"; w.Code != http.StatusSeeOther || w.Header().Get("Location") != want {
			t.Errorf("%s: %d %q, want 303 to %q", name, w.Code, w.Header().Get("Location"), want)
		}
	}
}

// RequireFreshSession judges freshness on auth's clock in records mode,
// the clock the cookie was minted and verified on.
func TestRecordsRequireFreshSessionUsesTheAuthClock(t *testing.T) {
	a, m, _ := newRecordsAuth(t, nil)
	// An hour behind the wall clock: on time.Now this session is stale.
	start := time.Now().Add(-time.Hour)
	a.now = func() time.Time { return start }
	c := signInAt(t, a, m, "ana@example.com")
	a.now = func() time.Time { return start.Add(time.Minute) }
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "https://ppl.test/settings", nil)
	r.AddCookie(c)
	a.RequireFreshSession(10*time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})).ServeHTTP(w, r)
	if w.Code != http.StatusTeapot {
		t.Fatalf("%d %q: a session a minute old on auth's clock was judged stale", w.Code, w.Header().Get("Location"))
	}
}

func TestRecordsSweepDropsOldLinks(t *testing.T) {
	a, _, store := newRecordsAuth(t, nil)
	frozen := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return frozen }
	beginSignin(t, a, "ana@example.com")
	if keysUnder(t, store, "links/") != 1 {
		t.Fatal("no link was stored")
	}
	if err := a.Sweep(frozen.Add(72 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n := keysUnder(t, store, "links/"); n != 0 {
		t.Fatalf("%d links/ records after a sweep three days on", n)
	}
}

// The flow stamps a link's expiry and the link store judges it. On a
// clock two hours ahead of the wall, a flow left on time.Now would
// stamp links that are already expired by the store's clock.
func TestRecordsOneClockForLinksAndCookies(t *testing.T) {
	a, m, _ := newRecordsAuth(t, nil)
	ahead := time.Now().Add(2 * time.Hour)
	a.now = func() time.Time { return ahead }
	signInAt(t, a, m, "ana@example.com")
}

// reopenBlind is the centre restarted on the same store and pepper with
// Blind turned on.
func reopenBlind(t *testing.T, cfg Config) *Auth {
	t.Helper()
	cfg.Blind = true
	b, err := New(cfg)
	if err != nil {
		t.Fatalf("New with Blind: %v", err)
	}
	return b
}

// emailMember reports whether users/<uid>.json has an email member.
func emailMember(t *testing.T, store objrec.Store, uid string) bool {
	t.Helper()
	body, _, err := store.Get(context.Background(), "users/"+uid+".json")
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal(body, &rec); err != nil {
		t.Fatal(err)
	}
	_, has := rec["email"]
	return has
}

// A centre that turns Blind on keeps the users it has. The addresses
// they were stored with must go: on the next read of each user, from
// EndSessions, and from Sweep for the users nobody reads.
func TestRecordsBlindStripsAddressesStoredBefore(t *testing.T) {
	var cfg Config
	plain, _, store := newRecordsAuth(t, func(c *Config) { cfg = *c })
	ctx := context.Background()
	uids := map[string]string{}
	for _, name := range []string{"ana", "bob", "cy", "dee", "eve"} {
		u, err := plain.users.ensure(ctx, name+"@example.com")
		if err != nil {
			t.Fatal(err)
		}
		if !emailMember(t, store, u.ID) {
			t.Fatalf("users/%s.json holds no address before Blind: the checks below prove nothing", u.ID)
		}
		uids[name] = u.ID
	}
	blind := reopenBlind(t, cfg)

	// User: one read.
	u, err := blind.User(ctx, uids["ana"])
	if err != nil || u.Email != "" || emailMember(t, store, uids["ana"]) {
		t.Fatalf("User: %+v %v, email stored %v; want no address returned or stored", u, err, emailMember(t, store, uids["ana"]))
	}
	// EndSessions: the epoch moves and the address is not written back.
	before, _, _ := objrec.GetJSON[User](ctx, store, "users/"+uids["bob"]+".json")
	if err := blind.EndSessions(ctx, uids["bob"]); err != nil {
		t.Fatal(err)
	}
	after, _, _ := objrec.GetJSON[User](ctx, store, "users/"+uids["bob"]+".json")
	if after.Epoch != before.Epoch+1 || emailMember(t, store, uids["bob"]) {
		t.Fatalf("EndSessions: epoch %d to %d, email stored %v; want up by one and no address", before.Epoch, after.Epoch, emailMember(t, store, uids["bob"]))
	}
	// Sign-in and refresh read the user too.
	if u, err := blind.users.ensure(ctx, "cy@example.com"); err != nil || u.Email != "" || emailMember(t, store, uids["cy"]) {
		t.Fatalf("sign-in: %+v %v; want no address returned or stored", u, err)
	}
	if _, err := blind.users.epoch(ctx, uids["dee"]); err != nil || emailMember(t, store, uids["dee"]) {
		t.Fatalf("refresh: %v; want no address stored", err)
	}
	// Sweep: eve was never read.
	if !emailMember(t, store, uids["eve"]) {
		t.Fatal("eve's address went before the sweep: the sweep check proves nothing")
	}
	if err := blind.Sweep(time.Now()); err != nil {
		t.Fatal(err)
	}
	assertNoAddress(t, store, "example.com")
}

// raceUpdates runs before once, the first time an Update under prefix
// is called, before that Update reaches the store.
type raceUpdates struct {
	objrec.Store
	prefix string
	once   sync.Once
	before func()
}

func (r *raceUpdates) Update(ctx context.Context, key string, body []byte, v objrec.Version) (objrec.Version, error) {
	if strings.HasPrefix(key, r.prefix) {
		r.once.Do(r.before)
	}
	return r.Store.Update(ctx, key, body, v)
}

// A write between the strip's read and its swap (here an instance not
// yet blind ending the user's sessions) must neither be lost nor leave
// the address behind.
func TestRecordsBlindStripSurvivesARacingWriter(t *testing.T) {
	var cfg Config
	plain, _, store := newRecordsAuth(t, func(c *Config) { cfg = *c })
	ctx := context.Background()
	u, err := plain.users.ensure(ctx, "ana@example.com")
	if err != nil {
		t.Fatal(err)
	}
	blind := reopenBlind(t, cfg)
	raced := false
	blind.users.store = &raceUpdates{Store: store, prefix: "users/", before: func() {
		if err := plain.EndSessions(ctx, u.ID); err != nil {
			t.Error(err)
		}
		raced = true
	}}
	got, err := blind.User(ctx, u.ID)
	if !raced {
		t.Fatal("no Update raced the strip: the test proves nothing")
	}
	stored, _, _ := objrec.GetJSON[User](ctx, store, "users/"+u.ID+".json")
	if err != nil || got.Email != "" || emailMember(t, store, u.ID) || stored.Epoch != u.Epoch+1 || got.Epoch != stored.Epoch {
		t.Fatalf("User: %+v %v; stored %+v; want no address and the racing epoch %d kept", got, err, stored, u.Epoch+1)
	}
}
