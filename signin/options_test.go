package signin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/webauthn/authtest"
)

// options is the Options a request drew.
func options(t *testing.T, views *[]View) *Options {
	t.Helper()
	if len(*views) != 1 || (*views)[0].Options == nil {
		t.Fatalf("want one Options view, got %+v", *views)
	}
	shown := (*views)[0].Options
	*views = (*views)[:0]
	return shown
}

func requestWith(cookie *http.Cookie) *http.Request {
	request := httptest.NewRequest(http.MethodGet, testOrigin+"/", nil)
	request.AddCookie(cookie)
	return request
}

func (test *rig) removeWay(
	way, id string, cookie *http.Cookie,
) int {
	test.t.Helper()
	form := url.Values{"way": {way}}
	if id != "" {
		form.Set("id", id)
	}
	return test.postTo(test.signins.RemoveWay, OptionsRemovePath, form,
		cookie).Code
}

func TestOptionsListsEmailAlways(t *testing.T) {
	test, views := viewRig(t, nil)
	test.get(test.signins.ShowOptions, OptionsPath, test.linkSession())
	shown := options(t, views)
	if shown.Address != ana || shown.RemovePath != OptionsRemovePath {
		t.Fatalf("options: %+v", shown)
	}
	// The email link has no row, so it has no way to be removed; every
	// row is a way the app turned on, and none is had yet.
	if len(shown.Ways) != 3 {
		t.Fatalf("%d rows, want 3: %+v", len(shown.Ways), shown.Ways)
	}
	for _, row := range shown.Ways {
		if row.Have || row.AddPath == "" {
			t.Errorf("row %+v: want an Add link and nothing to remove", row)
		}
	}
	wantAdd := AddPasskeyPath + "?return_to=%2Faccount%2Fsignin"
	if shown.Ways[0].AddPath != wantAdd {
		t.Errorf("passkey add path %q", shown.Ways[0].AddPath)
	}
	if response := test.postTo(test.signins.RemoveWay, OptionsRemovePath,
		url.Values{"way": {"email"}}, test.linkSession()); response.Code !=
		http.StatusBadRequest {
		t.Errorf("removing email: %d, want 400", response.Code)
	}
}

func TestOptionsOnlyListsTheWaysThatAreOn(t *testing.T) {
	test, views := viewRig(t, func(config *Config) {
		config.Ways = Passphrase
	})
	test.get(test.signins.ShowOptions, OptionsPath, test.linkSession())
	if rows := options(t, views).Ways; len(rows) != 1 ||
		rows[0].Way != Passphrase {
		t.Fatalf("rows: %+v", rows)
	}
	if code := test.removeWay("authenticator", "", test.linkSession()); code !=
		http.StatusNotFound {
		t.Fatalf("a way that is off: %d, want 404", code)
	}
}

func TestOptionsAddsAndRemoves(t *testing.T) {
	test, views := viewRig(t, nil)
	cookie := test.linkSession()
	test.enrol(ana)
	test.setPassphrase(ana, "a long passphrase of mine")
	first, _ := authtest.New()
	second, _ := authtest.New()
	test.registerPasskey(ana, first)
	test.registerPasskey(ana, second)
	test.get(test.signins.ShowOptions, OptionsPath, cookie)
	rows := options(t, views).Ways
	if len(rows) != 4 || !rows[0].Have || !rows[1].Have ||
		rows[0].ID == rows[1].ID || rows[0].Way != Passkey {
		t.Fatalf("rows with everything on: %+v", rows)
	}
	ids := []string{rows[0].ID, rows[1].ID}

	for _, removal := range []struct {
		way, id string
		check   func() bool // true while the way is still there
	}{
		{"passkey", ids[0], func() bool {
			list, _ := test.signins.passkeys.List(ana)
			return len(list) != 1 || list[0].ID != ids[1]
		}},
		{"authenticator", "", func() bool {
			has, _ := test.signins.codes.Enrolled(ana)
			return has
		}},
		{"passphrase", "", func() bool {
			_, has, _ := test.signins.passphrases.hash(
				context.Background(), ana)
			return has
		}},
	} {
		response := test.postTo(test.signins.RemoveWay, OptionsRemovePath,
			url.Values{"way": {removal.way}, "id": {removal.id}}, cookie)
		if response.Code != http.StatusSeeOther ||
			response.Header().Get("Location") != OptionsPath+"?removed=1" ||
			removal.check() {
			t.Fatalf("remove %s: %d %q, still there: %v", removal.way,
				response.Code, response.Header().Get("Location"),
				removal.check())
		}
	}
	test.get(test.signins.ShowOptions, OptionsPath+"?removed=1", cookie)
	shown := options(t, views)
	if !shown.Removed || len(shown.Ways) != 3 || !shown.Ways[0].Have ||
		shown.Ways[1].Have || shown.Ways[2].Have ||
		shown.Ways[1].AddPath == "" || shown.Ways[2].AddPath == "" {
		t.Fatalf("after removing: %+v", shown)
	}
}

func TestRemovingSomeoneElsesPasskeyIsNotFound(t *testing.T) {
	test, _ := viewRig(t, nil)
	device, _ := authtest.New()
	test.registerPasskey("bo@example.com", device)
	list, _ := test.signins.passkeys.List("bo@example.com")
	if code := test.removeWay("passkey", list[0].ID,
		test.linkSession()); code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", code)
	}
	if list, _ = test.signins.passkeys.List("bo@example.com"); len(list) != 1 {
		t.Fatal("the passkey went")
	}
}

func TestRemovingNeedsAStrongSession(t *testing.T) {
	test, _ := viewRig(t, nil)
	test.enrol(ana)
	stale := test.linkSession()
	test.clock.now = test.clock.now.Add(11 * time.Minute)
	redirect := signinRedirect(t, test.postTo(test.signins.RemoveWay,
		OptionsRemovePath, url.Values{"way": {"authenticator"}}, stale))
	if redirect.Get("reauth") != "1" ||
		redirect.Get("return_to") != OptionsPath {
		t.Fatalf("stale session: %v", redirect)
	}
	if has, _ := test.signins.codes.Enrolled(ana); !has {
		t.Fatal("a stale session removed the authenticator")
	}
}

func TestOptionsRefusesSignedOut(t *testing.T) {
	test, views := viewRig(t, nil)
	redirect := signinRedirect(t, test.get(test.signins.ShowOptions,
		OptionsPath, nil))
	if redirect.Has("reauth") || redirect.Get("return_to") != OptionsPath ||
		len(*views) != 0 {
		t.Fatalf("signed out: %v", redirect)
	}
	redirect = signinRedirect(t, test.postTo(test.signins.RemoveWay,
		OptionsRemovePath, url.Values{"way": {"passphrase"}}, nil))
	if redirect.Get("return_to") != OptionsPath {
		t.Fatalf("signed-out removal returns to %v", redirect)
	}
}

// Review Focus 5: a link user adds an authenticator from the offer,
// removes it, and signs in by link again. Adding answered the offer, so
// it does not come back, though they never pressed "Not now".
func TestRemovingTheLastWayDoesNotBringTheOfferBack(t *testing.T) {
	test, views := viewRig(t, nil)
	cookie := test.linkSession()
	session, _ := test.signins.session(requestWith(cookie))
	if due, _ := test.signins.offerDue(context.Background(),
		session); !due {
		t.Fatal("the offer should be due before anything is added")
	}
	test.get(test.signins.ShowAddAuthenticator, AddAuthenticatorPath, cookie)
	key := only(t, views).Add.Key
	test.postTo(test.signins.AddAuthenticator, AddAuthenticatorPath,
		url.Values{"code": {test.code(key)}}, cookie)
	if !only(t, views).Add.Done {
		t.Fatal("the authenticator was not added")
	}
	if code := test.removeWay("authenticator", "", cookie); code !=
		http.StatusSeeOther {
		t.Fatalf("remove: %d", code)
	}
	again, _ := test.signins.session(requestWith(test.linkSession()))
	due, err := test.signins.offerDue(context.Background(), again)
	if err != nil || due {
		t.Fatalf("offerDue after removing the last way = %v, %v", due, err)
	}
}

// When the offer's answer cannot be written the way is taken back out
// and the person gets a 500: a way is never kept without its answer, or
// removing it later would bring the offer back.
func TestAddingWithoutTheOfferMarkerEnrolsNothing(t *testing.T) {
	test, views := viewRig(t, nil)
	cookie := test.linkSession()
	test.get(test.signins.ShowAddAuthenticator, AddAuthenticatorPath, cookie)
	key := only(t, views).Add.Key
	device, _ := authtest.New()
	begun := postJSONTo(t, test.signins.RegisterBegin, RegisterBeginPath,
		cookie, struct{}{})
	challenge := challengeOf(t, begun)
	if _, err := test.database.Exec(`DROP TABLE signin_offers`); err != nil {
		t.Fatal(err)
	}
	responses := map[string]int{
		"authenticator": test.postTo(test.signins.AddAuthenticator,
			AddAuthenticatorPath, url.Values{"code": {test.code(key)}},
			cookie).Code,
		"passphrase": test.postTo(test.signins.AddPassphrase,
			AddPassphrasePath, url.Values{
				"passphrase": {"a long passphrase of mine"}}, cookie).Code,
	}
	// Last, because a passkey that registers rotates the session.
	clientData, attestation := device.Create(challenge,
		authtest.Options{RPID: "app.test", Origin: testOrigin})
	responses["passkey"] = postJSONTo(t, test.signins.RegisterFinish,
		RegisterFinishPath, cookie, map[string]string{
			"clientDataJSON":    encoded(clientData),
			"attestationObject": encoded(attestation)}).Code
	for way, code := range responses {
		if code != http.StatusInternalServerError {
			t.Errorf("%s: got %d, want 500", way, code)
		}
	}
	if list, _ := test.signins.passkeys.List(ana); len(list) != 0 {
		t.Error("a passkey was stored")
	}
	if has, _ := test.signins.codes.Enrolled(ana); has {
		t.Error("the authenticator was enrolled")
	}
	if _, has, _ := test.signins.passphrases.hash(context.Background(),
		ana); has {
		t.Error("a passphrase was stored")
	}
}

// A passphrase-only app needs no session store of its own, so it may
// leave Sessions unset; its pages must still see who is signed in.
func TestPassphraseOnlyUsesAuthsSessions(t *testing.T) {
	test, views := viewRig(t, func(config *Config) {
		config.Ways, config.Sessions = Passphrase, nil
	})
	response := test.get(test.signins.ShowOptions, OptionsPath,
		test.authSession())
	if response.Code != http.StatusOK {
		t.Fatalf("options: %d to %q, want 200", response.Code,
			response.Header().Get("Location"))
	}
	if shown := options(t, views); shown.Address != ana {
		t.Fatalf("options: %+v", shown)
	}
}

// Only a way that was added answers the offer: a wrong code, a passkey
// the server refused or a passphrase too short leaves it due, so a slip
// and a closed tab do not lose it for good.
func TestAFailedAddLeavesTheOfferDue(t *testing.T) {
	test, views := viewRig(t, nil)
	cookie := test.linkSession()
	stillDue := func(after string) {
		t.Helper()
		session, _ := test.signins.session(requestWith(cookie))
		due, err := test.signins.offerDue(context.Background(), session)
		if err != nil || !due {
			t.Errorf("after %s: offer due = %v, %v; want due", after, due,
				err)
		}
	}
	test.get(test.signins.ShowAddAuthenticator, AddAuthenticatorPath, cookie)
	key := only(t, views).Add.Key
	test.postTo(test.signins.AddAuthenticator, AddAuthenticatorPath,
		url.Values{"code": {test.wrongCode(key)}}, cookie)
	stillDue("a wrong code")
	test.postTo(test.signins.AddPassphrase, AddPassphrasePath,
		url.Values{"passphrase": {"too short"}}, cookie)
	stillDue("a short passphrase")
	device, _ := authtest.New()
	challenge := challengeOf(t, postJSONTo(t, test.signins.RegisterBegin,
		RegisterBeginPath, cookie, struct{}{}))
	clientData, attestation := device.Create(challenge,
		authtest.Options{RPID: "app.test", Origin: "http://elsewhere.test"})
	refused := postJSONTo(t, test.signins.RegisterFinish, RegisterFinishPath,
		cookie, map[string]string{"clientDataJSON": encoded(clientData),
			"attestationObject": encoded(attestation)})
	if refused.Code != http.StatusBadRequest {
		t.Fatalf("a passkey from elsewhere: %d, want 400", refused.Code)
	}
	stillDue("a refused passkey")
}
