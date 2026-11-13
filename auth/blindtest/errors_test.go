package blindtest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/auth"
	"github.com/keymaildev/signin"
)

func TestBlindErrorsHaveDistinctIdentities(t *testing.T) {
	sentinels := map[string]error{
		"configuration": auth.ErrBlindConfiguration,
		"operation":     auth.ErrBlindOperation,
		"invalid-link":  signin.ErrLinkInvalid,
	}
	for name, sentinel := range sentinels {
		for other, target := range sentinels {
			want := name == other
			wrapped := fmt.Errorf("caller: %w", sentinel)
			if errors.Is(sentinel, target) != want ||
				errors.Is(wrapped, target) != want {
				t.Errorf("%s matched %s: want %v", name, other, want)
			}
		}
		if errors.Is(errors.New(sentinel.Error()), sentinel) {
			t.Errorf("%s matched an unrelated error with the same message",
				name)
		}
	}
}

func requireBlindError(t *testing.T, err, want, other error) {
	t.Helper()
	if err == nil || !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
	if errors.Is(err, other) || errors.Is(err, signin.ErrLinkInvalid) {
		t.Error("blind error also matches another category")
	}
	if errors.Unwrap(err) != nil {
		t.Error("blind error exposes an underlying failure")
	}
	message := strings.ToLower(err.Error())
	for _, address := range []string{blindCanary,
		strings.TrimSpace(blindCanary), strings.ToLower(blindCanary),
		strings.ToLower(strings.TrimSpace(blindCanary))} {
		if strings.Contains(message, strings.ToLower(address)) {
			t.Error("blind error contains the address")
		}
	}
}

func TestBlindConfigurationErrorsDoNotContainConfigValues(t *testing.T) {
	harness := newBlindHarness(t, true)
	harness.config.KeymailServers = []string{blindCanary}
	_, err := auth.New(harness.config)
	requireBlindError(t, err, auth.ErrBlindConfiguration,
		auth.ErrBlindOperation)
}

type errorMail struct{ failure error }

func (sender errorMail) Send(context.Context, string, string, string) error {
	return sender.failure
}

type errorStore struct {
	auth.BlindLinkStore
	failure error
}

func (store errorStore) Put(context.Context, auth.BlindLink) error {
	return store.failure
}

func (store errorStore) Spend(context.Context, string) (int, error) {
	return 0, store.failure
}

func (store errorStore) Sweep(context.Context, time.Time) error {
	return store.failure
}

func TestBlindOperationErrorsDiscardAddressBearingCauses(t *testing.T) {
	for _, operation := range []string{"mail", "put", "spend", "sweep"} {
		t.Run(operation, func(t *testing.T) {
			harness := newBlindHarness(t, true)
			cause := fmt.Errorf("%s: %w", blindCanary,
				auth.ErrBlindConfiguration)
			if operation == "mail" {
				harness.config.Mailer = errorMail{cause}
			} else {
				harness.config.BlindLinks.Store = errorStore{
					BlindLinkStore: harness.store, failure: cause,
				}
			}
			harness.configure(t)
			var err error
			switch operation {
			case "mail", "put":
				err = harness.auth.SendLink(
					context.Background(), blindCanary, "test",
				)
			case "spend":
				_, err = harness.auth.SpendLinks(
					context.Background(), blindCanary,
				)
			case "sweep":
				err = harness.auth.Sweep(time.Now())
			}
			requireBlindError(t, err, auth.ErrBlindOperation,
				auth.ErrBlindConfiguration)
			if errors.Is(err, cause) {
				t.Error("operation error retains its address-bearing cause")
			}
		})
	}
}
