package granttoken

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/op"
)

var (
	_ api.Verifier  = (*Door)(nil)
	_ api.Rechecker = (*Door)(nil)
)

// fakeGrants records each question and answers from active, or with err.
type fakeGrants struct {
	active map[[2]string]bool
	err    error
	asked  [][2]string
}

func (fake *fakeGrants) GrantActive(
	_ context.Context, subject, grant string,
) (bool, error) {
	fake.asked = append(fake.asked, [2]string{subject, grant})
	if fake.err != nil {
		return false, fake.err
	}
	return fake.active[[2]string{subject, grant}], nil
}

func activeGrants() *fakeGrants {
	return &fakeGrants{active: map[[2]string]bool{{"person_1", "g1"}: true}}
}

func doorClaims() Claims {
	claims := goodClaims()
	claims.Grant = "g1"
	return claims
}

// newDoor returns a door whose clock reads *now, a minute after issue.
func newDoor(
	t *testing.T, grants Grants,
) (*Door, *crypto.Keypair, *time.Time) {
	t.Helper()
	key, verifier := setup(t)
	now := time.Unix(issuedAt+60, 0)
	door, err := NewDoor(DoorConfig{
		Verifier: verifier, Grants: grants,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return door, key, &now
}

func signed(t *testing.T, key *crypto.Keypair, claims Claims) string {
	t.Helper()
	token, err := Sign(key, claims)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func bearerRequest(token string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, siteOrigin+"/api/x", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

// refusedAs fails unless err wraps every one of wants and holds none of
// the token's own text.
func refusedAs(t *testing.T, err error, wants ...error) {
	t.Helper()
	if err == nil {
		t.Fatal("err = nil, want a refusal")
	}
	for _, want := range wants {
		if !errors.Is(err, want) {
			t.Errorf("err = %v, want it to wrap %v", err, want)
		}
	}
	for _, submitted := range []string{"person_1", "Claude", Prefix} {
		if strings.Contains(err.Error(), submitted) {
			t.Errorf("err = %q holds %q", err, submitted)
		}
	}
}

func TestDoorVerifiesAGrantToken(t *testing.T) {
	door, key, _ := newDoor(t, activeGrants())
	request := bearerRequest(signed(t, key, doorClaims()))
	actor, err := door.Verify(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	want := rastrillo.Actor{
		Subject: "person_1", Agent: "Claude", Credential: "g1",
		Access: rastrillo.Write,
		Grant: &rastrillo.Grant{
			ID: "g1", Issuer: rootOrigin, IdentityIssuer: rootOrigin,
			Audience: siteOrigin,
		},
	}
	if !reflect.DeepEqual(actor, want) {
		t.Errorf("Verify = %#v, want %#v", actor, want)
	}
	wantString := "agent:Claude for user:person_1 grant:g1"
	if got := actor.String(); got != wantString {
		t.Errorf("String() = %q, want %q", got, wantString)
	}
}

func TestDoorReadGrantIsRead(t *testing.T) {
	door, key, _ := newDoor(t, activeGrants())
	claims := doorClaims()
	claims.Access = Read
	request := bearerRequest(signed(t, key, claims))
	actor, err := door.Verify(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if actor.Access != rastrillo.Read {
		t.Errorf("Access = %v, want Read", actor.Access)
	}
}

func TestDoorRefusesRevokedGrant(t *testing.T) {
	door, key, _ := newDoor(t, &fakeGrants{})
	request := bearerRequest(signed(t, key, doorClaims()))
	_, err := door.Verify(context.Background(), request)
	refusedAs(t, err, api.ErrUnauthorized, ErrRevoked)
	_, err = door.Recheck(context.Background(), request, rastrillo.Actor{})
	refusedAs(t, err, api.ErrUnauthorized, ErrRevoked)
}

func TestDoorAsksForTheTokensSubject(t *testing.T) {
	grants := activeGrants()
	door, key, _ := newDoor(t, grants)
	request := bearerRequest(signed(t, key, doorClaims()))
	if _, err := door.Verify(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	want := [][2]string{{"person_1", "g1"}}
	if !slices.Equal(grants.asked, want) {
		t.Errorf("GrantActive asked %v, want %v", grants.asked, want)
	}
}

func TestDoorChecksSignatureFirst(t *testing.T) {
	grants := activeGrants()
	door, _, _ := newDoor(t, grants)
	stranger, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	request := bearerRequest(signed(t, stranger, doorClaims()))
	_, err = door.Verify(context.Background(), request)
	refusedAs(t, err, api.ErrUnauthorized, ErrSignature)
	if len(grants.asked) != 0 {
		t.Errorf("GrantActive asked %v, want no call", grants.asked)
	}
}

// Well-formed origins that are not this door's: a malformed one fails
// as claims before the issuer and audience are compared.
func TestDoorRefusesAnotherIssuerOrAudience(t *testing.T) {
	grants := activeGrants()
	door, key, _ := newDoor(t, grants)
	issuer, audience := doorClaims(), doorClaims()
	issuer.Issuer = "https://other-root.test"
	audience.Audience = "https://other-site.test"
	for _, row := range []struct {
		claims Claims
		want   error
	}{{issuer, ErrIssuer}, {audience, ErrAudience}} {
		request := bearerRequest(signed(t, key, row.claims))
		_, err := door.Verify(context.Background(), request)
		refusedAs(t, err, api.ErrUnauthorized, row.want)
	}
	if len(grants.asked) != 0 {
		t.Errorf("GrantActive asked %v, want no call", grants.asked)
	}
}

func TestDoorUnavailable(t *testing.T) {
	grants := &fakeGrants{err: errors.New("roster unreadable")}
	door, key, _ := newDoor(t, grants)
	request := bearerRequest(signed(t, key, doorClaims()))
	_, err := door.Verify(context.Background(), request)
	refusedAs(t, err, api.ErrUnavailable)
	if errors.Is(err, api.ErrUnauthorized) {
		t.Errorf("err = %v, want no ErrUnauthorized", err)
	}
}

// A backend's error can carry what was submitted and can claim a
// classification of its own. The door answers 503 whatever it says.
func TestDoorUnavailableHidesTheBackendError(t *testing.T) {
	const leak = "backend saw person_1 and secret-text"
	for name, backend := range map[string]error{
		"submitted text": errors.New(leak),
		"unauthorized":   fmt.Errorf("%s: %w", leak, api.ErrUnauthorized),
		"conflict":       op.Conflict{Message: leak},
		"invalid": op.Invalid{
			Fields: map[string]string{"grant": leak}},
	} {
		door, key, _ := newDoor(t, &fakeGrants{err: backend})
		request := bearerRequest(signed(t, key, doorClaims()))
		check := map[string]func() error{
			"Verify": func() error {
				_, err := door.Verify(context.Background(), request)
				return err
			},
			"Recheck": func() error {
				_, err := door.Recheck(
					context.Background(), request, rastrillo.Actor{})
				return err
			},
		}
		for method, call := range check {
			err := call()
			status, body := api.Refusal(request, err)
			text := fmt.Sprintf("%v %+v", err, body)
			if status != http.StatusServiceUnavailable ||
				strings.Contains(text, "secret-text") ||
				errors.Is(err, api.ErrUnauthorized) ||
				errors.Is(err, backend) {
				t.Errorf("%s/%s: status %d, err %v, body %+v; "+
					"want 503 and no backend text",
					name, method, status, err, body)
			}
		}
	}
}

func TestDoorReadsOnlyTheAuthorizationHeader(t *testing.T) {
	door, key, _ := newDoor(t, activeGrants())
	token := signed(t, key, doorClaims())
	plain := func() *http.Request {
		return httptest.NewRequest(http.MethodGet, siteOrigin+"/api/x", nil)
	}
	cookie, twice, basic := plain(), bearerRequest(token), plain()
	query := httptest.NewRequest(
		http.MethodGet, siteOrigin+"/api/x?access_token="+token, nil)
	cookie.AddCookie(&http.Cookie{Name: "access_token", Value: token})
	twice.Header.Add("Authorization", "Bearer "+token)
	basic.Header.Set("Authorization", "Basic "+token)
	for name, request := range map[string]*http.Request{
		"cookie": cookie, "query": query, "two headers": twice,
		"Basic": basic,
	} {
		_, err := door.Verify(context.Background(), request)
		if !errors.Is(err, api.ErrUnauthorized) {
			t.Errorf("%s: err = %v, want ErrUnauthorized", name, err)
		}
	}
	lower := plain()
	lower.Header.Set("Authorization", "bearer "+token)
	if _, err := door.Verify(context.Background(), lower); err != nil {
		t.Errorf("lowercase bearer: err = %v, want nil", err)
	}
}

func TestDoorExpiresWhileInUse(t *testing.T) {
	door, key, now := newDoor(t, activeGrants())
	claims := doorClaims()
	request := bearerRequest(signed(t, key, claims))
	was, err := door.Verify(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	*now = time.Unix(claims.ExpiresAt, 0)
	_, err = door.Recheck(context.Background(), request, was)
	refusedAs(t, err, api.ErrUnauthorized, ErrExpired)
}

func TestNewDoorRefusesMissingParts(t *testing.T) {
	key, verifier := setup(t)
	for name, config := range map[string]DoorConfig{
		"no Verifier": {Grants: activeGrants()},
		"no Grants":   {Verifier: verifier},
	} {
		door, err := NewDoor(config)
		if door != nil || !errors.Is(err, ErrConfig) {
			t.Errorf("%s: NewDoor = %v, %v; want nil, ErrConfig",
				name, door, err)
		}
	}
	door, err := NewDoor(DoorConfig{Verifier: verifier, Grants: activeGrants()})
	if err != nil {
		t.Fatal(err)
	}
	claims := doorClaims()
	claims.IssuedAt = time.Now().Unix()
	claims.ExpiresAt = claims.IssuedAt + ninetyDays
	request := bearerRequest(signed(t, key, claims))
	if _, err := door.Verify(context.Background(), request); err != nil {
		t.Errorf("a nil Now: err = %v, want the real clock to pass", err)
	}
}
