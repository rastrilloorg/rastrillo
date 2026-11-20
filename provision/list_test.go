package provision_test

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/provision"
	"amadan.net/rastrillo/rastrillo/provision/provisiontest"
)

func createHosts(t *testing.T, console *provision.Console, hosts ...string) {
	t.Helper()
	for _, host := range hosts {
		request := provision.CreateRequest{Host: host}
		_, err := console.Create(t.Context(), request)
		if err != nil {
			t.Fatalf("Create %s: %v", host, err)
		}
	}
}

func TestListReadsHealth(t *testing.T) {
	platform, console, domain := newTenant(t)
	platform.Unready(1)
	createHosts(t, console, "slow."+domain)
	platform.Unready(0)
	createHosts(t, console, "quick."+domain)

	page, err := console.List(t.Context(), provision.ListRequest{})
	if err != nil || len(page.Instances) != 2 {
		t.Fatalf("List = %+v, %v", page, err)
	}
	health := map[string]string{}
	for _, listed := range page.Instances {
		health[listed.Host] = listed.Health
	}
	slow, quick := "slow."+domain, "quick."+domain
	if len(health) != 2 || health[slow] != provision.HealthSettingUp ||
		health[quick] != provision.HealthRunning {
		t.Fatalf("health = %v", health)
	}
	platform.Sleep(quick)
	onlyQuick := provision.ListRequest{Query: "quick"}
	page, err = console.List(t.Context(), onlyQuick)
	if err != nil || len(page.Instances) != 1 ||
		page.Instances[0].Health != provision.HealthAsleep {
		t.Fatalf("after Sleep: %+v, %v", page, err)
	}
	platform.Fail(quick)
	page, _ = console.List(t.Context(), onlyQuick)
	if page.Instances[0].Health != provision.HealthFailed {
		t.Fatalf("after Fail: %q", page.Instances[0].Health)
	}
	for _, call := range platform.Calls() {
		if strings.HasPrefix(call, "GET https://") {
			t.Fatalf("List probed an instance: %s", call)
		}
	}
}

func TestListPages(t *testing.T) {
	_, console, domain := newTenant(t)
	createHosts(t, console, "a."+domain, "b."+domain, "c."+domain)

	first, err := console.List(t.Context(), provision.ListRequest{Limit: 2})
	if err != nil || len(first.Instances) != 2 || first.Next == "" ||
		first.Total != 3 {
		t.Fatalf("first page = %+v, %v", first, err)
	}
	second, err := console.List(t.Context(),
		provision.ListRequest{Limit: 2, Cursor: first.Next})
	if err != nil || len(second.Instances) != 1 || second.Next != "" {
		t.Fatalf("second page = %+v, %v", second, err)
	}
	if second.Instances[0].Host != "c."+domain {
		t.Fatalf("second page host = %q", second.Instances[0].Host)
	}
}

func TestListQuery(t *testing.T) {
	_, console, domain := newTenant(t)
	createHosts(t, console, "acme."+domain, "acme2."+domain, "other."+domain)

	page, err := console.List(t.Context(), provision.ListRequest{Query: "acme"})
	if err != nil || len(page.Instances) != 2 {
		t.Fatalf("page = %+v, %v", page, err)
	}
	for _, listed := range page.Instances {
		if !strings.Contains(listed.Host, "acme") {
			t.Errorf("host %q does not contain acme", listed.Host)
		}
	}
}

func TestListRetryable(t *testing.T) {
	platform, console, _ := newTenant(t)
	platform.ScriptList(
		provisiontest.Reply{Status: 429, RetryAfter: "7"},
		provisiontest.Reply{Status: 503},
	)
	_, err := console.List(t.Context(), provision.ListRequest{})
	var retry *provision.RetryError
	if !errors.As(err, &retry) || retry.After != 7*time.Second {
		t.Fatalf("429 gave %v", err)
	}
	_, err = console.List(t.Context(), provision.ListRequest{})
	if !errors.As(err, &retry) || retry.Status != 503 {
		t.Fatalf("503 gave %v", err)
	}
	// Never retried by itself: the budget is shared.
	var lists int
	for _, call := range platform.Calls() {
		if call == "GET /api/cli/apps/bdf/tenant/instances" {
			lists++
		}
	}
	if lists != 2 {
		t.Fatalf("two Lists made %d calls", lists)
	}
}

func TestListForbiddenForOtherRoles(t *testing.T) {
	platform := provisiontest.New()
	cfg := tenantConfig(platform)
	cfg.Token = platform.Credential("bdf", "tenant", "operate")
	console := newConsole(t, cfg)
	_, err := console.List(t.Context(), provision.ListRequest{})
	if !errors.Is(err, provision.ErrForbidden) {
		t.Fatalf("operate credential gave %v", err)
	}
}

func TestListRefusesALimitOutOfRange(t *testing.T) {
	_, console, _ := newTenant(t)
	for _, limit := range []int{-1, 500} {
		_, err := console.List(t.Context(), provision.ListRequest{Limit: limit})
		if !errors.Is(err, provision.ErrBadRequest) {
			t.Errorf("limit %d gave %v", limit, err)
		}
	}
}

func TestListReadsLargePage(t *testing.T) {
	_, console, domain := newTenant(t)
	for number := 0; number < 499; number++ {
		label := fmt.Sprintf("%s-%03d", strings.Repeat("x", 55), number)
		createHosts(t, console, label+"."+domain)
	}
	page, err := console.List(t.Context(), provision.ListRequest{Limit: 499})
	if err != nil || len(page.Instances) != 499 {
		t.Fatalf("got %d instances, %v", len(page.Instances), err)
	}
}

// paddedBody answers 200 with a list whose JSON is complete in its first
// bytes and is followed by spaces up to size, so a reader that stops at the
// limit still has a document that decodes.
type paddedBody struct{ size int }

func (padded paddedBody) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	document := `{"instances":[],"total":0}`
	body := document + strings.Repeat(" ", padded.size-len(document))
	return &http.Response{
		StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)),
		Header: http.Header{}, Request: request,
	}, nil
}

func TestListRefusesABodyOverFourMiB(t *testing.T) {
	platform := provisiontest.New()
	cfg := tenantConfig(platform)
	cfg.HTTP = &http.Client{Transport: paddedBody{size: 4<<20 + 1}}
	console := newConsole(t, cfg)
	page, err := console.List(t.Context(), provision.ListRequest{})
	if err == nil {
		t.Fatalf("a body over 4 MiB gave a page: %+v", page)
	}
	var retry *provision.RetryError
	if errors.As(err, &retry) || !strings.Contains(err.Error(), "4194304") {
		t.Fatalf("want a refusal naming the limit, got %v", err)
	}
}

func TestListAcceptsABodyOfFourMiB(t *testing.T) {
	platform := provisiontest.New()
	cfg := tenantConfig(platform)
	cfg.HTTP = &http.Client{Transport: paddedBody{size: 4 << 20}}
	console := newConsole(t, cfg)
	_, err := console.List(t.Context(), provision.ListRequest{})
	if err != nil {
		t.Fatalf("a body of exactly 4 MiB gave %v", err)
	}
}

// rateLimitedBrokenBody answers 429 with a Retry-After and a body that fails
// midway.
type rateLimitedBrokenBody struct{}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("connection reset")
}

func (rateLimitedBrokenBody) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	return &http.Response{
		StatusCode: 429, Body: io.NopCloser(failingReader{}),
		Header:  http.Header{"Retry-After": []string{"7"}},
		Request: request,
	}, nil
}

func TestListKeepsRetryAfterWhenTheBodyFails(t *testing.T) {
	platform := provisiontest.New()
	cfg := tenantConfig(platform)
	cfg.HTTP = &http.Client{Transport: rateLimitedBrokenBody{}}
	console := newConsole(t, cfg)
	_, err := console.List(t.Context(), provision.ListRequest{})
	var retry *provision.RetryError
	if !errors.As(err, &retry) || retry.After != 7*time.Second ||
		retry.Status != 429 || retry.Err == nil {
		t.Fatalf("got %+v from %v", retry, err)
	}
}

func TestListLeavesPublicHostEmptyWithoutANameHost(t *testing.T) {
	_, console, domain := newTenant(t)
	createHosts(t, console, "acme."+domain)
	page, err := console.List(t.Context(), provision.ListRequest{})
	if err != nil || len(page.Instances) != 1 {
		t.Fatalf("page = %+v, %v", page, err)
	}
	if page.Instances[0].PublicHost != "" {
		t.Fatalf("PublicHost = %q, want empty", page.Instances[0].PublicHost)
	}
}
