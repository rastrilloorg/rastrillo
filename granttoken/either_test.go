package granttoken

import (
	"context"
	"net/http"
	"strings"
	"testing"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
	"amadan.net/rastrillo/rastrillo/apitoken"
	"amadan.net/rastrillo/rastrillo/crypto"
)

var (
	_ api.Verifier  = (*Either)(nil)
	_ api.Rechecker = (*Either)(nil)
	_ Fallback      = (*apitoken.Store)(nil)
)

const personalSample = "tickets_aaaaaaaaaaaa_sample"

// fakeFallback counts calls and answers with one fixed actor.
type fakeFallback struct {
	calls int
	actor rastrillo.Actor
}

func (fake *fakeFallback) Verify(
	context.Context, *http.Request,
) (rastrillo.Actor, error) {
	fake.calls++
	return fake.actor, nil
}

func (fake *fakeFallback) Recheck(
	context.Context, *http.Request, rastrillo.Actor,
) (rastrillo.Actor, error) {
	fake.calls++
	return fake.actor, nil
}

func (fake *fakeFallback) Redact(text string) string {
	return strings.ReplaceAll(text, personalSample, "tickets_[redacted]")
}

func newEither(
	t *testing.T, grants Grants,
) (*Either, *fakeFallback, *crypto.Keypair) {
	t.Helper()
	door, key, _ := newDoor(t, grants)
	fallback := &fakeFallback{actor: rastrillo.Actor{Subject: "reader"}}
	return door.Or(fallback), fallback, key
}

func TestEitherSendsGrantTokensToTheDoor(t *testing.T) {
	either, fallback, key := newEither(t, activeGrants())
	request := bearerRequest(signed(t, key, doorClaims()))
	ctx := context.Background()
	actor, err := either.Verify(ctx, request)
	if err != nil || actor.Credential != "g1" {
		t.Fatalf("Verify = %+v, %v; want grant g1", actor, err)
	}
	actor, err = either.Recheck(ctx, request, actor)
	if err != nil || actor.Credential != "g1" {
		t.Fatalf("Recheck = %+v, %v; want grant g1", actor, err)
	}
	if fallback.calls != 0 {
		t.Errorf("fallback called %d times, want 0", fallback.calls)
	}
}

func TestEitherSendsOtherTokensOn(t *testing.T) {
	either, fallback, _ := newEither(t, activeGrants())
	ctx := context.Background()
	request := bearerRequest(personalSample)
	actor, err := either.Verify(ctx, request)
	if err != nil || actor != fallback.actor {
		t.Fatalf("Verify = %+v, %v; want %+v", actor, err, fallback.actor)
	}
	actor, err = either.Recheck(ctx, request, actor)
	if err != nil || actor != fallback.actor {
		t.Fatalf("Recheck = %+v, %v; want %+v", actor, err, fallback.actor)
	}
	if fallback.calls != 2 {
		t.Errorf("fallback called %d times, want 2", fallback.calls)
	}
}

func TestEitherNeverFallsThrough(t *testing.T) {
	dashboard := doorClaims()
	dashboard.Audience = rootOrigin
	cases := []struct {
		name   string
		grants Grants
		claims Claims
		token  string
		want   error
	}{
		{"dashboard audience", activeGrants(), dashboard, "", ErrAudience},
		{"revoked", &fakeGrants{}, doorClaims(), "", ErrRevoked},
		{"garbage", activeGrants(), Claims{}, Prefix + "garbage", nil},
	}
	for _, row := range cases {
		t.Run(row.name, func(t *testing.T) {
			either, fallback, key := newEither(t, row.grants)
			token := row.token
			if token == "" {
				token = signed(t, key, row.claims)
			}
			request := bearerRequest(token)
			ctx := context.Background()
			_, err := either.Verify(ctx, request)
			refusedAs(t, err, api.ErrUnauthorized)
			if row.want != nil {
				refusedAs(t, err, row.want)
			}
			_, err = either.Recheck(ctx, request, rastrillo.Actor{})
			refusedAs(t, err, api.ErrUnauthorized)
			if fallback.calls != 0 {
				t.Errorf("fallback called %d times, want 0", fallback.calls)
			}
		})
	}
}

func TestEitherRedactsBoth(t *testing.T) {
	either, _, _ := newEither(t, activeGrants())
	text := "a " + Prefix + "eyJ2.abc-_ and " + personalSample
	want := "a " + Prefix + "[redacted] and tickets_[redacted]"
	if got := either.Redact(text); got != want {
		t.Errorf("Redact = %q, want %q", got, want)
	}
}
