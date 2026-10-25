package provisiontest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/carlos"
)

const (
	createPath = "/api/cli/apps/bdf/tenant/instances"
	tenantDom  = "bdf.oncarlos.test"
)

// call sends one request through the fake and returns the status, the
// response and its body. A JSON body gets the content type the platform
// insists on.
func call(t *testing.T, p *Platform, method, url, token, body string) (*http.Response, string) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return send(t, p, req)
}

func send(t *testing.T, p *Platform, req *http.Request) (*http.Response, string) {
	t.Helper()
	resp, err := p.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(b)
}

func decode(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("body %q is not a JSON object: %v", body, err)
	}
	return m
}

func create(t *testing.T, p *Platform, token, host string) (*http.Response, string) {
	t.Helper()
	return call(t, p, "POST", p.Console()+createPath, token, `{"host":"`+host+`"}`)
}

func TestFakeRefusesBadCredentials(t *testing.T) {
	p := New()
	p.Enable("bdf", "tenant", tenantDom)
	good := p.Credential("bdf", "tenant", "instance")
	publish := p.Credential("bdf", "tenant", "publish")
	otherApp := p.Credential("bdf", "other", "instance")
	otherAcct := p.Credential("zzz", "tenant", "instance")
	url := p.Console() + createPath
	body := `{"host":"a.` + tenantDom + `"}`

	resp, b := call(t, p, "POST", url, "", body)
	if resp.StatusCode != 401 || !strings.Contains(decode(t, b)["error"].(string), "not signed in") {
		t.Errorf("no bearer: %d %s, want 401 not signed in", resp.StatusCode, b)
	}
	if resp, b = call(t, p, "POST", url, "made-up", body); resp.StatusCode != 401 {
		t.Errorf("unknown bearer: %d %s, want 401", resp.StatusCode, b)
	}
	if resp, b = call(t, p, "POST", url, publish, body); resp.StatusCode != 403 {
		t.Errorf("publish credential at create: %d %s, want 403", resp.StatusCode, b)
	} else if decode(t, b)["error"] == nil {
		t.Errorf("403 body %q is not the JSON error", b)
	}
	for name, tok := range map[string]string{"another app": otherApp, "another account": otherAcct} {
		resp, b = call(t, p, "POST", url, tok, body)
		if resp.StatusCode != 404 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") {
			t.Errorf("credential for %s: %d %q %s, want 404 text/plain", name, resp.StatusCode, resp.Header.Get("Content-Type"), b)
		}
	}
	if resp, b = call(t, p, "POST", url, good, body); resp.StatusCode != 201 {
		t.Errorf("the right credential: %d %s, want 201", resp.StatusCode, b)
	}
}

func TestFakeCreateAnswers(t *testing.T) {
	p := New()
	tok := p.Credential("bdf", "tenant", "instance")
	host := "acme." + tenantDom

	resp, b := create(t, p, tok, host)
	if resp.StatusCode != 409 || resp.Header.Get("Retry-After") != "" {
		t.Errorf("not enabled: %d Retry-After %q %s, want 409 and no Retry-After", resp.StatusCode, resp.Header.Get("Retry-After"), b)
	}

	p.Enable("bdf", "tenant", tenantDom)
	req, _ := http.NewRequest("POST", p.Console()+createPath, strings.NewReader(`{"host":"`+host+`"}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	if resp, b = send(t, p, req); resp.StatusCode != 415 {
		t.Errorf("create without a content type: %d %s, want 415", resp.StatusCode, b)
	}
	for name, body := range map[string]string{
		"an uppercase host":   `{"host":"Acme.` + tenantDom + `"}`,
		"two labels":          `{"host":"a.b.` + tenantDom + `"}`,
		"another domain":      `{"host":"acme.other.test"}`,
		"a message with a nl": `{"host":"` + host + `","message":"a\nb"}`,
	} {
		if resp, b = call(t, p, "POST", p.Console()+createPath, tok, body); resp.StatusCode != 400 {
			t.Errorf("%s: %d %s, want 400", name, resp.StatusCode, b)
		}
	}

	resp, b = create(t, p, tok, host)
	if resp.StatusCode != 201 {
		t.Fatalf("create: %d %s, want 201", resp.StatusCode, b)
	}
	rec := decode(t, b)
	for k, want := range map[string]any{"host": host, "app": "tenant", "account": "bdf"} {
		if rec[k] != want {
			t.Errorf("record %s = %v, want %v", k, rec[k], want)
		}
	}
	for _, k := range []string{"channel", "created_at", "created_by"} {
		if s, _ := rec[k].(string); s == "" {
			t.Errorf("record has no %s: %s", k, b)
		}
	}

	resp, b = create(t, p, tok, host)
	if resp.StatusCode != 409 || !strings.Contains(decode(t, b)["error"].(string), "already exists") {
		t.Errorf("same host again: %d %s, want 409 already exists", resp.StatusCode, b)
	}

	resp, b = call(t, p, "GET", p.Console()+createPath+"/"+host, tok, "")
	if resp.StatusCode != 200 || !strings.Contains(b, `"gated":false`) || decode(t, b)["host"] != host {
		t.Errorf("get: %d %s, want 200 with gated false", resp.StatusCode, b)
	}
	if decode(t, b)["served_by"] == nil {
		t.Errorf("get: %s names no served_by", b)
	}
	resp, _ = call(t, p, "GET", p.Console()+createPath+"/nobody."+tenantDom, tok, "")
	if resp.StatusCode != 404 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") {
		t.Errorf("get of an unknown host: %d %q, want 404 text/plain", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
}

func TestFakeScriptRecordsThenFails(t *testing.T) {
	p := New()
	p.Enable("bdf", "tenant", tenantDom)
	tok := p.Credential("bdf", "tenant", "instance")
	host := "acme." + tenantDom
	p.Script(Reply{Status: 502, Record: true})

	resp, b := create(t, p, tok, host)
	if resp.StatusCode != 502 || decode(t, b)["error"] != "scripted 502" {
		t.Errorf("scripted create: %d %s, want 502 scripted 502", resp.StatusCode, b)
	}
	if got := p.Instances("bdf", "tenant"); len(got) != 1 || got[0] != host {
		t.Errorf("Instances = %v, want the host written before the 502", got)
	}
	// The script is spent: the same create now meets the real logic.
	if resp, b = create(t, p, tok, host); resp.StatusCode != 409 {
		t.Errorf("create after the script: %d %s, want 409", resp.StatusCode, b)
	}
}

func TestFakeScriptWithoutRecordWritesNothing(t *testing.T) {
	p := New()
	p.Enable("bdf", "tenant", tenantDom)
	tok := p.Credential("bdf", "tenant", "instance")
	p.Script(Reply{Status: 503, RetryAfter: "7"})

	resp, _ := create(t, p, tok, "acme."+tenantDom)
	if resp.StatusCode != 503 || resp.Header.Get("Retry-After") != "7" {
		t.Errorf("scripted create: %d Retry-After %q, want 503 and 7", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	if got := p.Instances("bdf", "tenant"); len(got) != 0 {
		t.Errorf("Instances = %v, want none", got)
	}
}

func TestFakeScripted202(t *testing.T) {
	p := New()
	p.Enable("bdf", "tenant", tenantDom)
	tok := p.Credential("bdf", "tenant", "instance")
	host := "acme." + tenantDom
	p.Script(Reply{Status: 202, Record: true})

	resp, b := create(t, p, tok, host)
	if resp.StatusCode != 202 || resp.Header.Get("Retry-After") != "" {
		t.Errorf("scripted 202: %d Retry-After %q, want 202 and none", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	m := decode(t, b)
	if m["phase"] != "creating" || m["host"] != host || m["operation_id"] != "op1" {
		t.Errorf("202 body = %s, want host, operation_id op1 and phase creating", b)
	}
	if got := p.Instances("bdf", "tenant"); len(got) != 1 {
		t.Errorf("Instances = %v, want the instance the 202 accepted", got)
	}
}

func TestFakeScriptHangsUntilTheClientGivesUp(t *testing.T) {
	p := New()
	p.Enable("bdf", "tenant", tenantDom)
	tok := p.Credential("bdf", "tenant", "instance")
	host := "acme." + tenantDom
	p.Script(Reply{Hang: true, Record: true})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", p.Console()+createPath, strings.NewReader(`{"host":"`+host+`"}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	_, err := p.Client().Do(req)
	if err == nil || ctx.Err() == nil {
		t.Fatalf("hung create returned %v, want the client's own deadline", err)
	}
	if got := p.Instances("bdf", "tenant"); len(got) != 1 {
		t.Errorf("Instances = %v, want the instance written before the timeout", got)
	}
}

func publish(t *testing.T, p *Platform, tok, host, body string) (*http.Response, string) {
	t.Helper()
	return call(t, p, "PUT", p.Console()+"/api/cli/apps/bdf/hub/domains/"+host+"/paths", tok, body)
}

func TestFakePathsAnswers(t *testing.T) {
	p := New()
	p.Attach("bdf", "hub", "product.test")
	tok := p.Credential("bdf", "hub", "publish")
	tenant := p.Credential("bdf", "tenant", "instance")
	target := "acme.bdf.oncarlos.test"

	if resp, b := publish(t, p, tok, "product.test", `{}`); resp.StatusCode != 400 {
		t.Errorf("no paths key: %d %s, want 400", resp.StatusCode, b)
	}
	for name, body := range map[string]string{
		"a slash in the prefix":  `{"paths":{"/a":"x.test"}}`,
		"a target with no dot":   `{"paths":{"a":"x"}}`,
		"an unnormalised target": `{"paths":{"a":"X.test"}}`,
		"too many":               tooMany(),
	} {
		if resp, b := publish(t, p, tok, "product.test", body); resp.StatusCode != 422 {
			t.Errorf("%s: %d %.80s, want 422", name, resp.StatusCode, b)
		}
	}
	if resp, b := publish(t, p, tok, "other.test", `{"paths":{}}`); resp.StatusCode != 404 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") {
		t.Errorf("a hostname the app does not hold: %d %s, want 404 text/plain", resp.StatusCode, b)
	}
	if resp, b := call(t, p, "PUT", p.Console()+"/api/cli/apps/bdf/hub/domains/product.test/paths", tenant, `{"paths":{}}`); resp.StatusCode != 404 {
		t.Errorf("another app's credential: %d %s, want 404", resp.StatusCode, b)
	}

	body := `{"paths":{"acme":"` + target + `"}}`
	resp, b := publish(t, p, tok, "product.test", body)
	m := decode(t, b)
	if resp.StatusCode != 200 || m["changed"] != true || m["host"] != "product.test" || m["prefixes"] != float64(1) {
		t.Errorf("publish: %d %s, want 200 changed true, 1 prefix", resp.StatusCode, b)
	}
	if got := p.Paths("product.test"); got["acme"] != target || len(got) != 1 {
		t.Errorf("Paths = %v", got)
	}
	if resp, b = publish(t, p, tok, "product.test", body); resp.StatusCode != 200 || decode(t, b)["changed"] != false {
		t.Errorf("same map again: %d %s, want changed false", resp.StatusCode, b)
	}
	if resp, b = publish(t, p, tok, "product.test", `{"paths":{}}`); decode(t, b)["changed"] != true || len(p.Paths("product.test")) != 0 {
		t.Errorf("clearing the map: %d %s, Paths %v, want changed true and none left", resp.StatusCode, b, p.Paths("product.test"))
	}
}

func tooMany() string {
	var sb strings.Builder
	sb.WriteString(`{"paths":{`)
	for i := 0; i <= 10000; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`"p`)
		sb.WriteString(strconv.Itoa(i))
		sb.WriteString(`":"x.test"`)
	}
	sb.WriteString(`}}`)
	return sb.String()
}

func TestFakeScriptPaths(t *testing.T) {
	p := New()
	p.Attach("bdf", "hub", "product.test")
	tok := p.Credential("bdf", "hub", "publish")
	p.ScriptPaths(Reply{Status: 429, RetryAfter: "3"}, Reply{Status: 502, Record: true})

	resp, _ := publish(t, p, tok, "product.test", `{"paths":{"a":"x.test"}}`)
	if resp.StatusCode != 429 || resp.Header.Get("Retry-After") != "3" || len(p.Paths("product.test")) != 0 {
		t.Errorf("first scripted publish: %d Retry-After %q Paths %v, want 429, 3, nothing written", resp.StatusCode, resp.Header.Get("Retry-After"), p.Paths("product.test"))
	}
	resp, _ = publish(t, p, tok, "product.test", `{"paths":{"a":"x.test"}}`)
	if resp.StatusCode != 502 || p.Paths("product.test")["a"] != "x.test" {
		t.Errorf("second scripted publish: %d Paths %v, want 502 with the map written", resp.StatusCode, p.Paths("product.test"))
	}
}

// edgeGet fetches https://host/path through the fake's edge.
func edgeGet(t *testing.T, p *Platform, host, path string) (*http.Response, string) {
	t.Helper()
	return call(t, p, "GET", "https://"+host+path, "", "")
}

func routedPlatform(t *testing.T, tenantAccount string) (p *Platform, host string) {
	t.Helper()
	p = New()
	p.Release("bdf", "hub", "v1")
	p.Release("bdf", "tenant", "v2")
	p.Release("zzz", "tenant", "v9")
	p.Serve("bdf", "hub", Hub())
	p.Serve("bdf", "tenant", Tenant("acme"))
	p.Serve("zzz", "tenant", Tenant("acme"))
	p.Enable("bdf", "tenant", tenantDom)
	p.Enable("zzz", "tenant", "zzz.oncarlos.test")
	p.Attach("bdf", "hub", "product.test")

	host = "acme." + tenantDom
	tok := p.Credential("bdf", "tenant", "instance")
	if tenantAccount == "zzz" {
		host = "acme.zzz.oncarlos.test"
		tok = p.Credential("zzz", "tenant", "instance")
	}
	url := p.Console() + "/api/cli/apps/" + tenantAccount + "/tenant/instances"
	if resp, b := call(t, p, "POST", url, tok, `{"host":"`+host+`"}`); resp.StatusCode != 201 {
		t.Fatalf("create: %d %s", resp.StatusCode, b)
	}
	hubTok := p.Credential("bdf", "hub", "publish")
	if resp, b := publish(t, p, hubTok, "product.test", `{"paths":{"acme":"`+host+`"}}`); resp.StatusCode != 200 {
		t.Fatalf("publish: %d %s", resp.StatusCode, b)
	}
	return p, host
}

func TestFakeEdgeRoutesAndFallsThrough(t *testing.T) {
	p, host := routedPlatform(t, "bdf")

	resp, b := edgeGet(t, p, "product.test", "/acme/api/version")
	if resp.StatusCode != 200 || b != "test" || resp.Header.Get("X-Carlos-Version") != "v2" || resp.Header.Get(carlos.HostHeader) != host {
		t.Errorf("a listed prefix: %d %q version %q host %q, want the tenant: 200 test v2 %s",
			resp.StatusCode, b, resp.Header.Get("X-Carlos-Version"), resp.Header.Get(carlos.HostHeader), host)
	}
	// The product's own pages and an unlisted prefix stay with the hub.
	for _, path := range []string{"/api/version", "/other/api/version"} {
		resp, _ = edgeGet(t, p, "product.test", path)
		if resp.Header.Get("X-Carlos-Version") != "v1" {
			t.Errorf("GET %s: version %q, want the hub's v1", path, resp.Header.Get("X-Carlos-Version"))
		}
	}
	resp, _ = edgeGet(t, p, host, "/api/version")
	if resp.StatusCode != 200 || resp.Header.Get("X-Carlos-Version") != "v2" || resp.Header.Get(carlos.HostHeader) != host {
		t.Errorf("the instance's own host: %d version %q, want 200 v2", resp.StatusCode, resp.Header.Get("X-Carlos-Version"))
	}

	p.OtherBox(host)
	resp, _ = edgeGet(t, p, "product.test", "/acme/api/version")
	if resp.StatusCode != 404 || resp.Header.Get("X-Carlos-Version") != "v1" || resp.Header.Get(carlos.HostHeader) != "" {
		t.Errorf("an instance on another box: %d version %q host %q, want the hub's 404 under v1",
			resp.StatusCode, resp.Header.Get("X-Carlos-Version"), resp.Header.Get(carlos.HostHeader))
	}
	// Its own host is backhauled to the home box, so it still answers.
	resp, _ = edgeGet(t, p, host, "/api/version")
	if resp.StatusCode != 200 || resp.Header.Get("X-Carlos-Version") != "v2" || resp.Header.Get(carlos.HostHeader) != host {
		t.Errorf("the host of an instance on another box: %d version %q host %q, want 200 v2 naming itself",
			resp.StatusCode, resp.Header.Get("X-Carlos-Version"), resp.Header.Get(carlos.HostHeader))
	}
}

func TestFakeEdgeIgnoresAnotherAccountsTarget(t *testing.T) {
	p, _ := routedPlatform(t, "zzz")

	resp, _ := edgeGet(t, p, "product.test", "/acme/api/version")
	if resp.StatusCode != 404 || resp.Header.Get("X-Carlos-Version") != "v1" || resp.Header.Get(carlos.HostHeader) != "" {
		t.Errorf("another account's instance: %d version %q host %q, want the hub's 404 under v1",
			resp.StatusCode, resp.Header.Get("X-Carlos-Version"), resp.Header.Get(carlos.HostHeader))
	}
}

func TestFakeUnknownHosts(t *testing.T) {
	p := New()
	p.Release("bdf", "tenant", "v2")
	p.Serve("bdf", "tenant", Tenant(""))
	p.Enable("bdf", "tenant", tenantDom)
	p.Unready(2)
	tok := p.Credential("bdf", "tenant", "instance")
	host := "acme." + tenantDom
	if resp, b := create(t, p, tok, host); resp.StatusCode != 201 {
		t.Fatalf("create: %d %s", resp.StatusCode, b)
	}

	for i := 1; i <= 2; i++ {
		resp, _ := edgeGet(t, p, host, "/api/version")
		if resp.StatusCode != 503 || resp.Header.Get("Retry-After") != "5" || resp.Header.Get("Cache-Control") != "no-store" ||
			resp.Header.Get("X-Carlos-Version") != "" || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
			t.Errorf("request %d to a new instance: %d %v, want the pending 503", i, resp.StatusCode, resp.Header)
		}
	}
	if resp, _ := edgeGet(t, p, host, "/api/version"); resp.StatusCode != 200 || resp.Header.Get("X-Carlos-Version") != "v2" {
		t.Errorf("third request: %d version %q, want 200 v2", resp.StatusCode, resp.Header.Get("X-Carlos-Version"))
	}

	// A host under the deployment's own domain that nothing created yet may
	// be one the edge has not heard of; anywhere else is simply unknown.
	resp, _ := edgeGet(t, p, "later."+tenantDom, "/")
	if resp.StatusCode != 503 || resp.Header.Get("Retry-After") != "5" || resp.Header.Get("X-Carlos-Version") != "" {
		t.Errorf("unknown host under %s: %d, want the pending 503", AppsDomain, resp.StatusCode)
	}
	resp, _ = edgeGet(t, p, "nowhere.example", "/")
	if resp.StatusCode != 404 || resp.Header.Get("X-Carlos-Version") != "" || resp.Header.Get("Retry-After") != "" {
		t.Errorf("unknown host elsewhere: %d, want 404", resp.StatusCode)
	}
}

func TestFakeCallsRecordEveryRequest(t *testing.T) {
	p := New()
	p.Enable("bdf", "tenant", tenantDom)
	tok := p.Credential("bdf", "tenant", "instance")
	create(t, p, tok, "acme."+tenantDom)
	edgeGet(t, p, "nowhere.example", "/x")
	want := []string{
		"POST /api/cli/apps/bdf/tenant/instances",
		"GET https://nowhere.example/x",
	}
	got := p.Calls()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Calls = %q, want %q", got, want)
	}
}

func TestTenantAndHubHandlers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		h      http.Handler
		method string
		path   string
		status int
	}{
		{"tenant version", Tenant("")("h.test"), "GET", "/api/version", 200},
		{"tenant slug version", Tenant("acme")("h.test"), "GET", "/acme/api/version", 200},
		{"tenant slug unset", Tenant("")("h.test"), "GET", "/acme/api/version", 404},
		{"tenant other slug", Tenant("acme")("h.test"), "GET", "/zed/api/version", 404},
		{"tenant elsewhere", Tenant("acme")("h.test"), "GET", "/", 404},
		{"tenant post", Tenant("acme")("h.test"), "POST", "/api/version", 404},
		{"hub version", Hub()("h.test"), "GET", "/api/version", 200},
		{"hub slugged", Hub()("h.test"), "GET", "/acme/api/version", 404},
	} {
		req, _ := http.NewRequest(tc.method, "https://h.test"+tc.path, nil)
		rec := httptest.NewRecorder()
		tc.h.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Errorf("%s: %d, want %d", tc.name, rec.Code, tc.status)
		}
		if tc.status == 200 && (rec.Body.String() != "test" || rec.Header().Get(carlos.HostHeader) != "h.test") {
			t.Errorf("%s: body %q host %q, want test and h.test", tc.name, rec.Body.String(), rec.Header().Get(carlos.HostHeader))
		}
		if tc.status == 404 && rec.Header().Get(carlos.HostHeader) != "" {
			t.Errorf("%s: a 404 names %s", tc.name, carlos.HostHeader)
		}
	}
}

// TestFakeEdgeMatchesTheCleanedSegmentFirst pins the edge's lookup order
// (edge.go:1346-1351): the cleaned first segment, then the raw one, the
// cleaned one winning when both match. The tenants echo the path they were
// sent, which the edge forwards unchanged.
func TestFakeEdgeMatchesTheCleanedSegmentFirst(t *testing.T) {
	p := New()
	p.Release("bdf", "hub", "v1")
	p.Release("bdf", "tenant", "v2")
	p.Serve("bdf", "hub", Hub())
	p.Serve("bdf", "tenant", func(host string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(carlos.HostHeader, host)
			io.WriteString(w, r.URL.Path)
		})
	})
	p.Enable("bdf", "tenant", tenantDom)
	p.Attach("bdf", "hub", "product.test")
	tok := p.Credential("bdf", "tenant", "instance")
	acme, bob := "acme."+tenantDom, "bob."+tenantDom
	for _, h := range []string{acme, bob} {
		if resp, b := create(t, p, tok, h); resp.StatusCode != 201 {
			t.Fatalf("create %s: %d %s", h, resp.StatusCode, b)
		}
	}
	hubTok := p.Credential("bdf", "hub", "publish")
	setMap := func(body string) {
		t.Helper()
		if resp, b := publish(t, p, hubTok, "product.test", body); resp.StatusCode != 200 {
			t.Fatalf("publish: %d %s", resp.StatusCode, b)
		}
	}

	setMap(`{"paths":{"acme":"` + acme + `"}}`)
	for _, tc := range []struct{ name, path, host string }{
		{"cleaned match (leading empty segment)", "//acme/api/version", acme},
		{"cleaned match (dot segment)", "/./acme/x", acme},
		{"raw-only match (the cleaned segment is not listed)", "/acme/../bob/x", acme},
		{"neither matches", "/bob/x", ""},
	} {
		resp, body := edgeGet(t, p, "product.test", tc.path)
		if got := resp.Header.Get(carlos.HostHeader); got != tc.host {
			t.Errorf("%s: GET %s served by %q, want %q", tc.name, tc.path, got, tc.host)
		}
		if tc.host != "" && body != tc.path {
			t.Errorf("%s: the tenant saw path %q, want %q unchanged", tc.name, body, tc.path)
		}
		if tc.host == "" && resp.Header.Get("X-Carlos-Version") != "v1" {
			t.Errorf("%s: version %q, want the hub's v1", tc.name, resp.Header.Get("X-Carlos-Version"))
		}
	}

	// Both forms are listed: the cleaned one, "bob", wins over the raw "acme".
	setMap(`{"paths":{"acme":"` + acme + `","bob":"` + bob + `"}}`)
	resp, body := edgeGet(t, p, "product.test", "/acme/../bob/x")
	if got := resp.Header.Get(carlos.HostHeader); got != bob || body != "/acme/../bob/x" {
		t.Errorf("both forms listed: served by %q path %q, want %q and the path unchanged", got, body, bob)
	}
}

// The platform builds every door's role set with svcrole.Allow, which adds
// admin to it (internal/svcrole/svcrole.go:101-122). A fake that refused
// admin would fail an app's tests for a credential the platform takes; one
// that took any role would pass a credential the platform refuses.
func TestFakeAdmitsAdminWhereThePlatformDoes(t *testing.T) {
	p := New()
	p.Enable("bdf", "tenant", tenantDom)
	p.Attach("bdf", "hub", "product.test")
	admin := p.Credential("bdf", "tenant", "admin")
	host := "acme." + tenantDom
	if resp, b := create(t, p, admin, host); resp.StatusCode != 201 {
		t.Errorf("admin at create: %d %s, want 201", resp.StatusCode, b)
	}
	if resp, b := call(t, p, "GET", p.Console()+createPath+"/"+host, admin, ""); resp.StatusCode != 200 {
		t.Errorf("admin at get: %d %s, want 200", resp.StatusCode, b)
	}
	hubAdmin := p.Credential("bdf", "hub", "admin")
	if resp, b := publish(t, p, hubAdmin, "product.test", `{"paths":{"acme":"`+host+`"}}`); resp.StatusCode != 200 {
		t.Errorf("admin at paths: %d %s, want 200", resp.StatusCode, b)
	}

	operate := p.Credential("bdf", "tenant", "operate")
	if resp, b := create(t, p, operate, "globex."+tenantDom); resp.StatusCode != 403 {
		t.Errorf("operate at create: %d %s, want 403: admin is the one role every door adds", resp.StatusCode, b)
	}
}
