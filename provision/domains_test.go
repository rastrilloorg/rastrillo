package provision_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/carlos"
	"amadan.net/rastrillo/rastrillo/provision"
	"amadan.net/rastrillo/rastrillo/provision/provisiontest"
)

const customHost = "blog.example.org"

// servedTenant is newTenant with one instance, acme, that the edge
// serves.
func servedTenant(
	t *testing.T,
) (*provisiontest.Platform, *provision.Console, string) {
	t.Helper()
	platform, console, domain := newTenant(t)
	platform.Serve("bdf", "tenant", provisiontest.Hub())
	acme := "acme." + domain
	createHosts(t, console, acme)
	return platform, console, acme
}

// servedAs is the instance host the edge serves host's /api/version as,
// or "" when the edge does not serve it.
func servedAs(
	t *testing.T, platform *provisiontest.Platform, host string,
) string {
	t.Helper()
	response, err := platform.Client().Get("https://" + host + "/api/version")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	io.Copy(io.Discard, response.Body)
	if response.StatusCode != http.StatusOK {
		return ""
	}
	return response.Header.Get(carlos.HostHeader)
}

func TestAttachDomainWithProof(t *testing.T) {
	platform, console, acme := servedTenant(t)
	platform.DomainProof(true)

	attached, err := console.AttachDomain(t.Context(), customHost, acme)
	if err != nil || attached.Host != customHost || attached.Verified {
		t.Fatalf("first attach = %+v, %v", attached, err)
	}
	verify := attached.Verify
	if verify == nil || verify.Type != "TXT" ||
		verify.Name != "_carlos-challenge."+customHost ||
		!strings.HasPrefix(verify.Value, "carlos-verify=") ||
		verify.TTL != 300 || verify.Set != "verify" || !verify.Required {
		t.Fatalf("challenge = %+v", verify)
	}
	if as := servedAs(t, platform, customHost); as != "" {
		t.Fatalf("an unproved domain is served as %q", as)
	}

	// Verify is a repeated attach; without the record it stays pending.
	again, err := console.AttachDomain(t.Context(), customHost, acme)
	if err != nil || again.Verified || again.Verify == nil ||
		*again.Verify != *verify {
		t.Fatalf("unproved repeat = %+v, %v", again, err)
	}

	platform.ProveDomain(customHost)
	proved, err := console.AttachDomain(t.Context(), customHost, acme)
	if err != nil || !proved.Verified || proved.Verify != nil {
		t.Fatalf("proved repeat = %+v, %v", proved, err)
	}
	if as := servedAs(t, platform, customHost); as != acme {
		t.Fatalf("proved domain served as %q, want %q", as, acme)
	}
}

func TestDomainReportsDNSAndCert(t *testing.T) {
	platform, console, acme := servedTenant(t)
	_, err := console.AttachDomain(t.Context(), customHost, acme)
	if err != nil {
		t.Fatal(err)
	}

	waiting, err := console.Domain(t.Context(), customHost)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.Host != customHost || waiting.Route != acme ||
		!waiting.Verified || waiting.State != provision.DomainChecking ||
		waiting.Reason != "no-reading" || waiting.DNS != "" ||
		!waiting.CheckedAt.IsZero() || !waiting.CertExpiry.IsZero() ||
		waiting.Guidance == "" || waiting.CNAMETarget == "" {
		t.Fatalf("before DNS = %+v", waiting)
	}
	sets := map[string]int{}
	for _, record := range waiting.Records {
		sets[record.Set]++
		if record.Name != customHost || record.TTL != 300 ||
			record.Required {
			t.Errorf("record %+v", record)
		}
		if record.Type == "CNAME" && record.Value != waiting.CNAMETarget {
			t.Errorf("CNAME %q, cname_target %q", record.Value,
				waiting.CNAMETarget)
		}
	}
	if sets["cname"] != 1 || sets["a"] < 1 {
		t.Fatalf("records = %+v", waiting.Records)
	}

	before := time.Now().Add(-time.Second)
	platform.DNSReady(customHost)
	issuing, err := console.Domain(t.Context(), customHost)
	if err != nil || issuing.State != provision.DomainIssuingCert ||
		issuing.Reason != "awaiting-certificate" ||
		issuing.DNS != "pointing" || issuing.CheckedAt.Before(before) ||
		issuing.Guidance != "" || len(issuing.Records) != 0 ||
		issuing.CNAMETarget != "" || !issuing.CertExpiry.IsZero() {
		t.Fatalf("DNS ready = %+v, %v", issuing, err)
	}

	expiry := time.Now().Add(90 * 24 * time.Hour).Truncate(time.Second)
	platform.CertLive(customHost, expiry)
	live, err := console.Domain(t.Context(), customHost)
	if err != nil || live.State != provision.DomainLive ||
		live.Reason != "serving" || !live.CertExpiry.Equal(expiry) {
		t.Fatalf("certificate live = %+v, %v", live, err)
	}

	platform.CertLive(customHost, time.Now().Add(-time.Hour))
	lapsed, err := console.Domain(t.Context(), customHost)
	if err != nil || lapsed.State != provision.DomainIssuingCert {
		t.Fatalf("certificate lapsed = %+v, %v", lapsed, err)
	}

	platform.DomainProof(true)
	const unproved = "shop.example.org"
	if _, err := console.AttachDomain(t.Context(), unproved, acme); err != nil {
		t.Fatal(err)
	}
	pending, err := console.Domain(t.Context(), unproved)
	if err != nil || pending.Verified ||
		pending.State != provision.DomainUnverified ||
		pending.Reason != "ownership-unproven" {
		t.Fatalf("unproved = %+v, %v", pending, err)
	}
	var challenge *provision.DNSRecord
	for index, record := range pending.Records {
		if record.Set == "verify" {
			challenge = &pending.Records[index]
		}
	}
	if challenge == nil || challenge.Type != "TXT" || !challenge.Required ||
		!strings.Contains(pending.Guidance, challenge.Value) {
		t.Fatalf("unproved records = %+v, guidance %q", pending.Records,
			pending.Guidance)
	}

	_, err = console.Domain(t.Context(), "nobody.example.org")
	if !errors.Is(err, provision.ErrNotFound) {
		t.Fatalf("unknown host: %v", err)
	}
}

func TestDetachDomain(t *testing.T) {
	platform, console, acme := servedTenant(t)
	attached, err := console.AttachDomain(t.Context(), customHost, acme)
	if err != nil || !attached.Verified || attached.Verify != nil {
		t.Fatalf("attach without proof = %+v, %v", attached, err)
	}
	if as := servedAs(t, platform, customHost); as != acme {
		t.Fatalf("served as %q, want %q", as, acme)
	}
	if err := console.DetachDomain(t.Context(), customHost); err != nil {
		t.Fatal(err)
	}
	if as := servedAs(t, platform, customHost); as != "" {
		t.Fatalf("a detached domain is served as %q", as)
	}
	_, err = console.Domain(t.Context(), customHost)
	if !errors.Is(err, provision.ErrNotFound) {
		t.Fatalf("Domain after detach: %v", err)
	}
	// Detach is idempotent, so a retry after a lost answer is safe.
	if err := console.DetachDomain(t.Context(), customHost); err != nil {
		t.Fatalf("second detach: %v", err)
	}
}

func TestAttachTakenElsewhereConflicts(t *testing.T) {
	platform, console, acme := servedTenant(t)
	platform.Enable("xyz", "tenant", "xyz.oncarlos.test")
	platform.Serve("xyz", "tenant", provisiontest.Hub())
	stranger := newConsole(t, provision.Config{
		Console: platform.Console(), Account: "xyz", App: "tenant",
		Token: platform.Credential("xyz", "tenant", "instance"),
		HTTP:  platform.Client(), Timeout: 200 * time.Millisecond,
	})
	createHosts(t, stranger, "shop.xyz.oncarlos.test")
	_, err := stranger.AttachDomain(t.Context(), customHost,
		"shop.xyz.oncarlos.test")
	if err != nil {
		t.Fatal(err)
	}

	_, err = console.AttachDomain(t.Context(), customHost, acme)
	var refusal *provision.APIError
	if !errors.Is(err, provision.ErrConflict) || !errors.As(err, &refusal) ||
		refusal.Message != "hostname unavailable" {
		t.Fatalf("attach a held name: %v", err)
	}
	// The holder is not disclosed: a held name reads as no name at all.
	_, err = console.Domain(t.Context(), customHost)
	if !errors.Is(err, provision.ErrNotFound) {
		t.Fatalf("read a held name: %v", err)
	}
	if err := console.DetachDomain(t.Context(), customHost); err != nil {
		t.Fatalf("detach a held name: %v", err)
	}
	if as := servedAs(t, platform, customHost); as != "shop.xyz.oncarlos.test" {
		t.Fatalf("someone else's detach moved the name: served as %q", as)
	}
}

// answering is a client whose every answer is status, header and body.
func answering(status int, header http.Header, body string) *http.Client {
	return &http.Client{Transport: roundTripFunc(
		func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: status, Header: header, Request: request,
				Body: io.NopCloser(strings.NewReader(body)),
			}, nil
		})}
}

func TestDomainCallsClassifyAnswers(t *testing.T) {
	calls := map[string]func(*provision.Console) error{
		"attach": func(console *provision.Console) error {
			_, err := console.AttachDomain(t.Context(), customHost, "acme.x")
			return err
		},
		"get": func(console *provision.Console) error {
			_, err := console.Domain(t.Context(), customHost)
			return err
		},
		"detach": func(console *provision.Console) error {
			return console.DetachDomain(t.Context(), customHost)
		},
	}
	busy := http.Header{"Retry-After": {"7"}}
	for name, call := range calls {
		for _, client := range []*http.Client{
			answering(503, busy, `{"error":"later"}`),
			answering(429, busy, `{"error":"slow down"}`),
		} {
			config := tenantConfig(provisiontest.New())
			config.HTTP = client
			retry := wantRetry(t, call(newConsole(t, config)))
			if retry.After != 7*time.Second {
				t.Errorf("%s: after %v", name, retry.After)
			}
		}
		config := tenantConfig(provisiontest.New())
		config.HTTP = &http.Client{Transport: roundTripFunc(
			func(*http.Request) (*http.Response, error) {
				return nil, errors.New("connection reset")
			})}
		wantRetry(t, call(newConsole(t, config)))

		config.HTTP = answering(409, nil, `{"error":"hostname unavailable"}`)
		err := call(newConsole(t, config))
		if !errors.Is(err, provision.ErrConflict) {
			t.Errorf("%s: 409 gave %v", name, err)
		}
	}
	// A 200 that is not the answer may be a proxy's page.
	for _, name := range []string{"attach", "get"} {
		config := tenantConfig(provisiontest.New())
		config.HTTP = answering(200, nil, "<html>")
		wantRetry(t, calls[name](newConsole(t, config)))
	}
	config := tenantConfig(provisiontest.New())
	console := newConsole(t, config)
	_, err := console.AttachDomain(t.Context(), "", "acme.x")
	if !errors.Is(err, provision.ErrBadRequest) {
		t.Errorf("attach with no host: %v", err)
	}
	_, err = console.Domain(t.Context(), "")
	if !errors.Is(err, provision.ErrBadRequest) {
		t.Errorf("read with no host: %v", err)
	}
	if err := console.DetachDomain(t.Context(), ""); !errors.Is(
		err, provision.ErrBadRequest) {
		t.Errorf("detach with no host: %v", err)
	}
}
