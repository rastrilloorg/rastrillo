package main

import (
	"context"
	"testing"

	"amadan.net/rastrillo/rastrillo/locales"
	"amadan.net/rastrillo/rastrillo/objrec"
)

// The check is only worth running against the platform if it can tell
// a conditional store from one that ignores conditions.

func TestRunPassesOnAConditionalStore(t *testing.T) {
	if r := run(context.Background(), objrec.Dir(t.TempDir())); !r.OK {
		t.Fatalf("an honest store failed the check: %+v", r)
	}
}

// blindStore accepts every write, as a store that ignored If-None-Match
// and If-Match would.
type blindStore struct{ objrec.Store }

func (b blindStore) Create(ctx context.Context, key string, body []byte) (objrec.Version, error) {
	b.Store.Delete(ctx, key)
	return b.Store.Create(ctx, key, body)
}

func (b blindStore) Update(ctx context.Context, key string, body []byte, _ objrec.Version) (objrec.Version, error) {
	b.Store.Delete(ctx, key)
	return b.Store.Create(ctx, key, body)
}

func TestRunFailsOnAStoreThatIgnoresConditions(t *testing.T) {
	r := run(context.Background(), blindStore{objrec.Dir(t.TempDir())})
	if r.OK {
		t.Fatalf("a store that ignores conditions passed the check: %+v", r)
	}
	t.Logf("caught: %s", r.Error)
}

// Serve refuses to start unless the default locale's pack is linked, and
// run's tests never call Serve, so a missing import passes here and only
// shows up as an instance that exits at boot on the platform.
func TestTheBinaryLinksTheDefaultLocalePack(t *testing.T) {
	if _, err := locales.CheckConfig("", nil); err != nil {
		t.Fatal(err)
	}
}
