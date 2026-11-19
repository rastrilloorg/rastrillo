package scaffoldcheck

import (
	"path/filepath"
	"strings"
	"testing"
)

// mcpTest is written into the app's own package, where newTokens is. It
// serves the app for real: the token store rechecks each answer as it
// goes out, and a recorder cannot take the write deadline that needs.
const mcpTest = `package PACKAGE

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/apitoken"
	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/dbtest"
	// the app's imports
)

const origin = "http://app.test"

func TestMCPListsNoToolsYet(t *testing.T) {
	schema := dbtest.FromSet(BootSchema)
	defer schema.Remove()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	database, err := db.Open(schema.Path(t), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	// the app's New
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := newTokens(database)
	if err != nil {
		t.Fatal(err)
	}
	secret, _, err := tokens.Mint(context.Background(), "ada@example.com",
		apitoken.MintInput{Agent: "Claude", Access: rastrillo.Read})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client := *server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	for method, want := range map[string]string{
		"initialize": "\"protocolVersion\":\"2025-06-18\"",
		"tools/list": "\"tools\":[]",
	} {
		body := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"" + method +
			"\",\"params\":{\"protocolVersion\":\"2025-06-18\"," +
			"\"capabilities\":{},\"clientInfo\":{\"name\":\"t\"," +
			"\"version\":\"1\"}}}"
		request, err := http.NewRequest(http.MethodPost,
			server.URL+"/api/v1/mcp", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		request.Header.Set("Authorization", "Bearer "+secret)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		answer, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK ||
			!strings.Contains(string(answer), "\"jsonrpc\":\"2.0\"") ||
			!strings.Contains(string(answer), want) {
			t.Errorf("%s: %d, want 200, JSON-RPC and %s:\n%s", method,
				response.StatusCode, want, answer)
		}
	}
}
`

// Every new app serves MCP at /api/v1/mcp, beside its API: a token from
// the app's own store initializes a session at protocol 2025-06-18, and
// an app that exposes nothing lists no tools. It runs on a real server,
// as the app is served, so a missing mount or a redirect shows.
func TestEveryNewAppServesMCPBesideItsAPI(t *testing.T) {
	t.Parallel()
	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			t.Parallel()
			directory := buildableApp(t, variant.name, variant.flags...)
			source := strings.ReplaceAll(mcpTest, "PACKAGE", variant.name)
			write(t, filepath.Join(directory, "internal", variant.name,
				"mcp_extra_test.go"), withTheVariantsNew(variant, source))
			runTests(t, directory, "./internal/"+variant.name+"/",
				"TestMCPListsNoToolsYet")
		})
	}
}
