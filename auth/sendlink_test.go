package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// SendLink sends the same link Begin would: it lands, it redeems, and
// the session it makes is the ordinary one.
func TestSendLinkSendsALinkThatRedeems(t *testing.T) {
	a, m := newTestAuth(t, nil)
	if err := a.SendLink(context.Background(), "person@example.com", "relay"); err != nil {
		t.Fatalf("SendLink: %v", err)
	}
	if m.sentTo() != "person@example.com" {
		t.Fatalf("mail went to %q", m.sentTo())
	}
	link := linkRE.FindString(m.sentBody())
	if link == "" {
		t.Fatalf("no verify link in mail body:\n%s", m.sentBody())
	}
	w := httptest.NewRecorder()
	a.Verify(w, redeemRequest(t, link))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("Verify: %d → %q, want 303 → /", w.Code, w.Header().Get("Location"))
	}
	var session bool
	for _, c := range w.Result().Cookies() {
		if c.Name == a.SessionCookie() && c.Value != "" {
			session = true
		}
	}
	if !session {
		t.Fatal("redeeming the link set no session cookie")
	}
}

// The address budget is the form's, and shared with it: what SendLink
// spends, a post to Begin no longer has, and the other way round. The
// sixth in the window is refused as ErrRateLimited, with nothing sent.
func TestSendLinkSharesTheAddressBudgetWithBegin(t *testing.T) {
	a, m := newTestAuth(t, nil)
	for i := 0; i < 3; i++ {
		if err := a.SendLink(context.Background(), "person@example.com", "relay"); err != nil {
			t.Fatalf("send %d: %v", i+1, err)
		}
	}
	for i := 0; i < 2; i++ {
		if w := beginSignin(t, a, "person@example.com"); w.Header().Get("Location") != "/signin?sent=1" {
			t.Fatalf("Begin %d after three sends → %q, want sent", i+1, w.Header().Get("Location"))
		}
	}
	m.forget()
	if err := a.SendLink(context.Background(), "person@example.com", "relay"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("sixth link in the window: err = %v, want ErrRateLimited", err)
	}
	if m.sentTo() != "" {
		t.Error("a refused SendLink still sent mail")
	}
	if w := beginSignin(t, a, "person@example.com"); !strings.HasPrefix(w.Header().Get("Location"), "/signin?err=rate") {
		t.Errorf("Begin after the budget is spent → %q, want ?err=rate", w.Header().Get("Location"))
	}
}

// ipKey is the channel's name in the per-IP budget: twenty links in the
// window across any addresses, then the twenty-first is refused. That
// is what bounds a trusted caller — the budget is spent under the name
// it passes, not under any address it might claim.
func TestSendLinkSpendsTheIPBudgetUnderTheKey(t *testing.T) {
	a, m := newTestAuth(t, nil)
	for i := 0; i < 20; i++ {
		if err := a.SendLink(context.Background(), fmt.Sprintf("person%d@example.com", i), "relay"); err != nil {
			t.Fatalf("send %d: %v", i+1, err)
		}
	}
	m.forget()
	if err := a.SendLink(context.Background(), "person21@example.com", "relay"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("twenty-first link under one key: err = %v, want ErrRateLimited", err)
	}
	if m.sentTo() != "" {
		t.Error("a refused SendLink still sent mail")
	}
	// Another channel's name is another budget.
	if err := a.SendLink(context.Background(), "person21@example.com", "other"); err != nil {
		t.Errorf("the same address under another key: %v, want sent", err)
	}
}

// An address that is not one is refused as ErrBadAddress, and nothing
// goes out — not to the words typed, and not to anything made of them.
func TestSendLinkRefusesABadAddress(t *testing.T) {
	a, m := newTestAuth(t, nil)
	for _, bad := range []string{"", "person", "person@", "person@example.com\r\nBcc: x@example.com"} {
		if err := a.SendLink(context.Background(), bad, "relay"); !errors.Is(err, ErrBadAddress) {
			t.Errorf("SendLink(%q): err = %v, want ErrBadAddress", bad, err)
		}
		if m.sentTo() != "" {
			t.Errorf("SendLink(%q) sent mail to %q", bad, m.sentTo())
		}
	}
}
