package op

import (
	"context"
	"errors"
	"testing"

	rastrillo "amadan.net/rastrillo/rastrillo"
)

func TestCheck(t *testing.T) {
	scoped := rastrillo.Operation{ID: "b.create", Resource: "b", Scoped: true, Access: rastrillo.Write}
	unscoped := rastrillo.Operation{ID: "t.create", Resource: "t", Access: rastrillo.Write}
	session := rastrillo.Actor{Subject: "a", Access: rastrillo.Write}
	token := rastrillo.Actor{Subject: "a", Credential: "x", Access: rastrillo.Write}
	dbDown := errors.New("db down")
	allow := func(context.Context, rastrillo.Actor, rastrillo.Operation) error { return nil }
	refuse := func(context.Context, rastrillo.Actor, rastrillo.Operation) error { return Forbidden{Reason: "role"} }
	hide := func(context.Context, rastrillo.Actor, rastrillo.Operation) error { return NotFound{} }
	fail := func(context.Context, rastrillo.Actor, rastrillo.Operation) error { return dbDown }
	type pol = func(context.Context, rastrillo.Actor, rastrillo.Operation) error
	cases := []struct {
		name string
		o    rastrillo.Operation
		a    rastrillo.Actor
		p    pol
		want any // nil, Forbidden{}, NotFound{}, or dbDown
	}{
		{"scoped session", scoped, session, nil, nil},
		{"scoped token", scoped, token, nil, nil},
		{"scoped no subject", scoped, rastrillo.Actor{}, nil, Forbidden{}},
		{"unscoped session", unscoped, session, nil, nil},
		{"unscoped app", unscoped, rastrillo.Actor{}, nil, nil},
		{"unscoped token", unscoped, token, nil, Forbidden{}},
		{"policy allows token", unscoped, token, allow, nil},
		{"policy refuses", scoped, session, refuse, Forbidden{}},
		{"policy hides", scoped, session, hide, NotFound{}},
		{"policy fails", scoped, session, fail, dbDown},
		{"no subject beats policy", scoped, rastrillo.Actor{}, allow, Forbidden{}},
		{"read-only token on a write", scoped, rastrillo.Actor{Subject: "a", Credential: "x", Access: rastrillo.Read}, nil, Forbidden{}},
		{"read-only beats an allowing policy", scoped, rastrillo.Actor{Subject: "a", Credential: "x", Access: rastrillo.Read}, allow, Forbidden{}},
		{"read-only token on a read", rastrillo.Operation{ID: "b.get", Resource: "b", Scoped: true, Access: rastrillo.Read}, rastrillo.Actor{Subject: "a", Credential: "x", Access: rastrillo.Read}, nil, nil},
		{"unscoped anonymous page", unscoped, rastrillo.Actor{}, nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rc := &rastrillo.Ctx{Actor: c.a, Authorize: c.p}
			err := Check(context.Background(), rc, c.o)
			switch w := c.want.(type) {
			case nil:
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
			case Forbidden:
				var f Forbidden
				if !errors.As(err, &f) {
					t.Fatalf("err = %v, want Forbidden", err)
				}
			case NotFound:
				var n NotFound
				if !errors.As(err, &n) {
					t.Fatalf("err = %v, want NotFound", err)
				}
			case error:
				if !errors.Is(err, w) {
					t.Fatalf("err = %v, want %v", err, w)
				}
			}
		})
	}
}

// DefaultPolicy is exported so an app can name it in its own Authorize
// and add rules around it. Called directly, it must give the same
// answers Check gives with no policy set, or an app that wraps it
// would quietly let a token through to an unscoped table.
func TestDefaultPolicy(t *testing.T) {
	unscoped := rastrillo.Operation{ID: "t.create", Resource: "t", Access: rastrillo.Write}
	scoped := rastrillo.Operation{ID: "b.create", Resource: "b", Scoped: true, Access: rastrillo.Write}
	token := rastrillo.Actor{Subject: "a", Credential: "x", Access: rastrillo.Write}
	var f Forbidden
	err := DefaultPolicy(context.Background(), token, unscoped)
	if !errors.As(err, &f) {
		t.Fatalf("a token on an unscoped operation: err = %v, want Forbidden", err)
	}
	if want := "unscoped operation t.create has no Authorize policy for tokens"; f.Reason != want {
		t.Errorf("Reason = %q, want %q", f.Reason, want)
	}
	if err := DefaultPolicy(context.Background(), token, scoped); err != nil {
		t.Errorf("a token on a scoped operation: err = %v, want nil", err)
	}
	if err := DefaultPolicy(context.Background(), rastrillo.Actor{Subject: "a", Access: rastrillo.Write}, unscoped); err != nil {
		t.Errorf("a session on an unscoped operation: err = %v, want nil", err)
	}
}
