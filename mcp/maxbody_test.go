package mcp

import (
	"net/http"
	"strings"
	"testing"
)

// paddedList is a tools/list call made size bytes long by a _meta
// member, which the SDK reads and ignores.
func paddedList(size int) string {
	const frame = `{"jsonrpc":"2.0","id":1,"method":"tools/list",` +
		`"params":{"_meta":{"pad":""}}}`
	return strings.Replace(frame, `"pad":""`,
		`"pad":"`+strings.Repeat("x", size-len(frame))+`"`, 1)
}

func TestBodyUpToTheGateLimitReachesTheTools(t *testing.T) {
	config := testConfig()
	config.Gate.MaxBody = 16 << 20
	url := serve(t, config, bookmarks()...)
	// Over the SDK's own 4 MiB, under what the API door accepts.
	body := paddedList(10 << 20)
	answer := post(t, http.MethodPost, url, writerToken, "2025-06-18", body)
	if answer.status != http.StatusOK {
		t.Errorf("10 MiB under MaxBody 16 MiB: status %d, want 200: %.200s",
			answer.status, answer.body)
	}
}

func TestBodyOverTheGateLimitIsTheAPIs413(t *testing.T) {
	config := testConfig()
	config.Gate.MaxBody = 16 << 20
	url := serve(t, config, bookmarks()...)
	answer := post(t, http.MethodPost, url, writerToken, "2025-06-18",
		paddedList(16<<20+1))
	const want = `{"error":{"code":"too_large",` +
		`"message":"The request body is too large."}}`
	if answer.status != http.StatusRequestEntityTooLarge ||
		strings.TrimSpace(string(answer.body)) != want {
		t.Errorf("status %d, body %.200s; want 413 and %s",
			answer.status, answer.body, want)
	}
	if kind := answer.header.Get("Content-Type"); kind != "application/json" {
		t.Errorf("Content-Type %q, want application/json", kind)
	}
}

func TestDefaultBodyLimitIsTheGates(t *testing.T) {
	url := serve(t, testConfig(), bookmarks()...)
	answer := post(t, http.MethodPost, url, writerToken, "2025-06-18",
		paddedList(1<<20+1))
	if answer.status != http.StatusRequestEntityTooLarge ||
		!strings.Contains(string(answer.body), `"too_large"`) {
		t.Errorf("status %d, body %.200s; want the API's 413",
			answer.status, answer.body)
	}
}
