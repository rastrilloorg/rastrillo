package signin

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/auth"
	"amadan.net/rastrillo/rastrillo/dbtest"
	"amadan.net/rastrillo/rastrillo/lastsignin"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/objrec"
	"amadan.net/rastrillo/rastrillo/passkey"
	"amadan.net/rastrillo/rastrillo/pow"
	"amadan.net/rastrillo/rastrillo/sessions"
	"amadan.net/rastrillo/rastrillo/totp"
)

const testOrigin = "http://app.test"

var schema = dbtest.FromSet(migrate.Merge(
	sessions.Schema, auth.Schema, totp.Schema, passkey.Schema,
	pow.Schema, Schema,
))

func TestMain(run *testing.M) {
	code := run.Run()
	schema.Remove()
	os.Exit(code)
}

type quietMailer struct{}

func (quietMailer) Send(context.Context, string, string, string) error {
	return nil
}

// clock is the time signins judges misses by. It starts at the real
// time because totp checks codes against the real clock.
type clock struct{ now time.Time }

func (moment *clock) Now() time.Time { return moment.now }

type rig struct {
	t        *testing.T
	database *sql.DB
	auth     *auth.Auth
	signins  *Signins
	clock    *clock
}

func newAuth(
	t *testing.T, database *sql.DB, change func(*auth.Config),
) *auth.Auth {
	t.Helper()
	config := auth.Config{
		DB: database, Origin: testOrigin, InstanceKey: "test-instance-key",
		Mailer: quietMailer{}, SigninScreen: true, SigninOneWay: true,
		ProofOff: true,
	}
	if change != nil {
		change(&config)
	}
	authority, err := auth.New(config)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	return authority
}

func baseConfig(
	t *testing.T, database *sql.DB, authority *auth.Auth, moment *clock,
) Config {
	t.Helper()
	store, err := sessions.New(sessions.Config{
		DB: database, Origin: testOrigin,
	})
	if err != nil {
		t.Fatalf("sessions.New: %v", err)
	}
	return Config{
		Auth: authority, Sessions: store, DB: database, Origin: testOrigin,
		Key:    [32]byte{1, 2, 3},
		Issuer: "Test", Ways: Passkey | Authenticator | Passphrase,
		ProofOff: true,
		Render:   func(http.ResponseWriter, *http.Request, View) {},
		Logger:   slog.New(slog.DiscardHandler), Now: moment.Now,
	}
}

func newRig(t *testing.T, change func(*Config)) *rig {
	t.Helper()
	database := schema.Open(t)
	moment := &clock{now: time.Now()}
	authority := newAuth(t, database, nil)
	config := baseConfig(t, database, authority, moment)
	if change != nil {
		change(&config)
	}
	signins, err := New(config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &rig{t: t, database: database, auth: authority,
		signins: signins, clock: moment}
}

// enrol confirms an authenticator for subject with the previous step's
// code, so the current step's code is still unspent for a sign-in.
func (test *rig) enrol(subject string) string {
	test.t.Helper()
	enrolment, err := test.signins.codes.Begin(subject, subject)
	if err != nil {
		test.t.Fatalf("Begin: %v", err)
	}
	code, _ := totp.Code(enrolment.Key, time.Now().Add(-30*time.Second))
	confirmed, err := test.signins.codes.Confirm(subject, code)
	if err != nil || !confirmed {
		test.t.Fatalf("Confirm: %v, %v", confirmed, err)
	}
	return enrolment.Key
}

func (test *rig) code(key string) string {
	test.t.Helper()
	code, err := totp.Code(key, time.Now())
	if err != nil {
		test.t.Fatal(err)
	}
	return code
}

// wrongCode is the current code with its last digit moved on.
func (test *rig) wrongCode(key string) string {
	code := test.code(key)
	last := (code[len(code)-1]-'0'+1)%10 + '0'
	return code[:len(code)-1] + string(rune(last))
}

func (test *rig) setPassphrase(subject, passphrase string) {
	test.t.Helper()
	err := test.signins.passphrases.set(
		context.Background(), subject, passphrase)
	if err != nil {
		test.t.Fatalf("set passphrase: %v", err)
	}
}

func (test *rig) post(
	handler http.HandlerFunc, form url.Values, cookies ...*http.Cookie,
) *httptest.ResponseRecorder {
	return test.postFrom("192.0.2.1", handler, form, cookies...)
}

func (test *rig) postFrom(
	ip string, handler http.HandlerFunc, form url.Values,
	cookies ...*http.Cookie,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, testOrigin+"/signin/x",
		strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.RemoteAddr = ip + ":4242"
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler(response, request)
	return response
}

// carrying is a later request from the browser that got response.
func carrying(response *httptest.ResponseRecorder) *http.Request {
	request := httptest.NewRequest(http.MethodGet, testOrigin+"/", nil)
	for _, cookie := range response.Result().Cookies() {
		if cookie.MaxAge >= 0 {
			request.AddCookie(cookie)
		}
	}
	return request
}

func (test *rig) session(response *httptest.ResponseRecorder) auth.Identity {
	test.t.Helper()
	identity, ok := test.auth.SessionFrom(carrying(response))
	if !ok {
		test.t.Fatalf("no session after %d %s", response.Code,
			response.Header().Get("Location"))
	}
	return identity
}

func (test *rig) remembered(
	response *httptest.ResponseRecorder,
) lastsignin.Record {
	test.t.Helper()
	record, result := test.auth.RememberJar().Read(carrying(response))
	if result != lastsignin.Valid {
		test.t.Fatalf("nothing remembered: %v", result)
	}
	return record
}

func (test *rig) misses() int {
	test.t.Helper()
	var count int
	err := test.database.QueryRow(
		`SELECT COUNT(*) FROM signin_failures`).Scan(&count)
	if err != nil {
		test.t.Fatal(err)
	}
	return count
}

func query(t *testing.T, response *httptest.ResponseRecorder) url.Values {
	t.Helper()
	location, err := url.Parse(response.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return location.Query()
}

type holdNothing struct{}

func (holdNothing) hold(
	http.ResponseWriter, *http.Request, sessions.Session,
) (bool, error) {
	return false, nil
}

type unusedBlindStore struct{}

func (unusedBlindStore) Put(context.Context, auth.BlindLink) error {
	return nil
}

func (unusedBlindStore) Take(
	context.Context, string,
) (auth.BlindLink, bool, error) {
	return auth.BlindLink{}, false, nil
}

func (unusedBlindStore) Spend(context.Context, string) (int, error) {
	return 0, nil
}

func (unusedBlindStore) Sweep(context.Context, time.Time) error {
	return nil
}

func blindLinks() *auth.BlindLinks {
	opaque := func(context.Context, string) (string, error) {
		return "opaque", nil
	}
	return &auth.BlindLinks{Store: unusedBlindStore{},
		LookupKey: opaque, ResolveSubject: opaque}
}

func recordsAuth(t *testing.T) *auth.Auth {
	t.Helper()
	key, err := sessions.NewSigningKey("k1")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := auth.New(auth.Config{
		Origin: "https://ppl.test", InstanceKey: "test-instance-key",
		ProofOff: true, Mailer: quietMailer{},
		Records: objrec.Dir(t.TempDir()), Pepper: bytes.Repeat([]byte{7}, 32),
		Signed: &sessions.Signed{Name: "ppl", Key: key},
	})
	if err != nil {
		t.Fatalf("auth.New in records mode: %v", err)
	}
	return authority
}

func TestNewRefusesRecordsBlindAndSecondFactor(t *testing.T) {
	database := schema.Open(t)
	blind := newAuth(t, database, func(config *auth.Config) {
		config.SigninScreen, config.SigninOneWay = false, false
		config.BlindLinks = blindLinks()
	})
	second := newAuth(t, database, func(config *auth.Config) {
		config.SecondFactor = holdNothing{}.hold
	})
	for name, authority := range map[string]*auth.Auth{
		"Records": recordsAuth(t), "BlindLinks": blind,
		"SecondFactor": second,
	} {
		config := baseConfig(t, database, authority, &clock{time.Now()})
		_, err := New(config)
		want := authority.Assembles()
		if want == nil || err == nil || err.Error() != want.Error() {
			t.Errorf("%s: New answered %v, want auth's %v", name, err, want)
		}
	}
}

func TestNewNeedsLookupWithSubjectFor(t *testing.T) {
	database := schema.Open(t)
	authority := newAuth(t, database, func(config *auth.Config) {
		config.SubjectFor = func(address string) (string, error) {
			return "user:" + address, nil
		}
	})
	config := baseConfig(t, database, authority, &clock{time.Now()})
	if _, err := New(config); err == nil ||
		!strings.Contains(err.Error(), "Lookup") {
		t.Fatalf("New without Lookup: %v, want a refusal naming Lookup", err)
	}
	config.Lookup = func(
		_ context.Context, address string,
	) (string, bool, error) {
		return "user:" + address, true, nil
	}
	config.Authorize = func(string) bool { return true }
	if _, err := New(config); err != nil {
		t.Fatalf("New with Lookup: %v", err)
	}
}

func TestNewRefusesNoRender(t *testing.T) {
	database := schema.Open(t)
	authority := newAuth(t, database, nil)
	guard, err := pow.New(pow.Config{InstanceKey: "test-instance-key",
		Nonces: pow.SQLNonces(database), Difficulty: 8,
		ScriptURL: "/pow/pow.js", WorkerURL: "/pow/pow-worker.js"})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Config){
		"no Render": func(config *Config) { config.Render = nil },
		"neither Proof nor ProofOff": func(config *Config) {
			config.ProofOff = false
		},
		"both Proof and ProofOff": func(config *Config) {
			config.Proof = guard
		},
	}
	for name, change := range cases {
		config := baseConfig(t, database, authority, &clock{time.Now()})
		change(&config)
		if _, err := New(config); err == nil {
			t.Errorf("New with %s succeeded, want a refusal", name)
		}
	}
}

func TestFillSetsTheDoorsOfTheWaysOn(t *testing.T) {
	everything := newRig(t, nil)
	var state auth.SigninState
	everything.signins.Fill(&state)
	if state.Passkey == nil || state.Passkey.BeginPath != PasskeyBeginPath ||
		state.Passkey.ModuleURL != WebauthnModulePath ||
		state.Passkey.ScriptURL != PasskeyModulePath {
		t.Errorf("passkey door = %+v", state.Passkey)
	}
	if state.Authenticator == nil || state.Authenticator.Path != CodePath ||
		state.Passphrase == nil || state.Passphrase.Path != PassphrasePath {
		t.Errorf("secret doors = %+v, %+v",
			state.Authenticator, state.Passphrase)
	}
	codesOnly := newRig(t, func(config *Config) {
		config.Ways = Authenticator
	})
	state = auth.SigninState{}
	codesOnly.signins.Fill(&state)
	if state.Passkey != nil || state.Passphrase != nil ||
		state.Authenticator == nil {
		t.Errorf("authenticator only filled %+v", state)
	}
}

// answer is what a visitor can see of a response: its status, where it
// sends them, and which cookies it sets.
type answer struct {
	status   int
	location string
	cookies  string
}

func answered(response *httptest.ResponseRecorder) answer {
	var names []string
	for _, cookie := range response.Result().Cookies() {
		names = append(names, cookie.Name)
	}
	slices.Sort(names)
	return answer{response.Code, response.Header().Get("Location"),
		strings.Join(names, ",")}
}
