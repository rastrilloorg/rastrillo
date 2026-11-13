package auth

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/sessions"
)

type blindErrorLimiter struct{ err error }

func (limiter blindErrorLimiter) Allow(context.Context, string,
	time.Time, int,
) (bool, error) {
	return false, limiter.err
}

func TestBlindLinksReturnBareBudgetAndAddressSentinels(t *testing.T) {
	for _, sentinel := range []error{ErrRateLimited, ErrBadAddress} {
		harness := newBlindHarness(t, true)
		harness.auth.flow.Limiter = blindErrorLimiter{
			err: fmt.Errorf("%s: %w", blindCanary, sentinel),
		}
		err := harness.auth.SendLink(context.Background(), blindCanary, "test")
		if err != sentinel {
			t.Errorf("returned a wrapped error: %v", err)
		}
		assertBlindBytes(t, "safe sentinel", []byte(err.Error()))
	}
}

func TestBlindLinksRefuseLegacyAddressSessions(t *testing.T) {
	harness := newBlindHarness(t, true)
	token, err := harness.auth.sessions.Mint(sessions.Session{
		Subject: strings.TrimSpace(blindCanary), Method: "magiclink",
	})
	requireBlindSuccess(t, err)
	request := httptest.NewRequest("GET", "http://app.test/private", nil)
	request.AddCookie(&http.Cookie{
		Name: harness.auth.SessionCookie(), Value: token,
	})
	if _, ok := harness.auth.SessionFrom(request); ok {
		t.Error("blind SessionFrom accepted an address subject")
	}
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("blind middleware admitted an address subject")
	})
	harness.auth.RequireSession(handler).ServeHTTP(
		httptest.NewRecorder(), request.Clone(context.Background()))
	harness.auth.RequireFreshSession(time.Hour)(handler).ServeHTTP(
		httptest.NewRecorder(), request.Clone(context.Background()))
}

func TestBlindLinksAdmissionRefusalIsNotAServerError(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(fmt.Sprint(custom), func(t *testing.T) {
			harness := newBlindHarness(t, true)
			harness.config.BlindLinks.ResolveSubject = func(context.Context,
				string) (string, error) {
				return "", fmt.Errorf("%s: %w", blindCanary, sessions.ErrNoUser)
			}
			called := false
			if custom {
				harness.config.Refused = func(response http.ResponseWriter,
					_ *http.Request, identity Identity) {
					called = true
					key, _ := blindKey(context.Background(), blindCanary)
					if identity.Address != key {
						t.Error("refusal lost the verified key")
					}
					response.WriteHeader(http.StatusForbidden)
				}
			}
			harness.configure(t)
			link := harness.send(t)
			response := httptest.NewRecorder()
			harness.auth.Verify(response, redeemRequest(t, link))
			if response.Code != http.StatusForbidden || called != custom {
				t.Fatalf("refusal: status=%d, callback=%v",
					response.Code, called)
			}
			if strings.Contains(harness.logs.String(), "level=ERROR") {
				t.Error("admission refusal logged as a server error")
			}
			assertBlindResponse(t, response)
			harness.scan(t)
			response = httptest.NewRecorder()
			harness.auth.Verify(response, redeemRequest(t, link))
			if response.Header().Get("Location") != "/signin?err=expired" {
				t.Error("refused admission left its token reusable")
			}
		})
	}
}

func TestBlindLinksKeepReturnToAndOpaqueKeyCase(t *testing.T) {
	harness := newBlindHarness(t, true)
	key := "MiXeD_base64URL-0123"
	harness.config.BlindLinks.LookupKey = func(context.Context,
		string) (string, error) {
		return key, nil
	}
	harness.config.BlindLinks.ResolveSubject = func(_ context.Context,
		got string) (string, error) {
		if got != key {
			t.Errorf("lookup key changed: %q", got)
		}
		return blindSubject, nil
	}
	harness.configure(t)
	browser := newBrowser()
	response := browser.do(harness.auth.Begin, "POST", "/signin", url.Values{
		"address": {blindCanary}, "return_to": {"/vault?tab=shared"},
		"expect": {"keymail"},
	})
	assertBlindResponse(t, response)
	request := redeemRequest(t, linkRE.FindString(harness.mail.sentBody()))
	for _, cookie := range response.Result().Cookies() {
		request.AddCookie(cookie)
	}
	response = httptest.NewRecorder()
	harness.auth.Verify(response, request)
	if response.Header().Get("Location") != "/vault?tab=shared" {
		t.Fatalf("lost return_to: %q", response.Header().Get("Location"))
	}
	assertBlindResponse(t, response)
}

func TestBlindLinksSnapshotConfigAndUseTheFlowClock(t *testing.T) {
	harness := newBlindHarness(t, true)
	harness.config.BlindLinks.ResolveSubject = func(context.Context,
		string) (string, error) {
		return "changed", nil
	}
	now := time.Now().Add(time.Hour)
	harness.auth.now = func() time.Time { return now }
	link := harness.send(t)
	var expiry int64
	if err := harness.store.database.QueryRow(
		"SELECT expires FROM blind_links").Scan(&expiry); err != nil {
		t.Fatal(err)
	}
	if expiry != now.Add(LinkTTL).UnixNano() {
		t.Fatal("flow and adapter used different clocks")
	}
	response := httptest.NewRecorder()
	harness.auth.Verify(response, redeemRequest(t, link))
	requireBlindSession(t, harness.auth, response)
}

func TestBlindLinksNeverEchoBadOrRateLimitedAddresses(t *testing.T) {
	harness := newBlindHarness(t, true)
	malformed := strings.TrimSpace(blindCanary) + "\r\nBcc: another@example.net"
	err := harness.auth.SendLink(context.Background(), malformed, "test")
	if !errors.Is(err, ErrBadAddress) {
		t.Fatal("bad address accepted")
	}
	assertBlindBytes(t, "invalid address error", []byte(err.Error()))
	assertBlindResponse(t, beginSignin(t, harness.auth, malformed))
	for range 5 {
		harness.send(t)
	}
	response := beginSignin(t, harness.auth, blindCanary)
	if !strings.Contains(response.Header().Get("Location"), "err=rate") {
		t.Fatal("sixth begin did not hit the rate limit")
	}
	assertBlindResponse(t, response)
	harness.scan(t)
}

func TestBlindLinksCaptureBothFallbackLoggers(t *testing.T) {
	harness := newBlindHarness(t, true)
	log.Print("standard logger probe")
	slog.Default().Error("structured logger probe")
	for _, marker := range []string{"standard logger probe",
		"structured logger probe"} {
		if !strings.Contains(harness.logs.String(), marker) {
			t.Fatalf("did not capture %s", marker)
		}
	}
}
