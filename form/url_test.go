package form

import (
	"errors"
	"net/url"
	"testing"
)

// "Very forgiving whether I type the protocol or not": every way a
// person writes one site's address lands on one stored form.
func TestNormaliseURLForgives(t *testing.T) {
	for in, want := range map[string]string{
		"brightwater.example":            "https://brightwater.example",
		"  Brightwater.Example  ":        "https://brightwater.example",
		"www.brightwater.example/":       "https://www.brightwater.example",
		"https://brightwater.example":    "https://brightwater.example",
		"HTTPS://BRIGHTWATER.EXAMPLE/":   "https://brightwater.example",
		"http://brightwater.example":     "http://brightwater.example",
		"brightwater.example/pricing":    "https://brightwater.example/pricing",
		"brightwater.example:8443/x?y=1": "https://brightwater.example:8443/x?y=1",
		"brightwater.example/#team":      "https://brightwater.example/#team",
		"brightwater.example/Pricing/":   "https://brightwater.example/Pricing/",
		"192.0.2.10":                     "https://192.0.2.10",
		// "://" later in the address is not a scheme.
		"brightwater.example/in?next=https://x.example": "https://brightwater.example/in?next=https://x.example",
		"":    "",
		"   ": "",
		// A host in the reader's own script stays readable. Go's
		// url.URL.String would percent-encode it into m%C3%BCnchen.
		"München.example/straße": "https://münchen.example/straße",
	} {
		got, err := NormaliseURL(in)
		if err != nil || got != want {
			t.Errorf("NormaliseURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

// The stored value becomes a link, so anything a browser would execute,
// or that is not a web address, is refused.
func TestNormaliseURLRefuses(t *testing.T) {
	for _, in := range []string{
		"javascript:alert(1)", "JavaScript:alert(1)", "javascript://%0aalert(1)",
		"mailto:ana@example.com", "data:text/html,x", "ftp://files.example",
		"not a domain", "brightwater.example/a b", "localhost", "brightwater",
		".example", "example.", "bright..water.example", "https://",
		"https://bright<water>.example", "https://[::1]/",
		"brightwater.example:http",
	} {
		got, err := NormaliseURL(in)
		if err == nil {
			t.Errorf("NormaliseURL(%q) accepted it as %q", in, got)
			continue
		}
		var fe *Error
		if !errors.As(err, &fe) || fe.Key != "rastrillo.ui.url_invalid" || fe.Msg != urlInvalidEN {
			t.Errorf("NormaliseURL(%q) = %v; want the url_invalid catalog error", in, err)
		}
	}
}

// Credentials get their own message: someone who pasted an address
// with a password in it did type a web address, and "enter a web
// address" would send them looking for the wrong mistake. They are
// refused at all because https://bank.example@evil.example is the
// classic way to make a link read as one site and go to another.
func TestNormaliseURLRefusesCredentials(t *testing.T) {
	for _, in := range []string{
		"https://user:pass@brightwater.example",
		"http://user@brightwater.example/x",
		"https://bank.example@evil.example",
	} {
		_, err := NormaliseURL(in)
		var fe *Error
		if !errors.As(err, &fe) || fe.Key != "rastrillo.ui.url_credentials" || fe.Msg != urlCredentialsEN {
			t.Errorf("NormaliseURL(%q) = %v; want the url_credentials catalog error", in, err)
		}
	}
	// "mailto:ana@example.com" parses, once https:// is in front of it,
	// as user "mailto" at example.com. Telling that person to take a
	// password out would be nonsense: they typed an email address.
	_, err := NormaliseURL("mailto:ana@example.com")
	var fe *Error
	if !errors.As(err, &fe) || fe.Key != "rastrillo.ui.url_invalid" {
		t.Errorf("mailto: reported %v; want url_invalid, not the credentials message", err)
	}
}

func TestDisplayURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://brightwater.example/pricing/": "brightwater.example/pricing",
		"https://brightwater.example":          "brightwater.example",
		"http://brightwater.example/":          "brightwater.example",
		"HTTPS://Brightwater.example":          "Brightwater.example",
		"https://münchen.example/straße":       "münchen.example/straße",
		"brightwater.example":                  "brightwater.example",
		"":                                     "",
	} {
		if got := DisplayURL(in); got != want {
			t.Errorf("DisplayURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSafeHref(t *testing.T) {
	for in, want := range map[string]string{
		"https://brightwater.example":       "https://brightwater.example",
		"http://brightwater.example/x?y=1":  "http://brightwater.example/x?y=1",
		" https://brightwater.example ":     "https://brightwater.example",
		"javascript:alert(1)":               "",
		"JAVASCRIPT:alert(1)":               "",
		"mailto:ana@example.com":            "",
		"data:text/html,x":                  "",
		"//evil.example":                    "",
		"/relative/path":                    "",
		"brightwater.example":               "",
		"https://":                          "",
		"https://bank.example@evil.example": "",
		"":                                  "",
	} {
		if got := SafeHref(in); got != want {
			t.Errorf("SafeHref(%q) = %q, want %q", in, got, want)
		}
	}
}

// One site typed three ways is one key; two pages of it are two.
func TestURLKeyMatchesAcrossSpellings(t *testing.T) {
	a := URLKey("brightwater.example")
	for _, other := range []string{
		"https://www.Brightwater.example/", "http://brightwater.example",
		"WWW.BRIGHTWATER.EXAMPLE",
	} {
		if b := URLKey(other); a != b {
			t.Errorf("URLKey(%q) = %q, but URLKey(brightwater.example) = %q", other, b, a)
		}
	}
	for _, different := range []string{
		"brightwater.example/other-page", "brightwater.example:8443",
		"brightwater.test",
	} {
		if URLKey(different) == a {
			t.Errorf("URLKey(%q) collides with brightwater.example", different)
		}
	}
	if got := URLKey("  Not A URL  "); got != "not a url" {
		t.Errorf("URLKey on a value that does not normalise = %q; want it lowercased and trimmed", got)
	}
}

func TestParseURL(t *testing.T) {
	p := parseForm(t, url.Values{"Site": {"  WWW.Brightwater.example/ "}},
		Field{Name: "Site", Kind: URL, Required: true})
	if !p.OK() {
		t.Fatalf("a forgivable address was refused: %v", p.Errors())
	}
	if got := p.String("Site"); got != "https://www.brightwater.example" {
		t.Errorf("String(Site) = %q, want the normalised address", got)
	}
	// The echo is what was typed, so a re-render over some other
	// field's error does not rewrite this one under the person's eyes.
	if got := p.Echo()["Site"]; got != "  WWW.Brightwater.example/ " {
		t.Errorf("Echo(Site) = %q, want the raw text", got)
	}

	p = parseForm(t, url.Values{}, Field{Name: "Site", Kind: URL})
	if !p.OK() || p.String("Site") != "" {
		t.Errorf("empty optional URL = %q, %v; want \"\" and no error", p.String("Site"), p.Errors())
	}

	p = parseForm(t, url.Values{"Site": {"  "}}, Field{Name: "Site", Kind: URL, Required: true})
	if got := p.Errors()["Site"]; got != "rastrillo.ui.field_required" {
		t.Errorf("blank required URL error = %q, want the field_required key", got)
	}

	p = parseForm(t, url.Values{"Site": {"javascript:alert(1)"}}, Field{Name: "Site", Kind: URL, Required: true})
	if got := p.Errors()["Site"]; got != "rastrillo.ui.url_invalid" {
		t.Errorf("script URL error = %q, want the url_invalid key", got)
	}
	if got := p.String("Site"); got != "" {
		t.Errorf("a refused URL still reads back as %q; it must never reach storage", got)
	}
	if got := p.Echo()["Site"]; got != "javascript:alert(1)" {
		t.Errorf("refused URL echo = %q, want it as typed", got)
	}

	p = parseForm(t, url.Values{"Site": {"https://me:pw@brightwater.example"}}, Field{Name: "Site", Kind: URL})
	if got := p.Errors()["Site"]; got != "rastrillo.ui.url_credentials" {
		t.Errorf("credentialed URL error = %q, want the url_credentials key", got)
	}
}
