package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/sessions"
)

type blindFailMailer struct{}

func (blindFailMailer) Send(context.Context, string, string, string) error {
	return errors.New(blindCanary)
}

func TestBlindLinksRedactFailureLogsAndReturnedErrors(t *testing.T) {
	for _, failure := range []string{"mail", "lookup", "second-factor"} {
		t.Run(failure, func(t *testing.T) {
			harness := newBlindHarness(t, true)
			switch failure {
			case "mail":
				harness.config.Mailer = blindFailMailer{}
			case "lookup":
				harness.config.BlindLinks.LookupKey = func(context.Context,
					string) (string, error) {
					return "", errors.New(blindCanary)
				}
			case "second-factor":
				harness.config.SecondFactor = func(http.ResponseWriter,
					*http.Request, sessions.Session) (bool, error) {
					return false, errors.New(blindCanary)
				}
			}
			harness.configure(t)
			assertNoBlindCookies(t, beginSignin(t, harness.auth, blindCanary))
			if failure == "second-factor" {
				link := linkRE.FindString(harness.mail.sentBody())
				request := redeemRequest(t, link)
				recorder := httptest.NewRecorder()
				harness.auth.Verify(recorder, request)
				assertNoBlindCookies(t, recorder)
			} else {
				err := harness.auth.SendLink(
					context.Background(), blindCanary, "test",
				)
				if err == nil {
					t.Fatal("SendLink hid failure")
				}
				assertBlindBytes(t, "returned error", []byte(err.Error()))
			}
			if harness.logs.Len() == 0 {
				t.Fatal("failure was not logged")
			}
			harness.scan(t)
		})
	}
}

func TestBlindLinksDisableKeymailAndAddressCookies(t *testing.T) {
	harness := newBlindHarness(t, true)
	if harness.auth.flow.Classifier != nil || harness.auth.flow.Keymail != nil {
		t.Fatal("blind mode retained address-caching Keymail clients")
	}
	response := beginSignin(t, harness.auth, blindCanary)
	assertNoBlindCookies(t, response)
	response = httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/auth/callback", nil)
	harness.auth.Callback(response, request)
	if response.Header().Get("Location") != "/signin?err=expired" {
		t.Fatal("blind callback did not refuse the Keymail path")
	}
}

func TestBlindLinksSpendAndSweepUseTheConsumerStore(t *testing.T) {
	harness := newBlindHarness(t, false)
	harness.send(t)
	count, err := harness.auth.SpendLinks(context.Background(), blindCanary)
	if err != nil || count != 1 {
		t.Fatalf("SpendLinks = %d, %v; want 1", count, err)
	}
	harness.send(t)
	if err := harness.auth.Sweep(time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := harness.store.database.QueryRow(
		"SELECT count(*) FROM blind_links").Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatal("Sweep left expired links")
	}
	harness.scan(t)
}

func TestBlindLinksHaveOneConcurrentRedeemer(t *testing.T) {
	harness := newBlindHarness(t, true)
	link := harness.send(t)
	var workers sync.WaitGroup
	results := make(chan bool, 12)
	for range 12 {
		request := redeemRequest(t, link)
		workers.Go(func() {
			response := httptest.NewRecorder()
			harness.auth.Verify(response, request)
			results <- response.Header().Get("Location") == "/"
		})
	}
	workers.Wait()
	close(results)
	wins := 0
	for success := range results {
		if success {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("concurrent redemptions = %d, want 1", wins)
	}
}

func TestBlindLinksDoNotLogUntrustedAttributes(t *testing.T) {
	harness := newBlindHarness(t, true)
	harness.auth.cfg.Logger.With("recipient", blindCanary).
		WithGroup("request").Error(
		"auth failure", "err", errors.New(blindCanary),
	)
	if !strings.Contains(harness.logs.String(), "auth failure") {
		t.Fatal("redaction lost the diagnostic event")
	}
	assertBlindBytes(t, "log attributes", harness.logs.Bytes())
}

func TestBlindLinksRejectPendingKeymailFromBeforeTheSwitch(t *testing.T) {
	harness := newBlindHarness(t, true)
	legacyConfig := harness.config
	legacyConfig.BlindLinks = nil
	legacy, err := New(legacyConfig)
	requireBlindSuccess(t, err)
	wireKeymail(legacy, kayFake())
	browser := newBrowser()
	response := beginKeymail(legacy, browser, "kay@example.org")
	state := stateOf(t, response.Header.Get("Location"))
	request := browser.request("GET", "/auth/callback?code=c&state="+
		url.QueryEscape(state), nil)
	if _, ok := harness.auth.pendingValue(request); ok {
		t.Fatal("blind mode accepted a legacy address-bearing pending blob")
	}
	recorder := httptest.NewRecorder()
	harness.auth.Callback(recorder, request)
	if recorder.Header().Get("Location") != "/signin?err=expired" {
		t.Fatal("blind mode admitted a legacy Keymail callback")
	}
}

func TestBlindLinksRejectLookupValuesReturnedWithAnError(t *testing.T) {
	harness := newBlindHarness(t, true)
	harness.config.BlindLinks.LookupKey = func(ctx context.Context,
		address string) (string, error) {
		key, _ := blindKey(ctx, address)
		return key, errors.New(blindCanary)
	}
	harness.configure(t)
	err := harness.auth.SendLink(context.Background(), blindCanary, "test")
	if err == nil {
		t.Fatal("lookup error with a valid key was ignored")
	}
	assertBlindBytes(t, "lookup error", []byte(err.Error()))
	harness.scan(t)
}
