package provision_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/provision"
	"amadan.net/rastrillo/rastrillo/provision/provisiontest"
)

const product = "product.test"

// hubConfig attaches product.test to bdf/hub and returns a Config for a
// publish credential bound to hub.
func hubConfig(p *provisiontest.Platform) provision.Config {
	p.Attach("bdf", "hub", product)
	return provision.Config{
		Console: p.Console(),
		Account: "bdf",
		App:     "hub",
		Token:   p.Credential("bdf", "hub", "publish"),
		HTTP:    p.Client(),
		Timeout: 200 * time.Millisecond,
	}
}

func newHub(t *testing.T) (*provisiontest.Platform, *provision.Console) {
	t.Helper()
	p := provisiontest.New()
	return p, newConsole(t, hubConfig(p))
}

func fixed(m map[string]string) func(context.Context) (map[string]string, error) {
	return func(context.Context) (map[string]string, error) { return m, nil }
}

// puts counts the paths PUTs the platform has seen.
func puts(p *provisiontest.Platform) int {
	n := 0
	for _, c := range p.Calls() {
		if strings.HasPrefix(c, "PUT ") {
			n++
		}
	}
	return n
}

func TestPublishSendsTheWholeMap(t *testing.T) {
	p, c := newHub(t)
	want := map[string]string{"acme": "acme.bdf.oncarlos.test", "globex": "globex.bdf.oncarlos.test"}
	got, err := c.PublishPaths(context.Background(), product, fixed(want))
	if err != nil {
		t.Fatalf("PublishPaths: %v", err)
	}
	if got != (provision.Published{Prefixes: 2, Changed: true}) {
		t.Errorf("Published = %+v, want 2 prefixes, changed", got)
	}
	if !reflect.DeepEqual(p.Paths(product), want) {
		t.Errorf("stored map = %v, want %v", p.Paths(product), want)
	}
	got, err = c.PublishPaths(context.Background(), product, fixed(want))
	if err != nil {
		t.Fatalf("second PublishPaths: %v", err)
	}
	if got != (provision.Published{Prefixes: 2, Changed: false}) {
		t.Errorf("same map again: Published = %+v, want 2 prefixes, unchanged", got)
	}
}

func TestPublishRefusesAnEmptyMap(t *testing.T) {
	p, c := newHub(t)
	held := map[string]string{"acme": "acme.bdf.oncarlos.test"}
	if _, err := c.PublishPaths(context.Background(), product, fixed(held)); err != nil {
		t.Fatal(err)
	}
	before := len(p.Calls())
	for name, m := range map[string]map[string]string{"empty": {}, "nil": nil} {
		_, err := c.PublishPaths(context.Background(), product, fixed(m))
		if !errors.Is(err, provision.ErrEmptyMap) {
			t.Errorf("%s map: err = %v, want ErrEmptyMap", name, err)
		}
	}
	if n := len(p.Calls()) - before; n != 0 {
		t.Errorf("an empty map made %d calls, want 0", n)
	}
	if !reflect.DeepEqual(p.Paths(product), held) {
		t.Errorf("stored map = %v, want it unchanged: %v", p.Paths(product), held)
	}
}

func TestPublishSendsNothingWhenTheBuildFails(t *testing.T) {
	p, c := newHub(t)
	boom := errors.New("listing failed")
	_, err := c.PublishPaths(context.Background(), product, func(context.Context) (map[string]string, error) {
		// A partial map next to the error: it must not be sent.
		return map[string]string{"acme": "acme.bdf.oncarlos.test"}, boom
	})
	if err != boom {
		t.Errorf("err = %v, want the build's own error", err)
	}
	if n := len(p.Calls()); n != 0 {
		t.Errorf("a failed build made %d calls, want 0", n)
	}
}

func TestPublishChecksBeforeSending(t *testing.T) {
	p, c := newHub(t)
	const ok = "acme.bdf.oncarlos.test"
	bad := map[string]map[string]string{}
	for _, prefix := range []string{"/acme", "a/b", "..", ".", "", strings.Repeat("a", 256)} {
		bad[fmt.Sprintf("prefix %.20q", prefix)] = map[string]string{prefix: ok}
	}
	for _, target := range []string{"Acme.test", "acme", "a..b.test", strings.Repeat("a", 64) + ".test", "acme.test.", "-a.test", "a-.test", strings.Repeat("a.", 127) + "co"} {
		bad[fmt.Sprintf("target %.20q", target)] = map[string]string{"acme": target}
	}
	over := make(map[string]string, provision.MaxPaths+1)
	for i := 0; i <= provision.MaxPaths; i++ {
		over[fmt.Sprintf("t%d", i)] = ok
	}
	bad["too many"] = over
	for name, m := range bad {
		_, err := c.PublishPaths(context.Background(), product, fixed(m))
		if !errors.Is(err, provision.ErrBadRequest) {
			t.Errorf("%s: err = %v, want ErrBadRequest", name, err)
		}
	}
	if n := len(p.Calls()); n != 0 {
		t.Errorf("checked maps made %d calls, want 0", n)
	}

	// The limits themselves are allowed.
	edge := map[string]string{strings.Repeat("a", 255): strings.Repeat("a", 63) + ".test"}
	if _, err := c.PublishPaths(context.Background(), product, fixed(edge)); err != nil {
		t.Errorf("255-byte prefix and 63-byte label: %v", err)
	}
	full := make(map[string]string, provision.MaxPaths)
	for i := 0; i < provision.MaxPaths; i++ {
		full[fmt.Sprintf("t%d", i)] = ok
	}
	if got, err := c.PublishPaths(context.Background(), product, fixed(full)); err != nil || got.Prefixes != provision.MaxPaths {
		t.Errorf("MaxPaths entries: %+v, %v", got, err)
	}
}

func TestPublishNeedsTheHubsCredential(t *testing.T) {
	p, _ := newHub(t)
	cfg := hubConfig(p)
	// The tenant app's credential is bound to the tenant app, so the hub's
	// route does not exist for it.
	p.Enable("bdf", "tenant", domain)
	cfg.Token = p.Credential("bdf", "tenant", "instance")
	c := newConsole(t, cfg)
	_, err := c.PublishPaths(context.Background(), product, fixed(map[string]string{"acme": "acme.bdf.oncarlos.test"}))
	if !errors.Is(err, provision.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if p.Paths(product) != nil && len(p.Paths(product)) != 0 {
		t.Errorf("stored map = %v, want none", p.Paths(product))
	}
}

func TestPublishOnAnUnheldHostname(t *testing.T) {
	_, c := newHub(t)
	_, err := c.PublishPaths(context.Background(), "other.test", fixed(map[string]string{"acme": "acme.bdf.oncarlos.test"}))
	if !errors.Is(err, provision.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestPublishSerializesBuildsAcrossConsoles(t *testing.T) {
	p := provisiontest.New()
	cfg := hubConfig(p)
	consoles := []*provision.Console{newConsole(t, cfg), newConsole(t, cfg)}

	var mu sync.Mutex
	var slugs []string
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slug := fmt.Sprintf("t%d", i)
			_, err := consoles[i%2].PublishPaths(context.Background(), product, func(context.Context) (map[string]string, error) {
				mu.Lock()
				slugs = append(slugs, slug)
				m := make(map[string]string, len(slugs))
				for _, s := range slugs {
					m[s] = s + ".bdf.oncarlos.test"
				}
				mu.Unlock()
				// Without the lock, the gap between this listing and the
				// PUT is where another publisher's older map lands after
				// a newer one and drops its prefix.
				time.Sleep(2 * time.Millisecond)
				return m, nil
			})
			if err != nil {
				t.Errorf("publisher %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	if got := p.Paths(product); len(got) != 8 {
		t.Errorf("stored map has %d prefixes, want 8: %v", len(got), got)
	}
}

func TestPublishWaitingForTheLockEndsWithTheContext(t *testing.T) {
	_, c := newHub(t)
	inBuild, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := c.PublishPaths(context.Background(), product, func(context.Context) (map[string]string, error) {
			close(inBuild)
			<-release
			return map[string]string{"acme": "acme.bdf.oncarlos.test"}, nil
		})
		done <- err
	}()
	select {
	case <-inBuild:
	case err := <-done:
		t.Fatalf("the holder ended without running its build: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("the holder never ran its build")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	called := false
	_, err := c.PublishPaths(ctx, product, func(context.Context) (map[string]string, error) {
		called = true
		return map[string]string{"globex": "globex.bdf.oncarlos.test"}, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("waiting publisher: err = %v, want the context's", err)
	}
	if called {
		t.Error("a publisher that never got the lock ran its build")
	}
	close(release)
	if err := <-done; err != nil {
		t.Errorf("holder: %v", err)
	}
}

func TestPublishRetries502Once(t *testing.T) {
	p, c := newHub(t)
	p.ScriptPaths(provisiontest.Reply{Status: 502})
	got, err := c.PublishPaths(context.Background(), product, fixed(map[string]string{"acme": "acme.bdf.oncarlos.test"}))
	if err != nil {
		t.Fatalf("PublishPaths: %v", err)
	}
	if got != (provision.Published{Prefixes: 1, Changed: true}) {
		t.Errorf("Published = %+v", got)
	}
	if n := puts(p); n != 2 {
		t.Errorf("PUTs = %d, want 2", n)
	}
}

func TestPublishSecond5xxIsRetryLater(t *testing.T) {
	p, c := newHub(t)
	p.ScriptPaths(provisiontest.Reply{Status: 502}, provisiontest.Reply{Status: 500})
	_, err := c.PublishPaths(context.Background(), product, fixed(map[string]string{"acme": "acme.bdf.oncarlos.test"}))
	var re *provision.RetryError
	if !errors.As(err, &re) || re.Status != 500 {
		t.Fatalf("err = %v, want a *RetryError with status 500", err)
	}
	if n := puts(p); n != 2 {
		t.Errorf("PUTs = %d, want 2", n)
	}
}

func TestPublishRateLimitedIsRetryLater(t *testing.T) {
	p, c := newHub(t)
	p.ScriptPaths(provisiontest.Reply{Status: 429, RetryAfter: "7"})
	_, err := c.PublishPaths(context.Background(), product, fixed(map[string]string{"acme": "acme.bdf.oncarlos.test"}))
	var re *provision.RetryError
	if !errors.As(err, &re) || re.Status != 429 || re.After != 7*time.Second {
		t.Fatalf("err = %v, want a *RetryError{Status: 429, After: 7s}", err)
	}
	if n := puts(p); n != 1 {
		t.Errorf("PUTs = %d, want 1", n)
	}
}
