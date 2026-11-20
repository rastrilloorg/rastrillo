package provisiontest

import (
	"fmt"
	"net/http"
	"testing"

	"amadan.net/rastrillo/rastrillo/carlos"
)

func createAcme(t *testing.T, platform *Platform, token string) {
	t.Helper()
	body := `{"host":"acme.` + tenantDom + `"}`
	call(t, platform, "POST", "https://console.test"+createPath, token, body)
}

func TestMakeReady(t *testing.T) {
	platform := New()
	platform.Enable("bdf", "tenant", tenantDom)
	platform.Serve("bdf", "tenant", Hub())
	token := platform.Credential("bdf", "tenant", "instance")
	platform.Unready(1000)
	createAcme(t, platform, token)

	url := "https://acme." + tenantDom + "/api/version"
	resp, _ := call(t, platform, "GET", url, "", "")
	if resp.StatusCode != 503 {
		t.Fatalf("before MakeReady: %d", resp.StatusCode)
	}
	platform.MakeReady("acme." + tenantDom)
	resp, _ = call(t, platform, "GET", url, "", "")
	if resp.StatusCode != 200 {
		t.Fatalf("after MakeReady: %d", resp.StatusCode)
	}
}

func TestCatchAllServesLabel(t *testing.T) {
	platform := New()
	platform.Enable("bdf", "tenant", tenantDom)
	platform.Serve("bdf", "tenant", Hub())
	token := platform.Credential("bdf", "tenant", "instance")
	createAcme(t, platform, token)
	platform.CatchAll("bdf", "tenant", "example.test")

	url := "https://acme.example.test/api/version"
	resp, _ := call(t, platform, "GET", url, "", "")
	if resp.StatusCode != 200 ||
		resp.Header.Get(carlos.HostHeader) != "acme."+tenantDom {
		t.Fatalf("got %d, host %q", resp.StatusCode,
			resp.Header.Get(carlos.HostHeader))
	}
	url = "https://nobody.example.test/api/version"
	resp, _ = call(t, platform, "GET", url, "", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a label with no instance gave %d", resp.StatusCode)
	}
}

func TestFailAnswersBadGateway(t *testing.T) {
	platform := New()
	platform.Enable("bdf", "tenant", tenantDom)
	platform.Serve("bdf", "tenant", Hub())
	token := platform.Credential("bdf", "tenant", "instance")
	createAcme(t, platform, token)
	platform.Fail("acme." + tenantDom)
	url := "https://acme." + tenantDom + "/api/version"
	resp, _ := call(t, platform, "GET", url, "", "")
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestExactClaimBeatsCatchAllAndInstanceLabel(t *testing.T) {
	platform := New()
	platform.Enable("bdf", "tenant", tenantDom)
	platform.Serve("bdf", "tenant", Hub())
	token := platform.Credential("bdf", "tenant", "instance")
	call(t, platform, "POST", "https://console.test"+createPath, token,
		`{"host":"www.`+tenantDom+`"}`)
	platform.CatchAll("bdf", "tenant", "example.test")
	platform.Serve("bdf", "plaza", Hub())
	platform.Attach("bdf", "plaza", "www.example.test")

	resp, _ := call(t, platform, "GET",
		"https://www.example.test/api/version", "", "")
	if got := resp.Header.Get(carlos.HostHeader); got != "www.example.test" {
		t.Fatalf("the exact claim should answer, got host %q", got)
	}
	resp, _ = call(t, platform, "GET",
		"https://other.example.test/api/version", "", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a label with no instance gave %d", resp.StatusCode)
	}
}

func TestListCapsRecordsBeforeFiltering(t *testing.T) {
	platform := New()
	platform.Enable("bdf", "tenant", tenantDom)
	token := platform.Credential("bdf", "tenant", "instance")
	for number := 0; number <= instanceListMax; number++ {
		host := fmt.Sprintf("i%03d.%s", number, tenantDom)
		create(t, platform, token, host)
	}
	listed := func(query string) map[string]any {
		_, body := call(t, platform, "GET",
			platform.Console()+createPath+query, token, "")
		return decode(t, body)
	}
	all := listed("")
	if all["truncated"] != true || all["total"] != float64(instanceListMax) {
		t.Fatalf("501 instances: truncated %v, total %v",
			all["truncated"], all["total"])
	}
	beyond := listed("?q=i500")
	if beyond["total"] != float64(0) || beyond["truncated"] != true {
		t.Fatalf("an instance past the cap was found: %v", beyond)
	}
}
