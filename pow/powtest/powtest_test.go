package powtest_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/pow"
	"amadan.net/rastrillo/rastrillo/pow/powtest"
)

func TestFillProducesAnAdmissibleSubmission(t *testing.T) {
	g, err := pow.New(pow.Config{InstanceKey: "k", Nonces: pow.MemoryNonces(), Difficulty: 8,
		MinAge: time.Millisecond, ScriptURL: "/p.js", WorkerURL: "/w.js"})
	if err != nil {
		t.Fatal(err)
	}
	f := g.Form(time.Now().Add(-time.Second), "s")
	page := []byte(`<form ` + string(f.Attrs()) + `>` + string(f.Fields()) + `</form>`)
	v := powtest.Fill(t, page, url.Values{"email": {"a@b.c"}})
	if v.Get("email") != "a@b.c" {
		t.Fatal("Fill dropped the caller's fields")
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if a := g.Check(r, pow.Want{Scope: "s"}); !a.OK {
		t.Fatalf("filled submission refused: %s %v", a.Reason, a.Also)
	}
	_ = context.Background()
}
