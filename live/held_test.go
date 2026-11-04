package live

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// These tests play the CARLOS edge's part in a held stream
// (carlos/platform #432): it opens each upstream leg with the page's
// own stream URL and Last-Event-ID set to the last id it relayed, and
// between legs the process may have slept and been started again.
// Spec: docs/superpowers/specs/2026-11-04-live-on-hibernating-routes-design.md.

// edgeLeg opens one upstream leg as the edge does.
func edgeLeg(
	t *testing.T, ctx context.Context, address, cursor string,
) <-chan unit {
	t.Helper()
	header := http.Header{}
	if cursor != "" {
		header.Set("Last-Event-ID", cursor)
	}
	return openStream(t, ctx, address, header)
}

func TestTheHoldMarkerIsIgnoredWhereNoEdgeStripsIt(t *testing.T) {
	hub := fastHub(t, "a")
	hub.Notify("a")
	server, _ := streamServer(t, hub)

	stream := edgeLeg(t, testCtx(t), server.URL+
		"/live?carlos_hold=1&boot="+hub.Boot()+"&t=a@0", "")
	if event := nextEvent(t, stream); event.data != "a@1" {
		t.Fatalf("catch-up = %+v, want change a@1", event)
	}
}

// A woken process has a new boot ID and counters from zero, so the
// cursor from before the sleep must not be read as its own: every
// topic is sent as changed, under the new boot, and the page fetches
// its regions once. After that a reopen carries the new cursor and
// costs nothing.
func TestAHeldStreamCatchesUpAfterTheInstanceWakes(t *testing.T) {
	before := fastHub(t, "a", "b")
	before.Notify("a")
	server, _ := streamServer(t, before)
	query := "/live?boot=" + before.Boot() + "&t=a@1&t=b@0"

	legContext, cancelLeg := context.WithCancel(testCtx(t))
	stream := edgeLeg(t, legContext, server.URL+query, "")
	before.Notify("b")
	cursor := nextEvent(t, stream).id
	// The platform cancels the leg, then stops the process.
	cancelLeg()
	before.Close()
	server.Close()

	after := fastHub(t, "a", "b")
	server, _ = streamServer(t, after)
	stream = edgeLeg(t, testCtx(t), server.URL+query, cursor)
	for _, want := range []string{"a@0", "b@0"} {
		event := nextEvent(t, stream)
		if event.data != want ||
			!strings.HasPrefix(event.id, after.Boot()+" ") {
			t.Fatalf("after the wake, resumed from %q: %+v, want "+
				"data %s under boot %s", cursor, event, want,
				after.Boot())
		}
		cursor = event.id
	}
	quiet(t, stream, 60*time.Millisecond)

	stream = edgeLeg(t, testCtx(t), server.URL+query, cursor)
	quiet(t, stream, 60*time.Millisecond)
	after.Notify("a")
	if event := nextEvent(t, stream); event.data != "a@1" {
		t.Fatalf("a write after the wake = %+v, want change a@1", event)
	}
}
