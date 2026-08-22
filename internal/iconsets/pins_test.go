//go:build pins

// The answer to "who notices when the pinned icon assets go stale".
//
// Nobody re-pins these automatically, and a stale pin is invisible: the
// app keeps working, and the only symptom is that an icon added upstream
// after the pinned release cannot be used, with no error saying why. The
// opposite mistake is worse — bump a version without recomputing its
// hash and SRI rejects the asset at load time, which renders as a page
// with no icons rather than as an error.
//
// So this is a deliberate, networked check rather than part of the
// ordinary suite:
//
//	go test -tags pins ./internal/iconsets/
//
// Build-tagged, so `go test ./...` and CI never depend on jsdelivr or the
// npm registry being up — a check that fails when someone else's CDN has
// a bad afternoon teaches people to ignore it.
//
// Run it at release time. It reports two different things, and they are
// not equally urgent:
//
//   - INTEGRITY MISMATCH is serious and immediate: the bytes at a pinned
//     URL changed under a version that is supposed to be immutable.
//     Investigate before shipping; do not simply update the hash.
//   - "newer version available" is informational. Bumping is optional,
//     and when you do, change the version and its hash together.
package iconsets

import (
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func fetch(t *testing.T, rawURL string) []byte {
	t.Helper()
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %s", rawURL, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", rawURL, err)
	}
	return body
}

func sri(b []byte) string {
	sum := sha512.Sum384(b)
	return "sha384-" + base64.StdEncoding.EncodeToString(sum[:])
}

// Every pinned URL must still hash to the integrity value shipped beside
// it. A mismatch means the bytes changed under an immutable version —
// which should never happen, and matters more than being out of date.
func TestPinnedAssetsStillMatchTheirIntegrity(t *testing.T) {
	for _, name := range Names() {
		s := sets[name]
		for _, a := range []struct{ kind, url, want string }{
			{"cdn", s.cdnHref, s.cdnIntegrity},
			{"js", s.jsSrc, s.jsIntegrity},
		} {
			if a.url == "" {
				continue
			}
			got := sri(fetch(t, a.url))
			if got != a.want {
				t.Errorf("INTEGRITY MISMATCH for %s/%s\n  url:  %s\n  want: %s\n  got:  %s\n"+
					"the bytes at a pinned version changed; investigate before updating the hash",
					name, a.kind, a.url, a.want, got)
				continue
			}
			t.Logf("%-13s %-4s integrity ok", name, a.kind)
		}
	}
}

// Informational: is a newer release out? Never a failure on its own —
// being behind is a choice, and this only exists so the choice is made
// knowingly rather than by forgetting.
func TestReportNewerUpstreamVersions(t *testing.T) {
	pinned := map[string]string{}
	for _, name := range Names() {
		s := sets[name]
		for _, u := range []string{s.cdnHref, s.jsSrc} {
			if pkg, ver := pkgVersion(u); pkg != "" {
				pinned[pkg] = ver
			}
		}
	}
	if len(pinned) == 0 {
		t.Fatal("no pinned packages parsed out of the set URLs; the URL shape must have changed")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	for pkg, ver := range pinned {
		resp, err := client.Get("https://registry.npmjs.org/" + url.PathEscape(pkg))
		if err != nil {
			t.Fatalf("registry lookup for %s: %v", pkg, err)
		}
		var meta struct {
			DistTags map[string]string `json:"dist-tags"`
		}
		err = json.NewDecoder(resp.Body).Decode(&meta)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("decode registry response for %s: %v", pkg, err)
		}
		latest := meta.DistTags["latest"]
		switch {
		case latest == "":
			t.Errorf("%s: registry reported no latest version", pkg)
		case latest == ver:
			t.Logf("%-38s %-10s current", pkg, ver)
		default:
			t.Logf("%-38s %-10s newer available: %s (bump the version AND its hash together)", pkg, ver, latest)
		}
	}
}

// pkgVersion pulls the npm package and version out of a jsdelivr URL:
// https://cdn.jsdelivr.net/npm/<pkg>@<version>/<path>. Scoped packages
// keep their leading @.
func pkgVersion(raw string) (pkg, version string) {
	const prefix = "https://cdn.jsdelivr.net/npm/"
	if !strings.HasPrefix(raw, prefix) {
		return "", ""
	}
	rest := strings.TrimPrefix(raw, prefix)
	scoped := strings.HasPrefix(rest, "@")
	if scoped {
		rest = rest[1:]
	}
	at := strings.Index(rest, "@")
	if at < 0 {
		return "", ""
	}
	pkg, rest = rest[:at], rest[at+1:]
	if scoped {
		pkg = "@" + pkg
	}
	if slash := strings.Index(rest, "/"); slash >= 0 {
		rest = rest[:slash]
	}
	return pkg, rest
}
