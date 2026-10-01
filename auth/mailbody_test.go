package auth

import (
	"strings"
	"testing"
	"time"
)

// The email must tell the truth about the link it carries. keymaildev's
// default body says "expires shortly" while every sign-in page built on
// this package says 15 minutes; the default here names the real number.
func TestDefaultMailSaysTheLinkLifetime(t *testing.T) {
	if LinkTTL != 15*time.Minute {
		t.Fatalf("LinkTTL = %v; the default body below says 15 minutes", LinkTTL)
	}
	a, m := newTestAuth(t, nil)
	beginSignin(t, a, "person@example.com")
	body := m.sentBody()
	link := linkRE.FindString(body)
	if link == "" {
		t.Fatalf("no link in %q", body)
	}
	want := "Open this link to sign in:\n\n" + link + "\n\nIt works once and expires in 15 minutes. If you didn’t ask for it, you can ignore this email."
	if body != want {
		t.Fatalf("body\n%q\nwant\n%q", body, want)
	}
}

// An app writes its own email around the link: its name, its voice.
func TestConfigBodyWritesTheMail(t *testing.T) {
	a, m := newTestAuth(t, func(c *Config) {
		c.Body = func(link string) string { return "Here’s your link to sign in to Docs:\n\n" + link + "\n\nbye" }
	})
	beginSignin(t, a, "person@example.com")
	body := m.sentBody()
	if !strings.HasPrefix(body, "Here’s your link to sign in to Docs:\n\nhttp://app.test/auth/verify?token=") || !strings.HasSuffix(body, "\n\nbye") {
		t.Fatalf("Config.Body not used: %q", body)
	}
}
