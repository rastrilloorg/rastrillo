package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"

	rastrillo "amadan.net/rastrillo/rastrillo"
)

// msgInexactIDCopy is the id refusal, kept apart from the const it
// checks, so a change to the reviewed text fails here.
const msgInexactIDCopy = "The id must be a string or a plain integer " +
	"from -9007199254740991 through 9007199254740991."

func idCall(id string) string {
	return `{"jsonrpc":"2.0","id":` + id + `,"method":"tools/call",` +
		`"params":{"name":"bookmarks_get","arguments":{"id":"1"}}}`
}

// countedGet serves bookmarks() with get counting its Invokes.
func countedGet(t *testing.T) (string, *atomic.Int64) {
	invoked := &atomic.Int64{}
	descriptors := withInvoke(bookmarks(), "bookmarks.get",
		func(context.Context, *rastrillo.Ctx, json.RawMessage) (
			any, error,
		) {
			invoked.Add(1)
			return map[string]any{"id": "1"}, nil
		})
	return serve(t, testConfig(), descriptors...), invoked
}

// The SDK reads a numeric id through a float64, so an id past 2^53 would
// run the call and answer it under another id.
func TestInexactIDIsRefused(t *testing.T) {
	for _, id := range []string{
		"9007199254740993", "-9007199254740993", "9007199254740992",
		"1.5", "1e3", "1.0", "123456789012345678901234567890",
	} {
		url, invoked := countedGet(t)
		answer := post(t, http.MethodPost, url, writerToken,
			"2025-06-18", idCall(id))
		refused := answer.rpcError(t)
		if answer.status != http.StatusBadRequest ||
			refused.Error.Code != -32600 ||
			refused.Error.Message != msgInexactIDCopy ||
			string(refused.ID) != "null" {
			t.Errorf("id %s answered %d %s, want a 400 invalid request "+
				"with a null id", id, answer.status, answer.body)
		}
		if invoked.Load() != 0 {
			t.Errorf("id %s reached Invoke", id)
		}
	}
}

func TestExactIDIsAnswered(t *testing.T) {
	for _, id := range []string{
		"9007199254740991", "-9007199254740991", "0",
		`"9007199254740993"`,
	} {
		url, invoked := countedGet(t)
		answer := post(t, http.MethodPost, url, writerToken,
			"2025-06-18", idCall(id))
		var decoded struct{ ID json.RawMessage }
		err := json.Unmarshal(answer.body, &decoded)
		if answer.status != http.StatusOK || err != nil ||
			string(decoded.ID) != id || invoked.Load() != 1 {
			t.Errorf("id %s answered %d %s after %d Invokes, want "+
				"its result under the same id", id, answer.status,
				answer.body, invoked.Load())
		}
	}
}
