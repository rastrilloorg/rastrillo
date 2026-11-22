package signin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/password"
	"amadan.net/rastrillo/rastrillo/sessions"
	"amadan.net/rastrillo/rastrillo/webauthn/authtest"
)

// strong judges the gesture alone. A session row a refresh or rotation
// minted just now, over an old or unknown gesture, is not fresh.
func TestStrong(t *testing.T) {
	test := newRig(t, nil)
	now := test.clock.now
	for _, row := range []struct {
		name    string
		session sessions.Session
		want    bool
	}{
		{"a link 5 minutes ago", sessions.Session{Method: "magiclink",
			AuthTime: now.Add(-5 * time.Minute), At: now}, true},
		{"a link 11 minutes ago", sessions.Session{Method: "magiclink",
			AuthTime: now.Add(-11 * time.Minute), At: now}, false},
		{"a new row, no gesture", sessions.Session{Method: "magiclink",
			At: now}, false},
		{"Keymail with no auth_time", sessions.Session{Method: "keymail",
			At: now}, false},
		{"Keymail a minute ago", sessions.Session{Method: "keymail",
			AuthTime: now.Add(-time.Minute), At: now}, true},
		{"a passkey with no user verification", sessions.Session{
			Method: "passkey-nouv", AuthTime: now, At: now}, false},
		{"an authenticator just now", sessions.Session{Method: "totp",
			AuthTime: now, At: now}, true},
		{"a fresh session, an old gesture", sessions.Session{
			Method: "passkey", AuthTime: now.Add(-11 * time.Minute),
			At: now}, false},
		{"a gesture in the future", sessions.Session{Method: "totp",
			AuthTime: now.Add(time.Minute), At: now}, false},
	} {
		if got := test.signins.strong(row.session); got != row.want {
			t.Errorf("%s: strong = %v, want %v", row.name, got, row.want)
		}
	}
}

// only is the one View a request rendered.
func only(t *testing.T, views *[]View) View {
	t.Helper()
	if len(*views) != 1 {
		t.Fatalf("%d views rendered, want 1", len(*views))
	}
	view := (*views)[0]
	*views = (*views)[:0]
	if view.Add == nil {
		t.Fatalf("no Add on the view: %+v", view)
	}
	return view
}

func TestAddAuthenticatorConfirmsOneCode(t *testing.T) {
	test, views := viewRig(t, nil)
	cookie := test.linkSession()
	test.get(test.signins.ShowAddAuthenticator,
		AddAuthenticatorPath+"?return_to=%2Freports", cookie)
	shown := only(t, views)
	add := shown.Add
	if shown.Status != http.StatusOK || add.Way != Authenticator ||
		add.Key == "" || !strings.Contains(add.URI, "secret="+add.Key) ||
		add.Action != AddAuthenticatorPath || add.ReturnTo != "/reports" {
		t.Fatalf("add page: %d %+v", shown.Status, add)
	}
	form := url.Values{"return_to": {"/reports"}}
	form.Set("code", test.wrongCode(add.Key))
	test.postTo(test.signins.AddAuthenticator, AddAuthenticatorPath, form,
		cookie)
	wrong := only(t, views)
	if wrong.Status != http.StatusUnprocessableEntity ||
		wrong.Add.Problem != problemWrongCode || wrong.Add.Done ||
		wrong.Add.Key != add.Key {
		t.Fatalf("wrong code: %d %+v", wrong.Status, wrong.Add)
	}
	if enrolled, _ := test.signins.codes.Enrolled(ana); enrolled {
		t.Fatal("a wrong code enrolled the authenticator")
	}
	form.Set("code", test.code(add.Key))
	test.postTo(test.signins.AddAuthenticator, AddAuthenticatorPath, form,
		cookie)
	done := only(t, views)
	if !done.Add.Done || done.Add.ReturnTo != "/reports" {
		t.Fatalf("right code: %d %+v", done.Status, done.Add)
	}
	if enrolled, _ := test.signins.codes.Enrolled(ana); !enrolled {
		t.Fatal("the right code did not enrol the authenticator")
	}
}

// Review Focus 4: two tabs open the page. Had the second replaced the
// first's secret, a code from the first tab's QR would be refused;
// Pending is reused instead, so both show one secret and either tab's
// code confirms it.
func TestTwoTabsAddingTheAuthenticator(t *testing.T) {
	test, views := viewRig(t, nil)
	cookie := test.linkSession()
	test.get(test.signins.ShowAddAuthenticator, AddAuthenticatorPath,
		cookie)
	first := only(t, views).Add
	test.get(test.signins.ShowAddAuthenticator, AddAuthenticatorPath,
		cookie)
	second := only(t, views).Add
	if first.Key != second.Key || first.URI != second.URI {
		t.Fatalf("the second tab began again: %q then %q", first.Key,
			second.Key)
	}
	test.postTo(test.signins.AddAuthenticator, AddAuthenticatorPath,
		url.Values{"code": {test.code(first.Key)}}, cookie)
	if !only(t, views).Add.Done {
		t.Fatal("the first tab's code was refused")
	}
}

func TestAddPassphraseRules(t *testing.T) {
	test, views := viewRig(t, nil)
	cookie := test.linkSession()
	for _, row := range []struct {
		name, passphrase, problem string
	}{
		{"14 characters", strings.Repeat("a", 14), problemShort},
		{"257 characters", strings.Repeat("a", 257), problemLong},
		{"257 emoji", strings.Repeat("🙂", 257), problemLong},
		{"256 emoji", strings.Repeat("🙂", 256), ""},
		{"15 emoji", strings.Repeat("🙂", 15), ""},
	} {
		test.postTo(test.signins.AddPassphrase, AddPassphrasePath,
			url.Values{"passphrase": {row.passphrase}}, cookie)
		view := only(t, views)
		if view.Add.Problem != row.problem || view.Add.Done !=
			(row.problem == "") {
			t.Errorf("%s: %d %+v", row.name, view.Status, view.Add)
		}
		if row.problem != "" &&
			view.Status != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, want 422", row.name, view.Status)
		}
	}
	hash, found, err := test.signins.passphrases.hash(
		context.Background(), ana)
	if err != nil || !found ||
		!password.Verify(hash, strings.Repeat("🙂", 15)) {
		t.Fatalf("stored passphrase: found %v, %v", found, err)
	}
}

func TestAddPassphrasePageNamesTheAddress(t *testing.T) {
	test, views := viewRig(t, nil)
	test.get(test.signins.ShowAddPassphrase, AddPassphrasePath,
		test.linkSession())
	add := only(t, views).Add
	if add.Way != Passphrase || add.Address != ana ||
		add.Action != AddPassphrasePath ||
		add.RevealURL != RevealModulePath {
		t.Fatalf("passphrase page: %+v", add)
	}
}

// postJSONTo drives a JSON endpoint at target with cookie.
func postJSONTo(
	t *testing.T, handler http.HandlerFunc, target string,
	cookie *http.Cookie, body any,
) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, testOrigin+target,
		strings.NewReader(string(encoded)))
	request.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler(response, request)
	return response
}

func TestAddPasskeyRegistersWithAuthtest(t *testing.T) {
	test, views := viewRig(t, nil)
	cookie := test.linkSession()
	test.get(test.signins.ShowAddPasskey,
		AddPasskeyPath+"?return_to=%2Freports", cookie)
	door := only(t, views).Add.Passkey
	if door == nil ||
		door.BeginPath != RegisterBeginPath+"?return_to=%2Freports" ||
		door.FinishPath != RegisterFinishPath+"?return_to=%2Freports" ||
		door.ModuleURL != WebauthnModulePath ||
		door.ScriptURL != RegisterModulePath {
		t.Fatalf("passkey door: %+v", door)
	}
	device, _ := authtest.New()
	challenge := challengeOf(t, postJSONTo(t, test.signins.RegisterBegin,
		door.BeginPath, cookie, struct{}{}))
	clientData, attestation := device.Create(challenge,
		authtest.Options{RPID: "app.test", Origin: testOrigin})
	finished := postJSONTo(t, test.signins.RegisterFinish, door.FinishPath,
		cookie, map[string]string{"clientDataJSON": encoded(clientData),
			"attestationObject": encoded(attestation)})
	var answer struct {
		OK bool   `json:"ok"`
		To string `json:"to"`
	}
	if finished.Code != http.StatusOK ||
		json.Unmarshal(finished.Body.Bytes(), &answer) != nil ||
		!answer.OK ||
		answer.To != AddPasskeyPath+"?done=1&return_to=%2Freports" {
		t.Fatalf("finish: %d %s", finished.Code, finished.Body.String())
	}
	if list, _ := test.signins.passkeys.List(ana); len(list) != 1 {
		t.Fatalf("%d passkeys listed, want 1", len(list))
	}
	// The rotated session's AuthTime is the real clock's, which may have
	// ticked past the rig's.
	test.clock.now = time.Now()
	test.get(test.signins.ShowAddPasskey, answer.To,
		carrying(finished).Cookies()[0])
	if done := only(t, views).Add; !done.Done ||
		done.ReturnTo != "/reports" {
		t.Fatalf("after the passkey: %+v", done)
	}
}

// signinRedirect is where a refused add page sent the browser.
func signinRedirect(
	t *testing.T, response *httptest.ResponseRecorder,
) url.Values {
	t.Helper()
	location := response.Header().Get("Location")
	if response.Code != http.StatusSeeOther ||
		!strings.HasPrefix(location, "/signin?") {
		t.Fatalf("got %d %q, want 303 to the sign-in page", response.Code,
			location)
	}
	return query(t, response)
}

func TestAddNeedsAStrongSession(t *testing.T) {
	test, views := viewRig(t, nil)
	link := test.linkSession()
	keymail := test.signedIn(sessions.Session{Method: "keymail"})
	test.clock.now = test.clock.now.Add(11 * time.Minute)
	stale := signinRedirect(t, test.get(test.signins.ShowAddPassphrase,
		AddPassphrasePath+"?return_to=%2Freports", link))
	if stale.Get("reauth") != "1" || stale.Has("force") ||
		stale.Get("return_to") != AddPassphrasePath+"?return_to=%2Freports" {
		t.Fatalf("stale link: %v", stale)
	}
	test.clock.now = time.Now()
	forced := signinRedirect(t, test.postTo(test.signins.AddPassphrase,
		AddPassphrasePath, url.Values{"passphrase": {"fifteen letters!"}},
		keymail))
	if forced.Get("reauth") != "1" || forced.Get("force") != "1" ||
		forced.Get("return_to") != AddPassphrasePath {
		t.Fatalf("keymail with no auth_time: %v", forced)
	}
	if _, found, _ := test.signins.passphrases.hash(context.Background(),
		ana); found || len(*views) != 0 {
		t.Fatal("a refused request stored a passphrase or drew a page")
	}
	begun := postJSONTo(t, test.signins.RegisterBegin, RegisterBeginPath,
		keymail, struct{}{})
	if got := reauthOf(t, begun); got != "/signin?reauth=1&return_to="+
		url.QueryEscape(AddPasskeyPath)+"&force=1" {
		t.Fatalf("register begin, keymail: reauth %q", got)
	}
}

// reauthOf is the sign-in page a refused passkey request names.
func reauthOf(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var answer struct {
		Reauth string `json:"reauth"`
	}
	if response.Code != http.StatusUnauthorized ||
		json.Unmarshal(response.Body.Bytes(), &answer) != nil {
		t.Fatalf("got %d %s, want 401 naming the sign-in page",
			response.Code, response.Body.String())
	}
	return answer.Reauth
}

// The page loaded fresh, then the person waited: the ceremony's
// requests send them to sign in again and back to the add page.
func TestAddPasskeyGoneStaleAsksToSignInAgain(t *testing.T) {
	test, views := viewRig(t, nil)
	cookie := test.linkSession()
	test.get(test.signins.ShowAddPasskey,
		AddPasskeyPath+"?return_to=%2Freports", cookie)
	door := only(t, views).Add.Passkey
	test.clock.now = test.clock.now.Add(11 * time.Minute)
	want := "/signin?reauth=1&return_to=" +
		url.QueryEscape(AddPasskeyPath+"?return_to=%2Freports")
	for name, handler := range map[string]struct {
		run    http.HandlerFunc
		target string
	}{
		"begin":  {test.signins.RegisterBegin, door.BeginPath},
		"finish": {test.signins.RegisterFinish, door.FinishPath},
	} {
		got := reauthOf(t, postJSONTo(t, handler.run, handler.target,
			cookie, struct{}{}))
		if got != want {
			t.Errorf("%s: reauth %q, want %q", name, got, want)
		}
	}
}

func TestAddNeedsASession(t *testing.T) {
	test, _ := viewRig(t, nil)
	signedOut := signinRedirect(t, test.get(test.signins.ShowAddPasskey,
		AddPasskeyPath, nil))
	if signedOut.Has("reauth") ||
		signedOut.Get("return_to") != AddPasskeyPath {
		t.Fatalf("signed out: %v", signedOut)
	}
}

func TestAddAWayThatIsOffIsNotFound(t *testing.T) {
	test, views := viewRig(t, func(config *Config) {
		config.Ways = Passkey
	})
	response := test.get(test.signins.ShowAddPassphrase, AddPassphrasePath,
		test.linkSession())
	if response.Code != http.StatusNotFound || len(*views) != 0 {
		t.Fatalf("passphrase off: %d", response.Code)
	}
}
