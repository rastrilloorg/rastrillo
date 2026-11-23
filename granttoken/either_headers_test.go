package granttoken_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
	"amadan.net/rastrillo/rastrillo/granttoken"
	"amadan.net/rastrillo/rastrillo/handoff/handofftest"
)

// askedGrants says every grant is active and counts the questions.
type askedGrants struct{ asked atomic.Int32 }

func (grants *askedGrants) GrantActive(
	context.Context, string, string,
) (bool, error) {
	grants.asked.Add(1)
	return true, nil
}

func headerRequest(values ...string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, siteOrigin+things, nil)
	for _, value := range values {
		request.Header.Add("Authorization", value)
	}
	return request
}

// Either picks the door from the one header it understands, and the real
// apitoken store behind it refuses what Either does not recognise.
func TestEitherRoutesByTheAuthorizationHeader(t *testing.T) {
	root := handofftest.New(t)
	now := time.Now()
	token := root.GrantToken(t, granttoken.Claims{
		Audience: siteOrigin, Subject: "owneruid", Grant: "g1",
		Agent: "Claude", Access: granttoken.Read,
	})
	cases := []struct {
		name     string
		headers  []string
		wantDoor bool
	}{
		{"lowercase scheme", []string{"bearer " + token}, true},
		{"uppercase scheme", []string{"BEARER " + token}, true},
		{"extra spaces", []string{"Bearer   " + token}, true},
		{"two grant headers", []string{"Bearer " + token,
			"Bearer " + token}, false},
		{"grant then other", []string{"Bearer " + token,
			"Bearer site_k3Jx9QpL2mWa_secret"}, false},
		{"other then grant", []string{"Bearer site_k3Jx9QpL2mWa_secret",
			"Bearer " + token}, false},
	}
	for _, row := range cases {
		t.Run(row.name, func(t *testing.T) {
			grants := &askedGrants{}
			verify := newVerifier(t, root, schema.Open(t), grants,
				func() time.Time { return now })
			request := headerRequest(row.headers...)
			ctx := context.Background()
			was, err := verify.Verify(ctx, request)
			_, again := verify.Recheck(ctx, request, was)
			if row.wantDoor {
				if err != nil || was.Credential != "g1" || again != nil {
					t.Errorf("Verify = %+v, %v; Recheck err %v; "+
						"want the door's actor", was, err, again)
				}
				if got := grants.asked.Load(); got != 2 {
					t.Errorf("door asked %d times, want 2", got)
				}
				return
			}
			if was != (rastrillo.Actor{}) || !isUnauthorized(err) ||
				!isUnauthorized(again) {
				t.Errorf("Verify = %+v, %v; Recheck err %v; "+
					"want 401 from the fallback", was, err, again)
			}
			if got := grants.asked.Load(); got != 0 {
				t.Errorf("door asked %d times, want 0", got)
			}
		})
	}
}

func isUnauthorized(err error) bool {
	status, _ := api.Refusal(nil, err)
	return err != nil && status == http.StatusUnauthorized
}
