package signin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/auth"
	"amadan.net/rastrillo/rastrillo/lastsignin"
	"amadan.net/rastrillo/rastrillo/pow"
	"amadan.net/rastrillo/rastrillo/pow/powtest"
)

const (
	ada        = "ada@example.com"
	stranger   = "nobody@example.com"
	adaSecret  = "correct horse battery staple"
	wrongWords = "incorrect horse battery staple"
)

func TestCodeSignsIn(t *testing.T) {
	test := newRig(t, nil)
	key := test.enrol(ada)
	response := test.post(test.signins.SignInWithCode,
		url.Values{"address": {ada}, "code": {test.code(key)}})
	if response.Code != http.StatusSeeOther ||
		response.Header().Get("Location") != "/" {
		t.Fatalf("got %d to %q, want 303 to /", response.Code,
			response.Header().Get("Location"))
	}
	if identity := test.session(response); identity.Method != "totp" {
		t.Errorf("session method %q, want totp", identity.Method)
	}
	want := lastsignin.Record{Method: "totp", Address: ada}
	if record := test.remembered(response); record != want {
		t.Errorf("remembered %+v, want %+v", record, want)
	}
}

func TestPassphraseSignsIn(t *testing.T) {
	test := newRig(t, nil)
	test.setPassphrase(ada, adaSecret)
	response := test.post(test.signins.SignInWithPassphrase,
		url.Values{"address": {ada}, "passphrase": {adaSecret}})
	if response.Code != http.StatusSeeOther ||
		response.Header().Get("Location") != "/" {
		t.Fatalf("got %d to %q, want 303 to /", response.Code,
			response.Header().Get("Location"))
	}
	if identity := test.session(response); identity.Method != "passphrase" {
		t.Errorf("session method %q, want passphrase", identity.Method)
	}
	want := lastsignin.Record{Method: "passphrase", Address: ada}
	if record := test.remembered(response); record != want {
		t.Errorf("remembered %+v, want %+v", record, want)
	}
}

func TestUnknownAddressAnswersLikeAWrongCode(t *testing.T) {
	test := newRig(t, nil)
	key := test.enrol(ada)
	wrong := test.post(test.signins.SignInWithCode, url.Values{
		"address": {ada}, "code": {test.wrongCode(key)},
		"return_to": {"/notes/42"}})
	unknown := test.post(test.signins.SignInWithCode, url.Values{
		"address": {stranger}, "code": {test.code(key)},
		"return_to": {"/notes/42"}})
	if answered(wrong) != answered(unknown) {
		t.Fatalf("wrong code %+v, unknown address %+v",
			answered(wrong), answered(unknown))
	}
	got := query(t, wrong)
	if got.Get("err") != "code" || got.Get("way") != "authenticator" ||
		got.Get("return_to") != "/notes/42" {
		t.Errorf("a wrong code lands on %q", wrong.Header().Get("Location"))
	}
}

func TestUnknownAddressAnswersLikeAWrongPassphrase(t *testing.T) {
	test := newRig(t, nil)
	test.setPassphrase(ada, adaSecret)
	wrong := test.post(test.signins.SignInWithPassphrase,
		url.Values{"address": {ada}, "passphrase": {wrongWords}})
	unknown := test.post(test.signins.SignInWithPassphrase,
		url.Values{"address": {stranger}, "passphrase": {adaSecret}})
	blank := test.post(test.signins.SignInWithPassphrase,
		url.Values{"address": {""}, "passphrase": {""}})
	if answered(wrong) != answered(unknown) ||
		answered(wrong) != answered(blank) {
		t.Fatalf("wrong %+v, unknown %+v, blank %+v", answered(wrong),
			answered(unknown), answered(blank))
	}
	got := query(t, wrong)
	if got.Get("err") != "passphrase" || got.Get("way") != "passphrase" {
		t.Errorf("a wrong passphrase lands on %q",
			wrong.Header().Get("Location"))
	}
	if test.misses() != 6 {
		t.Errorf("%d failures recorded, want 2 for each of 3 misses",
			test.misses())
	}
}

func TestPausedAnswersErrPaused(t *testing.T) {
	test := newRig(t, nil)
	test.setPassphrase(ada, adaSecret)
	for range accountMisses {
		test.post(test.signins.SignInWithPassphrase,
			url.Values{"address": {ada}, "passphrase": {wrongWords}})
	}
	response := test.post(test.signins.SignInWithPassphrase, url.Values{
		"address": {ada}, "passphrase": {adaSecret}, "reauth": {"1"}})
	got := query(t, response)
	if got.Get("err") != "paused" || got.Get("way") != "passphrase" ||
		got.Get("reauth") != "1" {
		t.Errorf("paused lands on %q", response.Header().Get("Location"))
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == test.auth.SessionCookie() {
			t.Errorf("a paused answer set a session: %v", cookie)
		}
	}
}

// TestRemovedCredentialFailsLikeAWrongOne is review focus 1: a browser
// still remembers the authenticator after its owner disabled it.
func TestRemovedCredentialFailsLikeAWrongOne(t *testing.T) {
	test := newRig(t, nil)
	key := test.enrol(ada)
	jar := httptest.NewRecorder()
	test.auth.RememberJar().Remember(jar,
		lastsignin.Record{Method: "totp", Address: ada})
	if err := test.signins.codes.Disable(ada); err != nil {
		t.Fatal(err)
	}
	response := test.post(test.signins.SignInWithCode,
		url.Values{"address": {ada}, "code": {test.code(key)}},
		jar.Result().Cookies()...)
	if response.Code != http.StatusSeeOther ||
		query(t, response).Get("err") != "code" {
		t.Fatalf("got %d to %q, want 303 with err=code", response.Code,
			response.Header().Get("Location"))
	}
}

func TestSecretSignInKeepsReturnTo(t *testing.T) {
	cases := map[string]string{
		"/notes/42":             "/notes/42",
		"https://evil.example/": "/",
	}
	for asked, want := range cases {
		test := newRig(t, nil)
		key := test.enrol(ada)
		response := test.post(test.signins.SignInWithCode, url.Values{
			"address": {ada}, "code": {test.code(key)},
			"return_to": {asked}})
		if got := response.Header().Get("Location"); got != want {
			t.Errorf("return_to %q landed on %q, want %q", asked, got, want)
		}
	}
}

func TestUnsolvedProofIsRefusedBeforeAnyCheck(t *testing.T) {
	test := newRig(t, func(config *Config) {
		guard, err := pow.New(pow.Config{InstanceKey: "test-instance-key",
			Nonces: pow.SQLNonces(config.DB), Difficulty: 8,
			MinAge:    time.Millisecond,
			ScriptURL: "/pow/pow.js", WorkerURL: "/pow/pow-worker.js"})
		if err != nil {
			t.Fatal(err)
		}
		config.Proof, config.ProofOff = guard, false
	})
	checks := 0
	counted := func(context.Context, string, string) (bool, error) {
		checks++
		return false, nil
	}
	test.signins.checkCode, test.signins.checkPassphrase = counted, counted
	for way, handler := range map[string]http.HandlerFunc{
		"authenticator": test.signins.SignInWithCode,
		"passphrase":    test.signins.SignInWithPassphrase,
	} {
		response := test.post(handler, url.Values{"address": {ada},
			"code": {"123456"}, "passphrase": {adaSecret}})
		got := query(t, response)
		if got.Get("err") != "check" || got.Get("rec") != "1" ||
			got.Get("way") != way {
			t.Errorf("unsolved %s lands on %q", way,
				response.Header().Get("Location"))
		}
	}
	if checks != 0 || test.misses() != 0 {
		t.Errorf("%d checks and %d failures before the proof", checks,
			test.misses())
	}
}

// TestSolvedProofSignsIn is the screen's own challenge, solved, letting
// a code through: the secret doors carry the scope Begin's form does.
func TestSolvedProofSignsIn(t *testing.T) {
	database := schema.Open(t)
	guard, err := pow.New(pow.Config{InstanceKey: "test-instance-key",
		Nonces: pow.SQLNonces(database), Difficulty: 8,
		MinAge:    time.Millisecond,
		ScriptURL: "/pow/pow.js", WorkerURL: "/pow/pow-worker.js"})
	if err != nil {
		t.Fatal(err)
	}
	authority := newAuth(t, database, func(config *auth.Config) {
		config.Proof, config.ProofOff = guard, false
	})
	config := baseConfig(t, database, authority, &clock{time.Now()})
	config.Proof, config.ProofOff = guard, false
	signins, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	test := &rig{t: t, database: database, auth: authority,
		signins: signins, clock: &clock{time.Now()}}
	key := test.enrol(ada)
	state := authority.SigninState(
		httptest.NewRequest(http.MethodGet, "/signin", nil))
	time.Sleep(2 * time.Millisecond)
	form := powtest.Fill(t, []byte(state.Proof.Fields()),
		url.Values{"address": {ada}, "code": {test.code(key)}})
	response := test.post(signins.SignInWithCode, form)
	if response.Header().Get("Location") != "/" {
		t.Fatalf("a solved proof landed on %q, want /",
			response.Header().Get("Location"))
	}
}

func TestAnUnknownSubjectsCodeIsCheckedAgainstTheDecoy(t *testing.T) {
	test := newRig(t, nil)
	key := test.enrol(ada)
	decoys := 0
	work := test.signins.checkDecoyCode
	test.signins.checkDecoyCode = func(submitted string) {
		decoys++
		work(submitted)
	}
	test.post(test.signins.SignInWithCode,
		url.Values{"address": {stranger}, "code": {test.code(key)}})
	if decoys != 1 {
		t.Fatalf("an unknown address ran %d decoy checks, want 1", decoys)
	}
	test.post(test.signins.SignInWithCode,
		url.Values{"address": {ada}, "code": {test.wrongCode(key)}})
	if decoys != 1 {
		t.Fatalf("an enrolled address ran a decoy check too")
	}
}

// A miss sends the browser back with the address it typed in the field,
// even over another one it remembers, and never in the URL.
func TestAMissKeepsTheTypedAddress(t *testing.T) {
	test := newRig(t, nil)
	key := test.enrol(ada)
	jar := httptest.NewRecorder()
	test.auth.RememberJar().Remember(jar,
		lastsignin.Record{Method: "totp", Address: "bea@example.com"})
	response := test.post(test.signins.SignInWithCode,
		url.Values{"address": {ada}, "code": {test.wrongCode(key)}},
		jar.Result().Cookies()...)
	if strings.Contains(response.Header().Get("Location"), "ada") {
		t.Fatalf("the address rode the URL: %q",
			response.Header().Get("Location"))
	}
	back := carrying(response)
	for _, cookie := range jar.Result().Cookies() {
		back.AddCookie(cookie)
	}
	if state := test.auth.SigninState(back); state.Address != ada {
		t.Fatalf("the secret door holds %q, want %q", state.Address, ada)
	}
}
