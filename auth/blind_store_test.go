package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type blindStoreFault struct {
	BlindLinkStore
	change  func(*BlindLink)
	failure string
}

func (store blindStoreFault) Put(ctx context.Context, link BlindLink) error {
	if store.failure == "put" {
		return errors.New(blindCanary)
	}
	return store.BlindLinkStore.Put(ctx, link)
}

func (store blindStoreFault) Take(ctx context.Context,
	hash string,
) (BlindLink, bool, error) {
	link, ok, err := store.BlindLinkStore.Take(ctx, hash)
	if store.change != nil {
		store.change(&link)
	}
	if store.failure == "not-found" {
		return link, false, nil
	}
	if store.failure == "take" {
		return link, true, errors.New(blindCanary)
	}
	return link, ok, err
}

func (store blindStoreFault) Spend(
	ctx context.Context, key string,
) (int, error) {
	if store.failure == "spend" {
		return 0, errors.New(blindCanary)
	}
	return store.BlindLinkStore.Spend(ctx, key)
}

func (store blindStoreFault) Sweep(ctx context.Context, now time.Time) error {
	if store.failure == "sweep" {
		return errors.New(blindCanary)
	}
	return store.BlindLinkStore.Sweep(ctx, now)
}

func TestBlindLinksRejectMalformedStoreResults(t *testing.T) {
	changes := map[string]func(*BlindLink){
		"wrong-hash": func(link *BlindLink) { link.Hash = "another-token" },
		"empty-key":  func(link *BlindLink) { link.LookupKey = "" },
		"address-key": func(link *BlindLink) {
			link.LookupKey = blindCanary
		},
		"whitespace-key": func(link *BlindLink) { link.LookupKey = " " },
		"trimmed-address": func(link *BlindLink) {
			link.LookupKey = strings.TrimSpace(blindCanary)
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			harness := newBlindHarness(t, true)
			harness.config.BlindLinks.Store = blindStoreFault{
				BlindLinkStore: harness.store, change: change,
			}
			resolved := false
			harness.config.BlindLinks.ResolveSubject = func(context.Context,
				string) (string, error) {
				resolved = true
				return blindSubject, nil
			}
			var err error
			harness.auth, err = New(harness.config)
			if err != nil {
				t.Fatal(err)
			}
			link := harness.send(t)
			response := httptest.NewRecorder()
			harness.auth.Verify(response, redeemRequest(t, link))
			if resolved {
				t.Fatal("invalid store result reached subject resolver")
			}
			location := response.Header().Get("Location")
			if !strings.Contains(location, "err=expired") {
				t.Fatal("invalid store result admitted")
			}
		})
	}
}

func TestBlindLinksDoNotExposeStoreErrors(t *testing.T) {
	operations := []string{"put", "take", "not-found", "spend", "sweep"}
	for _, operation := range operations {
		t.Run(operation, func(t *testing.T) {
			harness := newBlindHarness(t, true)
			harness.config.BlindLinks.Store = blindStoreFault{
				BlindLinkStore: harness.store, failure: operation,
			}
			var err error
			harness.auth, err = New(harness.config)
			if err != nil {
				t.Fatal(err)
			}
			switch operation {
			case "put":
				err = harness.auth.SendLink(
					context.Background(), blindCanary, "test",
				)
			case "spend":
				_, err = harness.auth.SpendLinks(
					context.Background(), blindCanary,
				)
			case "sweep":
				err = harness.auth.Sweep(time.Now())
			case "take", "not-found":
				link := harness.send(t)
				response := httptest.NewRecorder()
				harness.auth.Verify(response, redeemRequest(t, link))
				location := response.Header().Get("Location")
				if !strings.Contains(location, "err=expired") {
					t.Fatal("store error admitted a session")
				}
				harness.scan(t)
				return
			}
			if err == nil {
				t.Fatal("store error lost")
			}
			assertBlindBytes(t, "store error", []byte(err.Error()))
			harness.scan(t)
		})
	}
}

func TestBlindLinksNormalizeOnlyBeforeTheConsumerLookup(t *testing.T) {
	harness := newBlindHarness(t, true)
	var addresses []string
	harness.config.BlindLinks.LookupKey = func(ctx context.Context,
		address string) (string, error) {
		addresses = append(addresses, address)
		return blindKey(ctx, address)
	}
	var err error
	harness.auth, err = New(harness.config)
	if err != nil {
		t.Fatal(err)
	}
	harness.send(t)
	_, err = harness.auth.SpendLinks(context.Background(), blindCanary)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.ToLower(strings.TrimSpace(blindCanary))
	if len(addresses) != 2 || addresses[0] != want || addresses[1] != want {
		t.Fatalf("LookupKey received %q; want normalized addresses", addresses)
	}
}

func TestBlindLinksKeepAddressesOutOfLimiterKeysAndObjects(t *testing.T) {
	harness := newBlindHarness(t, true)
	limiter, ok := harness.auth.flow.Limiter.(*blindLimiter)
	if !ok {
		t.Fatal("blind limiter was not installed")
	}
	probe := &blindLimitProbe{}
	limiter.next = probe
	harness.send(t)
	if len(probe.keys) != 2 {
		t.Fatal("missing limiter keys")
	}
	for _, key := range probe.keys {
		assertBlindBytes(t, "limiter key", []byte(key))
	}

	var record BlindLink
	var expiry int64
	err := harness.store.database.QueryRow(`SELECT hash, lookup_key,
 purpose, expires FROM blind_links`).Scan(&record.Hash, &record.LookupKey,
		&record.Purpose, &expiry)
	if err != nil {
		t.Fatal(err)
	}
	record.Expires = time.Unix(0, expiry)
	object, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	assertBlindBytes(t, "replicated link object", object)
}

type blindLimitProbe struct{ keys []string }

func (probe *blindLimitProbe) Allow(_ context.Context, key string,
	_ time.Time, _ int,
) (bool, error) {
	probe.keys = append(probe.keys, key)
	return true, nil
}

func TestBlindLinksKeepTheAddressRateLimit(t *testing.T) {
	harness := newBlindHarness(t, true)
	for range 5 {
		harness.send(t)
	}
	err := harness.auth.SendLink(context.Background(), blindCanary, "test")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("sixth link: %v; want rate limit", err)
	}
}
