package signin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/sessions"
	"amadan.net/rastrillo/rastrillo/webauthn/authtest"
)

const ana = "ana@example.com"

// noWays is a table's way of asking for no way on at all, since its
// zero means every way.
const noWays Ways = 1 << 7

// viewRig is a rig whose Render keeps every View it is handed, so a
// test reads what a page would draw.
func viewRig(t *testing.T, change func(*Config)) (*rig, *[]View) {
	t.Helper()
	views := &[]View{}
	test := newRig(t, func(config *Config) {
		config.Render = func(
			response http.ResponseWriter, _ *http.Request, view View,
		) {
			*views = append(*views, view)
			response.WriteHeader(view.Status)
		}
		if change != nil {
			change(config)
		}
	})
	return test, views
}

// signedIn mints session for subject and returns its cookie. The row's
// At is the sessions store's own clock, the real time.
func (test *rig) signedIn(session sessions.Session) *http.Cookie {
	test.t.Helper()
	if session.Subject == "" {
		session.Subject = ana
	}
	store := test.signins.config.Sessions
	token, err := store.Mint(session)
	if err != nil {
		test.t.Fatalf("Mint: %v", err)
	}
	return &http.Cookie{Name: store.CookieName(), Value: token}
}

// linkSession is a session an emailed link just minted: method
// magiclink, its AuthTime the moment auth's Admit stamped.
func (test *rig) linkSession() *http.Cookie {
	return test.signedIn(sessions.Session{Method: "magiclink",
		AuthTime: test.clock.now})
}

func (test *rig) get(
	handler http.HandlerFunc, target string, cookie *http.Cookie,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, testOrigin+target, nil)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler(response, request)
	return response
}

func (test *rig) postTo(
	handler http.HandlerFunc, path string, form url.Values,
	cookie *http.Cookie,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, testOrigin+path,
		strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler(response, request)
	return response
}

func (test *rig) dismissals() int {
	test.t.Helper()
	var count int
	err := test.database.QueryRow(
		`SELECT COUNT(*) FROM signin_offers`).Scan(&count)
	if err != nil {
		test.t.Fatal(err)
	}
	return count
}

func TestOfferDueOnlyForLinkSignInsWithNoWay(t *testing.T) {
	for _, row := range []struct {
		name, method string
		has          func(*rig)
		ways         Ways // 0 leaves every way on
		want         bool
	}{
		{"a link, nothing added", "magiclink", nil, 0, true},
		{"Keymail", "keymail", nil, 0, false},
		{"a passkey", "passkey", nil, 0, false},
		{"an authenticator", "totp", nil, 0, false},
		{"a link, a passkey added", "magiclink", func(test *rig) {
			device, _ := authtest.New()
			test.registerPasskey(ana, device)
		}, 0, false},
		{"a link, an authenticator added", "magiclink", func(test *rig) {
			test.enrol(ana)
		}, 0, false},
		{"a link, a passphrase set", "magiclink", func(test *rig) {
			test.setPassphrase(ana, "fifteen letters!")
		}, 0, false},
		{"a link, not now chosen", "magiclink", func(test *rig) {
			test.dismiss(ana)
		}, 0, false},
		{"a link, no way on", "magiclink", nil, noWays, false},
		{"a link, the passphrase off and set", "magiclink",
			func(test *rig) { test.setPassphrase(ana, "fifteen letters!") },
			Passkey | Authenticator, true},
	} {
		t.Run(row.name, func(t *testing.T) {
			test := newRig(t, func(config *Config) {
				if row.ways != 0 {
					config.Ways = row.ways &^ noWays
				}
			})
			if row.has != nil {
				row.has(test)
			}
			due, err := test.signins.offerDue(context.Background(),
				sessions.Session{Subject: ana, Method: row.method})
			if err != nil || due != row.want {
				t.Fatalf("offerDue = %v, %v; want %v", due, err, row.want)
			}
		})
	}
}

// dismiss chooses "Not now" for subject the way the page does.
func (test *rig) dismiss(subject string) {
	test.t.Helper()
	err := test.signins.dismiss(context.Background(), subject)
	if err != nil {
		test.t.Fatalf("dismiss: %v", err)
	}
}

func TestLaterRecordsAndNeverAsksAgain(t *testing.T) {
	test, _ := gateRig(t)
	cookie := test.linkSession()
	if response, _ := reaches(test, page("/reports", cookie)); response.
		Header().Get("Location") != OfferPath+"?return_to=%2Freports" {
		t.Fatalf("before Not now: %d %q", response.Code,
			response.Header().Get("Location"))
	}
	for range 2 {
		response := test.postTo(test.signins.LaterOffer, OfferLaterPath,
			url.Values{"return_to": {"/reports"}}, cookie)
		if response.Code != http.StatusSeeOther ||
			response.Header().Get("Location") != "/reports" {
			t.Fatalf("Not now: %d %q", response.Code,
				response.Header().Get("Location"))
		}
	}
	if count := test.dismissals(); count != 1 {
		t.Fatalf("%d dismissals recorded, want 1", count)
	}
	if _, reached := reaches(test, page("/reports", cookie)); !reached {
		t.Fatal("the offer came back after Not now")
	}
}

func TestLaterNeedsASession(t *testing.T) {
	test, _ := gateRig(t)
	response := test.postTo(test.signins.LaterOffer, OfferLaterPath,
		url.Values{"return_to": {"/reports"}}, nil)
	if response.Code != http.StatusSeeOther || test.dismissals() != 0 {
		t.Fatalf("signed out Not now: %d, %d dismissals", response.Code,
			test.dismissals())
	}
}

func TestGateShowsTheOfferAfterTheSteps(t *testing.T) {
	name := &needs{}
	test, _ := gateRig(t, Step{Path: "/welcome/name", Needed: name.needed})
	cookie := test.linkSession()
	response, _ := reaches(test, page("/reports", cookie))
	if got := response.Header().Get("Location"); got !=
		"/welcome/name?return_to=%2Freports" {
		t.Fatalf("first stop %q, want the step", got)
	}
	name.done = true
	response, reached := reaches(test, page("/reports", cookie))
	if reached || response.Header().Get("Location") !=
		OfferPath+"?return_to=%2Freports" {
		t.Fatalf("after the step: %d %q", response.Code,
			response.Header().Get("Location"))
	}
}

func TestOfferPageCarriesTheWaysOn(t *testing.T) {
	test, views := viewRig(t, func(config *Config) {
		config.Ways = Authenticator | Passphrase
	})
	response := test.get(test.signins.ShowOffer,
		OfferPath+"?return_to=%2Freports", test.linkSession())
	if response.Code != http.StatusOK || len(*views) != 1 {
		t.Fatalf("offer: %d, %d views", response.Code, len(*views))
	}
	offer := (*views)[0].Offer
	want := Offer{Ways: Authenticator | Passphrase,
		LaterPath: OfferLaterPath, ReturnTo: "/reports"}
	if offer == nil || *offer != want {
		t.Fatalf("Offer = %+v, want %+v", offer, want)
	}
}

func TestOfferPageGoesOnWhenNotDue(t *testing.T) {
	test, views := viewRig(t, nil)
	cookie := test.signedIn(sessions.Session{Method: "keymail",
		AuthTime: time.Now()})
	response := test.get(test.signins.ShowOffer,
		OfferPath+"?return_to=%2Freports", cookie)
	if response.Code != http.StatusSeeOther || len(*views) != 0 ||
		response.Header().Get("Location") != "/reports" {
		t.Fatalf("offer to Keymail: %d %q", response.Code,
			response.Header().Get("Location"))
	}
}
