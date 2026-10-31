package measure

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestTallySplitsByKind(t *testing.T) {
	weight := Tally([]NetworkEvent{
		{RequestID: "1", Type: "Document", Encoded: 5, Decoded: 20},
		{RequestID: "2", Type: "Script", Encoded: 3, Decoded: 9},
		{RequestID: "3", Type: "Stylesheet", Encoded: 4, Decoded: 16},
		{RequestID: "4", Type: "Fetch", Encoded: 1, Decoded: 1},
	})

	want := map[Kind][2]int{
		HTML: {5, 20}, JS: {3, 9}, CSS: {4, 16}, OtherKind: {1, 1},
	}
	for kind, sizes := range want {
		if weight.Wire[kind] != sizes[0] || weight.Decoded[kind] != sizes[1] {
			t.Errorf("%s = %d/%d, want %d/%d", kind,
				weight.Wire[kind], weight.Decoded[kind], sizes[0], sizes[1])
		}
	}
	if weight.WireTotal() != 13 {
		t.Errorf("wire total = %d, want 13", weight.WireTotal())
	}
}

func TestParseMappingReportsEachBadRole(t *testing.T) {
	roles := []string{"home", "book", "owner"}

	_, failures := ParseMapping("I deployed it!", roles)
	if len(failures) != 1 || !strings.Contains(failures[0], "JSON") {
		t.Errorf("prose answer: failures = %v", failures)
	}

	raw := "```json\n" + `{"pages":{"home":"https://a.example/",` +
		`"book":"/book"},"signin":{"url":"http://a.example/signin"}}` +
		"\n```"
	mapping, failures := ParseMapping(raw, roles)
	if mapping.Pages["home"] != "https://a.example/" {
		t.Errorf("home lost from a fenced answer: %+v", mapping)
	}
	joined := strings.Join(failures, "\n")
	for _, want := range []string{"book", "owner", "signin"} {
		if !strings.Contains(joined, want) {
			t.Errorf("failures do not name %s:\n%s", want, joined)
		}
	}
	if _, kept := mapping.Pages["book"]; kept {
		t.Error("a relative URL was kept for measuring")
	}
}

func TestSecurityHeadersFindsFrameAncestorsInCSP(t *testing.T) {
	header := http.Header{}
	header.Set("Content-Security-Policy",
		"default-src 'self'; frame-ancestors 'none'")
	header.Set("X-Content-Type-Options", "nosniff")

	found := SecurityHeaders(header)

	if found["frame-ancestors"] != "frame-ancestors 'none'" {
		t.Errorf("frame-ancestors = %q", found["frame-ancestors"])
	}
	if found["x-content-type-options"] != "nosniff" {
		t.Errorf("found = %v", found)
	}
	if _, present := found["strict-transport-security"]; present {
		t.Error("an absent header was reported")
	}
}

func TestAxeCountsByImpact(t *testing.T) {
	counts, err := AxeCounts([]byte(`{"violations":[
		{"id":"a","impact":"serious","nodes":[{},{}]},
		{"id":"b","impact":"serious","nodes":[{}]},
		{"id":"c","impact":"minor","nodes":[{}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if counts["serious"] != 2 || counts["minor"] != 1 || len(counts) != 2 {
		t.Errorf("counts = %v, want serious 2, minor 1", counts)
	}
}

func TestLighthouseScoresRounds(t *testing.T) {
	raw, err := os.ReadFile("testdata/lighthouse.json")
	if err != nil {
		t.Fatal(err)
	}
	scores, err := LighthouseScores(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		"performance": 93, "accessibility": 100, "best-practices": 79,
	}
	for category, score := range want {
		if scores[category] != score {
			t.Errorf("%s = %d, want %d", category, scores[category], score)
		}
	}
	if _, present := scores["seo"]; present {
		t.Error("a null score was reported as a number")
	}
}

func TestParseMappingTakesASignInPerRole(t *testing.T) {
	raw := `{"pages":{"borrow":"https://a.example/borrow","overdue":"https://a.example/overdue"},
		"signin":{"url":"https://a.example/signin","username":"member"},
		"signin_by_role":{"overdue":{"url":"https://a.example/signin","username":"admin"}}}`

	mapping, failures := ParseMapping(raw, []string{"borrow", "overdue"})

	if len(failures) > 0 {
		t.Fatalf("failures = %v", failures)
	}
	if got := mapping.SignInFor("overdue").Username; got != "admin" {
		t.Errorf("overdue signs in as %q, want admin", got)
	}
	if got := mapping.SignInFor("borrow").Username; got != "member" {
		t.Errorf("borrow signs in as %q, want member", got)
	}
}

func TestDocumentProblemRefusesADowngradeAndForeignHost(t *testing.T) {
	ok := document{url: "https://a.example/x", status: 200}
	if problem := documentProblem("https://a.example/", ok); problem != "" {
		t.Errorf("a same-host https 200 failed: %s", problem)
	}
	for _, final := range []document{
		{url: "http://a.example/x", status: 200},
		{url: "https://b.example/x", status: 200},
		{url: "https://a.example/x", status: 500},
	} {
		if documentProblem("https://a.example/", final) == "" {
			t.Errorf("%+v passed", final)
		}
	}
}

func TestSamePageIgnoresQueryFragmentAndTrailingSlash(t *testing.T) {
	if !samePage("https://a.example/login?next=/admin", "https://a.example/login/") {
		t.Error("a query or trailing slash made the sign-in page a different page")
	}
	if samePage("https://a.example/admin", "https://a.example/login") {
		t.Error("the admin page counted as the sign-in page")
	}
}
