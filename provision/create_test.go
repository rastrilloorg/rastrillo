package provision_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/provision"
	"amadan.net/rastrillo/rastrillo/provision/provisiontest"
)

const domain = "bdf.oncarlos.test"

// tenantConfig turns instances on for bdf/tenant and returns a Config that
// holds its instance credential. The timeout is short so a hung attempt
// ends inside the test, and still long enough that a fake answer is never
// cut off.
func tenantConfig(p *provisiontest.Platform) provision.Config {
	p.Enable("bdf", "tenant", domain)
	return provision.Config{
		Console: p.Console(),
		Account: "bdf",
		App:     "tenant",
		Token:   p.Credential("bdf", "tenant", "instance"),
		HTTP:    p.Client(),
		Timeout: 200 * time.Millisecond,
	}
}

func newConsole(t *testing.T, cfg provision.Config) *provision.Console {
	t.Helper()
	c, err := provision.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c == nil {
		t.Fatal("New returned no Console")
	}
	return c
}

func newTenant(t *testing.T) (*provisiontest.Platform, *provision.Console, string) {
	t.Helper()
	p := provisiontest.New()
	return p, newConsole(t, tenantConfig(p)), domain
}

// posts counts the create POSTs the platform has seen.
func posts(p *provisiontest.Platform) int {
	n := 0
	for _, c := range p.Calls() {
		if strings.HasPrefix(c, "POST ") {
			n++
		}
	}
	return n
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// failing sends requests of one method nowhere and fails them with err;
// every other request reaches the platform.
func failing(p *provisiontest.Platform, method string, err error) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == method {
			return nil, err
		}
		return p.RoundTrip(r)
	})}
}

// permanent are the errors that tell an app to stop: none may come out of
// an attempt whose outcome is unknown.
var permanent = []error{
	provision.ErrHostTaken, provision.ErrConflict, provision.ErrNotFound,
	provision.ErrUnauthorized, provision.ErrForbidden, provision.ErrBadRequest,
}

func wantRetry(t *testing.T, err error) *provision.RetryError {
	t.Helper()
	var re *provision.RetryError
	if !errors.As(err, &re) {
		t.Fatalf("got %v (%T), want a *RetryError", err, err)
	}
	for _, e := range permanent {
		if errors.Is(err, e) {
			t.Errorf("%v is %v: an unknown outcome read as a permanent one", err, e)
		}
	}
	return re
}

func TestCreateCreates(t *testing.T) {
	p, c, d := newTenant(t)
	h := "acme." + d
	got, err := c.Create(context.Background(), provision.CreateRequest{Host: h})
	if err != nil || got != (provision.Created{Host: h}) {
		t.Fatalf("Create = %+v, %v; want Created{Host: %s}", got, err, h)
	}
	if hosts := p.Instances("bdf", "tenant"); len(hosts) != 1 || hosts[0] != h {
		t.Fatalf("platform holds %v, want [%s]", hosts, h)
	}
}

func TestCreateFirstDuplicateIsHostTaken(t *testing.T) {
	p, c, d := newTenant(t)
	h := "acme." + d
	if _, err := c.Create(context.Background(), provision.CreateRequest{Host: h}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	before := len(p.Calls())
	_, err := c.Create(context.Background(), provision.CreateRequest{Host: h})
	if !errors.Is(err, provision.ErrHostTaken) {
		t.Fatalf("second Create: got %v, want ErrHostTaken", err)
	}
	var apiErr *provision.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict {
		t.Fatalf("second Create: %v carries no *APIError with status 409", err)
	}
	calls := p.Calls()[before:]
	if len(calls) != 2 || !strings.HasPrefix(calls[0], "POST ") || !strings.HasPrefix(calls[1], "GET ") {
		t.Fatalf("calls = %v, want POST then GET", calls)
	}
}

func TestCreateNotEnabledIsRefused(t *testing.T) {
	p := provisiontest.New()
	c := newConsole(t, provision.Config{
		Console: p.Console(), Account: "bdf", App: "tenant",
		Token: p.Credential("bdf", "tenant", "instance"),
		HTTP:  p.Client(), Timeout: 200 * time.Millisecond,
	})
	_, err := c.Create(context.Background(), provision.CreateRequest{Host: "acme." + domain})
	if !errors.Is(err, provision.ErrConflict) || errors.Is(err, provision.ErrHostTaken) {
		t.Fatalf("got %v, want ErrConflict and not ErrHostTaken", err)
	}
	if !strings.Contains(err.Error(), "not enabled") {
		t.Errorf("error %q does not name the reason", err)
	}
	if n := posts(p); n != 1 {
		t.Errorf("%d POSTs, want 1", n)
	}
}

func TestCreateRetryAfterTimeoutLands(t *testing.T) {
	p, c, d := newTenant(t)
	h := "acme." + d
	p.Script(provisiontest.Reply{Hang: true, Record: true})
	got, err := c.Create(context.Background(), provision.CreateRequest{Host: h})
	if err != nil || got != (provision.Created{Host: h, Landed: true}) {
		t.Fatalf("Create = %+v, %v; want Landed", got, err)
	}
	if n := posts(p); n != 1 {
		t.Errorf("%d POSTs, want 1: the GET found the instance, so nothing is posted twice", n)
	}
}

func TestCreateRetryAfter502Lands(t *testing.T) {
	p, c, d := newTenant(t)
	h := "acme." + d
	p.Script(provisiontest.Reply{Status: http.StatusBadGateway, Record: true})
	got, err := c.Create(context.Background(), provision.CreateRequest{Host: h})
	if err != nil || got != (provision.Created{Host: h, Landed: true}) {
		t.Fatalf("Create = %+v, %v; want Landed", got, err)
	}
	if n := posts(p); n != 1 {
		t.Errorf("%d POSTs, want 1", n)
	}
}

func TestCreateRetryAfter502WithoutTheWritePostsAgain(t *testing.T) {
	p, c, d := newTenant(t)
	h := "acme." + d
	p.Script(provisiontest.Reply{Status: http.StatusBadGateway})
	got, err := c.Create(context.Background(), provision.CreateRequest{Host: h})
	if err != nil || got != (provision.Created{Host: h}) {
		t.Fatalf("Create = %+v, %v; want Created{Host: %s}", got, err, h)
	}
	if n := posts(p); n != 2 {
		t.Errorf("%d POSTs, want 2", n)
	}
}

// A 5xx other than 503's "no room" may come after the write, from the
// console or a proxy in front of it, so it is reconciled like a 500: read
// as a refusal it would drop a host that may be live.
func TestCreateOther5xxIsUnknown(t *testing.T) {
	for _, status := range []int{http.StatusNotImplemented, http.StatusGatewayTimeout} {
		t.Run(http.StatusText(status)+" after the write", func(t *testing.T) {
			p, c, d := newTenant(t)
			h := "acme." + d
			p.Script(provisiontest.Reply{Status: status, Record: true})
			got, err := c.Create(context.Background(), provision.CreateRequest{Host: h})
			if err != nil || got != (provision.Created{Host: h, Landed: true}) {
				t.Fatalf("Create = %+v, %v; want Landed", got, err)
			}
			if n := posts(p); n != 1 {
				t.Errorf("%d POSTs, want 1", n)
			}
		})
		t.Run(http.StatusText(status)+" without the write", func(t *testing.T) {
			p, c, d := newTenant(t)
			h := "acme." + d
			p.Script(provisiontest.Reply{Status: status})
			got, err := c.Create(context.Background(), provision.CreateRequest{Host: h})
			if err != nil || got != (provision.Created{Host: h}) {
				t.Fatalf("Create = %+v, %v; want Created{Host: %s}", got, err, h)
			}
			if n := posts(p); n != 2 {
				t.Errorf("%d POSTs, want 2", n)
			}
		})
		t.Run(http.StatusText(status)+" twice", func(t *testing.T) {
			p, c, d := newTenant(t)
			p.Script(provisiontest.Reply{Status: status}, provisiontest.Reply{Status: status})
			_, err := c.Create(context.Background(), provision.CreateRequest{Host: "acme." + d})
			if re := wantRetry(t, err); re.Status != status {
				t.Errorf("RetryError.Status = %d, want %d", re.Status, status)
			}
			if n := posts(p); n != 2 {
				t.Errorf("%d POSTs, want 2", n)
			}
		})
	}
}

func TestCreateRetriesOnceOnly(t *testing.T) {
	p, c, d := newTenant(t)
	p.Script(provisiontest.Reply{Status: http.StatusBadGateway}, provisiontest.Reply{Status: http.StatusBadGateway})
	_, err := c.Create(context.Background(), provision.CreateRequest{Host: "acme." + d})
	if re := wantRetry(t, err); re.Status != http.StatusBadGateway {
		t.Errorf("RetryError.Status = %d, want 502", re.Status)
	}
	if n := posts(p); n != 2 {
		t.Errorf("%d POSTs, want 2", n)
	}
}

func TestCreateTransportErrorIsUnknown(t *testing.T) {
	p := provisiontest.New()
	cfg := tenantConfig(p)
	cfg.HTTP = failing(p, http.MethodPost, io.ErrUnexpectedEOF)
	c := newConsole(t, cfg)
	_, err := c.Create(context.Background(), provision.CreateRequest{Host: "acme." + domain})
	wantRetry(t, err)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("%v does not wrap the transport's error", err)
	}
}

// brokenBody returns part of a body and then fails, as a connection that
// drops after the status line does.
type brokenBody struct{ sent bool }

func (b *brokenBody) Read(p []byte) (int, error) {
	if !b.sent {
		b.sent = true
		return copy(p, `{"host":"ac`), nil
	}
	return 0, io.ErrUnexpectedEOF
}

func (b *brokenBody) Close() error { return nil }

func TestCreateBodyReadErrorIsUnknown(t *testing.T) {
	p := provisiontest.New()
	cfg := tenantConfig(p)
	var mu sync.Mutex
	sent := 0
	// The POST never reaches the fake: its own answer would create the
	// instance, and the reconcile GET would then end the test as Landed.
	cfg.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			return p.RoundTrip(r)
		}
		mu.Lock()
		sent++
		mu.Unlock()
		return &http.Response{
			StatusCode: http.StatusCreated,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       &brokenBody{},
			Request:    r,
		}, nil
	})}
	c := newConsole(t, cfg)
	_, err := c.Create(context.Background(), provision.CreateRequest{Host: "acme." + domain})
	wantRetry(t, err)
	mu.Lock()
	defer mu.Unlock()
	if sent != 2 {
		t.Errorf("%d POSTs, want 2: the first is reconciled, the second is the last", sent)
	}
}

func TestCreateResumeReconcilesFirst(t *testing.T) {
	p, c, d := newTenant(t)
	h := "acme." + d
	if _, err := c.Create(context.Background(), provision.CreateRequest{Host: h}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	before := len(p.Calls())
	got, err := c.Create(context.Background(), provision.CreateRequest{Host: h, Resume: true})
	if err != nil || got != (provision.Created{Host: h, Landed: true}) {
		t.Fatalf("Create(Resume) = %+v, %v; want Landed", got, err)
	}
	if calls := p.Calls()[before:]; len(calls) != 1 || !strings.HasPrefix(calls[0], "GET ") {
		t.Fatalf("new calls = %v, want one GET", calls)
	}
}

func TestCreateResumeOfAMissingHostPosts(t *testing.T) {
	p, c, d := newTenant(t)
	h := "acme." + d
	got, err := c.Create(context.Background(), provision.CreateRequest{Host: h, Resume: true})
	if err != nil || got != (provision.Created{Host: h}) {
		t.Fatalf("Create(Resume) = %+v, %v; want Created{Host: %s}", got, err, h)
	}
	calls := p.Calls()
	if len(calls) != 2 || !strings.HasPrefix(calls[0], "GET ") || !strings.HasPrefix(calls[1], "POST ") {
		t.Fatalf("calls = %v, want GET then POST", calls)
	}
}

// Spec §7.2 step 3 has no retry for an auth failure: a revoked credential
// read as "try again" shows the person a retry button for ever.
func TestCreateResumeWithABadToken(t *testing.T) {
	p := provisiontest.New()
	cfg := tenantConfig(p)
	cfg.Token = "revoked"
	c := newConsole(t, cfg)
	_, err := c.Create(context.Background(), provision.CreateRequest{Host: "acme." + domain, Resume: true})
	var apiErr *provision.APIError
	var re *provision.RetryError
	if !errors.Is(err, provision.ErrUnauthorized) || !errors.As(err, &apiErr) || errors.As(err, &re) {
		t.Fatalf("got %v (%T), want an *APIError that is ErrUnauthorized", err, err)
	}
	if n := posts(p); n != 0 {
		t.Errorf("%d POSTs, want 0", n)
	}
}

func TestCreateResumeWhenTheGetFails(t *testing.T) {
	p := provisiontest.New()
	cfg := tenantConfig(p)
	cfg.HTTP = failing(p, http.MethodGet, io.ErrUnexpectedEOF)
	c := newConsole(t, cfg)
	_, err := c.Create(context.Background(), provision.CreateRequest{Host: "acme." + domain, Resume: true})
	wantRetry(t, err)
	if n := posts(p); n != 0 {
		t.Errorf("%d POSTs, want 0", n)
	}
}

func TestCreateBusyIsRetryLater(t *testing.T) {
	p, c, d := newTenant(t)
	p.Script(provisiontest.Reply{Status: http.StatusConflict, RetryAfter: "2"})
	_, err := c.Create(context.Background(), provision.CreateRequest{Host: "acme." + d})
	if re := wantRetry(t, err); re.Status != http.StatusConflict || re.After != 2*time.Second {
		t.Errorf("RetryError = %+v, want Status 409 and After 2s", re)
	}
	if n := posts(p); n != 1 {
		t.Errorf("%d POSTs, want 1", n)
	}
}

func TestCreateRateLimitedNeverLoops(t *testing.T) {
	p, c, d := newTenant(t)
	p.Script(provisiontest.Reply{Status: http.StatusTooManyRequests})
	_, err := c.Create(context.Background(), provision.CreateRequest{Host: "acme." + d})
	if re := wantRetry(t, err); re.Status != http.StatusTooManyRequests {
		t.Errorf("RetryError.Status = %d, want 429", re.Status)
	}
	if n := posts(p); n != 1 {
		t.Errorf("%d POSTs, want 1", n)
	}
}

func TestCreateNoRoomIsRetryLater(t *testing.T) {
	p, c, d := newTenant(t)
	p.Script(provisiontest.Reply{Status: http.StatusServiceUnavailable, RetryAfter: "60"})
	_, err := c.Create(context.Background(), provision.CreateRequest{Host: "acme." + d})
	if re := wantRetry(t, err); re.After != time.Minute {
		t.Errorf("RetryError.After = %v, want 1m", re.After)
	}
}

func TestCreateAccepted(t *testing.T) {
	p, c, d := newTenant(t)
	h := "acme." + d
	p.Script(provisiontest.Reply{Status: http.StatusAccepted})
	got, err := c.Create(context.Background(), provision.CreateRequest{Host: h})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Host != h || !got.Accepted || got.Landed || got.Operation != "op1" || got.Phase != "creating" {
		t.Fatalf("Create = %+v; want Host %s, Accepted, Operation op1, Phase creating", got, h)
	}
}

func TestCreateRefusals(t *testing.T) {
	p := provisiontest.New()
	base := tenantConfig(p)
	cases := []struct {
		name string
		mod  func(*provision.Config)
		host string
		want error
	}{
		{"wrong token", func(c *provision.Config) { c.Token = "nope" }, "acme." + domain, provision.ErrUnauthorized},
		{"publish credential", func(c *provision.Config) { c.Token = p.Credential("bdf", "tenant", "publish") }, "acme." + domain, provision.ErrForbidden},
		{"another app", func(c *provision.Config) { c.App = "other" }, "acme." + domain, provision.ErrNotFound},
		{"host off the domain", func(*provision.Config) {}, "acme.example.org", provision.ErrBadRequest},
	}
	for _, tc := range cases {
		cfg := base
		tc.mod(&cfg)
		c := newConsole(t, cfg)
		_, err := c.Create(context.Background(), provision.CreateRequest{Host: tc.host})
		var apiErr *provision.APIError
		if !errors.Is(err, tc.want) || !errors.As(err, &apiErr) {
			t.Errorf("%s: got %v, want an *APIError that is %v", tc.name, err, tc.want)
		}
	}
	if hosts := p.Instances("bdf", "tenant"); len(hosts) != 0 {
		t.Errorf("a refused create left %v", hosts)
	}
}

func TestCreateChecksBeforeSending(t *testing.T) {
	p, c, d := newTenant(t)
	h := "acme." + d
	cases := []provision.CreateRequest{
		{Host: "Acme X"},
		{Host: h, Message: "two\nlines"},
		{Host: h, Message: strings.Repeat("é", 241)},
		{Host: h, Message: "bad \xff utf-8"},
	}
	for _, req := range cases {
		if _, err := c.Create(context.Background(), req); !errors.Is(err, provision.ErrBadRequest) {
			t.Errorf("Create(host %q, message of %d bytes): got %v, want ErrBadRequest", req.Host, len(req.Message), err)
		}
	}
	if calls := p.Calls(); len(calls) != 0 {
		t.Errorf("calls = %v, want none: a request the platform refuses is not sent", calls)
	}
}

func TestCreateStopsWhenTheCallerGivesUp(t *testing.T) {
	p, c, d := newTenant(t)
	p.Script(provisiontest.Reply{Hang: true})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(50*time.Millisecond, cancel)
	_, err := c.Create(ctx, provision.CreateRequest{Host: "acme." + d})
	var re *provision.RetryError
	if !errors.Is(err, context.Canceled) || errors.As(err, &re) {
		t.Fatalf("got %v (%T), want context.Canceled: only the attempt's own deadline earns the retry", err, err)
	}
	if n := posts(p); n != 1 {
		t.Errorf("%d POSTs, want 1", n)
	}
}

func TestCreateObserves(t *testing.T) {
	p := provisiontest.New()
	cfg := tenantConfig(p)
	var seen []provision.Exchange
	cfg.Observe = func(e provision.Exchange) { seen = append(seen, e) }
	c := newConsole(t, cfg)
	if _, err := c.Create(context.Background(), provision.CreateRequest{Host: "acme." + domain}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(seen) != 1 || seen[0].Method != http.MethodPost || seen[0].Status != http.StatusCreated {
		t.Fatalf("Observe saw %+v, want one POST answered 201", seen)
	}
	e := seen[0]
	fields := []string{e.Method, e.Path, e.Message}
	if e.Err != nil {
		fields = append(fields, e.Err.Error())
	}
	for k, vs := range e.Header {
		fields = append(fields, k)
		fields = append(fields, vs...)
	}
	for _, f := range fields {
		if strings.Contains(f, cfg.Token) {
			t.Errorf("an Exchange field carries the token: %q", f)
		}
	}
}

func TestInstanceGets(t *testing.T) {
	_, c, d := newTenant(t)
	h := "acme." + d
	if _, err := c.Create(context.Background(), provision.CreateRequest{Host: h}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	in, err := c.Instance(context.Background(), h)
	if err != nil || in.Host != h || in.App != "tenant" || in.CreatedAt.IsZero() || in.Gated {
		t.Fatalf("Instance = %+v, %v; want host %s of app tenant, created, not gated", in, err, h)
	}
	if _, err := c.Instance(context.Background(), "nobody."+d); !errors.Is(err, provision.ErrNotFound) {
		t.Fatalf("Instance(unknown): got %v, want ErrNotFound", err)
	}
}

// notARecordOf answers every GET of an instance with a 200 whose body is
// body, and sends everything else to the platform.
func notARecordOf(p *provisiontest.Platform, body string) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/instances/") {
			return &http.Response{
				StatusCode: http.StatusOK, Request: r,
				Header: http.Header{"Content-Type": {"application/json"}},
				Body:   io.NopCloser(strings.NewReader(body)),
			}, nil
		}
		return p.RoundTrip(r)
	})}
}

// A 200 that does not name the requested host proves nothing landed:
// json.Unmarshal takes null and {} as a record, and a proxy can hand back
// another host's. Read as found, each makes Create report Landed for an
// instance that may not exist, and the app waits for it for ever.
func TestInstanceRefusesARecordOfAnotherHost(t *testing.T) {
	bodies := map[string]string{
		"null":         `null`,
		"empty":        `{}`,
		"another host": `{"host":"globex.` + domain + `","app":"tenant"}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			p := provisiontest.New()
			cfg := tenantConfig(p)
			direct := newConsole(t, cfg)
			cfg.HTTP = notARecordOf(p, body)
			c := newConsole(t, cfg)
			h := "acme." + domain

			_, err := c.Instance(context.Background(), h)
			wantRetry(t, err)

			// Resume: the reconcile before any post.
			got, err := c.Create(context.Background(), provision.CreateRequest{Host: h, Resume: true})
			wantRetry(t, err)
			if got.Landed {
				t.Errorf("Create(Resume) = %+v: Landed on a record of another host", got)
			}
			if n := posts(p); n != 0 {
				t.Errorf("%d POSTs after Resume, want 0: the GET could not tell", n)
			}

			// Conflict: the host exists, the POST meets 409, and the GET
			// that should explain it does not name the host.
			if _, err := direct.Create(context.Background(), provision.CreateRequest{Host: h}); err != nil {
				t.Fatal(err)
			}
			got, err = c.Create(context.Background(), provision.CreateRequest{Host: h})
			wantRetry(t, err)
			if got.Landed {
				t.Errorf("Create after a 409 = %+v: Landed on a record of another host", got)
			}
		})
	}
}

// An unknown outcome is reconciled after the post too: a 502 whose write
// landed, then a GET that does not name the host, is still unknown.
func TestCreateUnknownOutcomeRefusesARecordOfAnotherHost(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `{"host":"globex.` + domain + `"}`} {
		p := provisiontest.New()
		cfg := tenantConfig(p)
		cfg.HTTP = notARecordOf(p, body)
		c := newConsole(t, cfg)
		p.Script(provisiontest.Reply{Status: http.StatusBadGateway, Record: true})
		got, err := c.Create(context.Background(), provision.CreateRequest{Host: "acme." + domain})
		wantRetry(t, err)
		if got.Landed {
			t.Errorf("body %s: Create = %+v, Landed on a record of another host", body, got)
		}
		if n := posts(p); n != 1 {
			t.Errorf("body %s: %d POSTs, want 1: a GET that cannot tell must not lead to a blind second post", body, n)
		}
	}
}
