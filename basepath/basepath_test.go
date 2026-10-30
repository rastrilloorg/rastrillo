package basepath_test

import (
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/provision"
)

func TestSlugFromHost(t *testing.T) {
	for host, want := range map[string]string{
		"acme-abc234.bdf.oncarlos.com":   "acme",
		"my-org-x7k2pq.bdf.oncarlos.com": "my-org",
		"a-222222.example.com":           "a",
	} {
		if got, err := basepath.SlugFromHost(host); err != nil || got != want {
			t.Errorf("SlugFromHost(%q) = %q, %v; want %q", host, got, err, want)
		}
	}
	for _, host := range []string{"", strings.Repeat("a", 26) + "-abc234.x", "acme.bdf.oncarlos.com", "acme-abc23.x", "acme-abc189.x",
		"acme-ABC234.x", "-abc234.x", "acme--abc234.x", "localhost"} {
		if got, err := basepath.SlugFromHost(host); err == nil {
			t.Errorf("SlugFromHost(%q) = %q, want an error", host, got)
		}
	}
}

// Plan 4 owns the label shape. If the two drift, an instance reads a slug
// other than the one its hub routes to it, and 404s at its own /<slug>.
func TestSlugFromHostRoundTripsPlan4sLabels(t *testing.T) {
	for _, s := range []string{"a", strings.Repeat("a", 24), strings.Repeat("a", provision.MaxSlug)} {
		label, err := provision.NewLabel(s)
		if err != nil {
			t.Fatalf("NewLabel(%q): %v", s, err)
		}
		if got, err := basepath.SlugFromHost(label + ".x"); err != nil || got != s {
			t.Errorf("SlugFromHost(%q) = %q, %v; want %q", label+".x", got, err, s)
		}
	}
}

func TestFromEnv(t *testing.T) {
	const origin = "https://example.com"
	cases := []struct {
		name string
		env  map[string]string
		want basepath.Config
		err  bool
	}{
		{"nothing set is off", nil, basepath.Config{}, false},
		{"an explicit slug", map[string]string{"RASTRILLO_SLUG": "acme"}, basepath.Config{Origin: origin, Slug: "acme"}, false},
		{"from the host label", map[string]string{"RASTRILLO_SLUG_FROM_HOST": "1", "CARLOS_HOST": "acme-abc234.bdf.oncarlos.com"}, basepath.Config{Origin: origin, Slug: "acme"}, false},
		{"from the host with no CARLOS_HOST", map[string]string{"RASTRILLO_SLUG_FROM_HOST": "1"}, basepath.Config{}, true},
		{"a host that does not fit the label shape", map[string]string{"RASTRILLO_SLUG_FROM_HOST": "1", "CARLOS_HOST": "www.bdf.oncarlos.com"}, basepath.Config{}, true},
		{"both set", map[string]string{"RASTRILLO_SLUG": "acme", "RASTRILLO_SLUG_FROM_HOST": "1"}, basepath.Config{}, true},
		{"a bad slug", map[string]string{"RASTRILLO_SLUG": "Acme/x"}, basepath.Config{}, true},
	}
	for _, c := range cases {
		got, err := basepath.FromEnv(origin, func(k string) string { return c.env[k] })
		if (err != nil) != c.err {
			t.Errorf("%s: FromEnv error = %v, want error %v", c.name, err, c.err)
			continue
		}
		if err == nil && got != c.want {
			t.Errorf("%s: FromEnv = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestValidate(t *testing.T) {
	for _, c := range []basepath.Config{
		{Origin: "https://example.com", Slug: "acme"},
		{Origin: "http://localhost:8080", Slug: "acme"},
		{Origin: "https://example.com", Slug: strings.Repeat("a", 56)},
	} {
		if err := c.Validate(); err != nil {
			t.Errorf("%+v.Validate() = %v, want nil", c, err)
		}
	}
	var bad []basepath.Config
	for _, o := range []string{"https://example.com/hub", "example.com", "https://example.com/", "https://example.com?x", "ftp://x"} {
		bad = append(bad, basepath.Config{Origin: o, Slug: "acme"})
	}
	for _, s := range []string{"", "Acme", "a/b", "-a", "a-", "_carlos", strings.Repeat("a", 57)} {
		bad = append(bad, basepath.Config{Origin: "https://example.com", Slug: s})
	}
	bad = append(bad, basepath.Config{Origin: "", Slug: "acme"})
	for _, c := range bad {
		err := c.Validate()
		if err == nil {
			t.Errorf("%+v.Validate() = nil, want an error", c)
			continue
		}
		if !strings.HasPrefix(err.Error(), "rastrillo/basepath: ") {
			t.Errorf("%+v.Validate() = %q, want it to start with rastrillo/basepath: ", c, err)
		}
	}
}
