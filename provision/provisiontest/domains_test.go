package provisiontest

import (
	"encoding/json"
	"net/http"
	"testing"

	"amadan.net/rastrillo/rastrillo/carlos"
)

const (
	domainsPath = "/api/cli/apps/bdf/tenant/domains"
	customHost  = "blog.example.org"
)

// attachBody names acme, the instance createAcme makes, as the route.
const attachBody = `{"host":"` + customHost + `","route":"acme.` +
	tenantDom + `"}`

func domainsPlatform(t *testing.T) (*Platform, string) {
	t.Helper()
	platform := New()
	platform.Enable("bdf", "tenant", tenantDom)
	platform.Serve("bdf", "tenant", Hub())
	token := platform.Credential("bdf", "tenant", "instance")
	createAcme(t, platform, token)
	return platform, token
}

func servedHost(t *testing.T, platform *Platform, host string) string {
	t.Helper()
	response, _ := call(t, platform, "GET",
		"https://"+host+"/api/version", "", "")
	if response.StatusCode != http.StatusOK {
		return ""
	}
	return response.Header.Get(carlos.HostHeader)
}

func TestDomainDoorsTakeInstanceAndPublish(t *testing.T) {
	platform, _ := domainsPlatform(t)
	console := "https://console.test" + domainsPath
	for role, want := range map[string]int{
		"instance": 200, "publish": 200, "admin": 200, "deploy": 403,
	} {
		token := platform.Credential("bdf", "tenant", role)
		response, body := call(t, platform, "POST", console, token,
			attachBody)
		if response.StatusCode != want {
			t.Errorf("%s attach = %d %s, want %d", role,
				response.StatusCode, body, want)
		}
		response, body = call(t, platform, "GET",
			console+"/"+customHost, token, "")
		if response.StatusCode != want {
			t.Errorf("%s read = %d %s, want %d", role,
				response.StatusCode, body, want)
		}
	}
	token := platform.Credential("bdf", "tenant", "deploy")
	response, _ := call(t, platform, "DELETE", console+"/"+customHost,
		token, "")
	if response.StatusCode != http.StatusForbidden {
		t.Errorf("deploy detach = %d, want 403", response.StatusCode)
	}
}

func TestReattachMovesTheClaim(t *testing.T) {
	platform, token := domainsPlatform(t)
	body := `{"host":"beta.` + tenantDom + `"}`
	call(t, platform, "POST", "https://console.test"+createPath, token, body)
	console := "https://console.test" + domainsPath
	response, answer := call(t, platform, "POST", console, token, attachBody)
	if response.StatusCode != 200 {
		t.Fatalf("attach = %d %s", response.StatusCode, answer)
	}
	var first struct {
		Host, Route       string
		Created, Verified bool
	}
	if err := json.Unmarshal([]byte(answer), &first); err != nil {
		t.Fatalf("first attach answered %s: %v", answer, err)
	}
	if !first.Created || !first.Verified || first.Route != "acme."+tenantDom {
		t.Fatalf("first attach answered %s", answer)
	}
	if as := servedHost(t, platform, customHost); as != "acme."+tenantDom {
		t.Fatalf("served as %q", as)
	}
	moved := `{"host":"` + customHost + `","route":"beta.` + tenantDom + `"}`
	response, answer = call(t, platform, "POST", console, token, moved)
	var second struct{ Created bool }
	if err := json.Unmarshal([]byte(answer), &second); err != nil {
		t.Fatalf("re-attach answered %s: %v", answer, err)
	}
	if response.StatusCode != 200 || second.Created {
		t.Fatalf("re-attach = %d %s", response.StatusCode, answer)
	}
	if as := servedHost(t, platform, customHost); as != "beta."+tenantDom {
		t.Fatalf("after the move, served as %q", as)
	}
}

func TestEdgeServesAVerifiedDomainAsItsRoute(t *testing.T) {
	platform, token := domainsPlatform(t)
	platform.DomainProof(true)
	console := "https://console.test" + domainsPath
	call(t, platform, "POST", console, token, attachBody)
	if as := servedHost(t, platform, customHost); as != "" {
		t.Fatalf("an unproved domain is served as %q", as)
	}
	platform.ProveDomain(customHost)
	call(t, platform, "POST", console, token, attachBody)
	if as := servedHost(t, platform, customHost); as != "acme."+tenantDom {
		t.Fatalf("a proved domain is served as %q", as)
	}
	// A route that is not one of the app's instances serves nothing,
	// as a box materialises no claim naming a route it does not own.
	other := `{"host":"` + customHost + `","route":"nowhere.example.org"}`
	call(t, platform, "POST", console, token, other)
	if as := servedHost(t, platform, customHost); as != "" {
		t.Fatalf("a claim on a foreign route is served as %q", as)
	}
}
