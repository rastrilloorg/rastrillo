package paritytest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// answering is a Surface whose whole app answers every request with
// status and body, as a broken door or a middleware in front might.
func answering(status int, body string) Surface {
	return Surface{Token: "writer-a", Handler: http.HandlerFunc(func(
		response http.ResponseWriter, _ *http.Request,
	) {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(status)
		_, _ = io.WriteString(response, body)
	})}
}

// toolResult is a tools/call answer carrying result.
func toolResult(result string) string {
	return `{"jsonrpc":"2.0","id":1,"result":` + result + `}`
}

// parseError is encoding/json's error for document, as msgNotJSON
// carries it.
func parseError(document string) error {
	return json.Unmarshal([]byte(document), new(any))
}

// TestABrokenAnswerIsReportedNotCompared: a body that is missing, not
// JSON, followed by more, or not an object would otherwise read as no
// body, and compare equal to another door's broken answer.
func TestABrokenAnswerIsReportedNotCompared(t *testing.T) {
	create := named.create.Descriptor
	cases := []struct {
		name string
		call func(t testing.TB) Outcome
		want string
	}{
		{"an API answer with no body", func(t testing.TB) Outcome {
			return CallAPI(t, answering(http.StatusOK, ""), create, nil)
		}, fmt.Sprintf(msgNoBody, apiDoor, create.ID)},
		{"an API answer cut short", func(t testing.TB) Outcome {
			return CallAPI(t, answering(http.StatusOK, `{"id":`), create, nil)
		}, fmt.Sprintf(msgNotJSON, apiDoor, create.ID,
			parseError(`{"id":`))},
		{"an API answer with more after it", func(t testing.TB) Outcome {
			return CallAPI(t, answering(http.StatusOK, `{"id":1}x`),
				create, nil)
		}, fmt.Sprintf(msgNotJSON, apiDoor, create.ID,
			parseError(`{"id":1}x`))},
		{"an API answer that is a number", func(t testing.TB) Outcome {
			return CallAPI(t, answering(http.StatusOK, `42`), create, nil)
		}, fmt.Sprintf(msgNotObject, apiDoor, create.ID, "42")},
		{"an MCP result that is a number", func(t testing.TB) Outcome {
			return CallTool(t, answering(http.StatusOK, toolResult(
				`{"structuredContent":42,`+
					`"content":[{"type":"text","text":"42"}]}`)),
				create, nil)
		}, fmt.Sprintf(msgNotObject, mcpDoor, create.ID, "42")},
		{"an MCP result with no body", func(t testing.TB) Outcome {
			return CallTool(t, answering(http.StatusOK, toolResult(
				`{"content":[{"type":"text","text":"{}"}]}`)), create, nil)
		}, fmt.Sprintf(msgNoBody, mcpDoor, create.ID)},
		{"an MCP tool error with no content", func(t testing.TB) Outcome {
			return CallTool(t, answering(http.StatusOK, toolResult(
				`{"isError":true}`)), create, nil)
		}, fmt.Sprintf(msgEmptyToolError, create.ID)},
		{"an MCP answer that is not JSON", func(t testing.TB) Outcome {
			return CallTool(t, answering(http.StatusOK, `{"jsonrpc"`),
				create, nil)
		}, fmt.Sprintf(msgNotJSON, mcpDoor, create.ID,
			parseError(`{"jsonrpc"`))},
	}
	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			recorder := &spy{TB: t}
			outcome := each.call(recorder)
			if !slices.Equal(recorder.failures, []string{each.want}) {
				t.Errorf("reported:\n\t%s\nwant:\n\t%s",
					strings.Join(recorder.failures, "\n\t"), each.want)
			}
			if outcome.Kind == "ok" {
				t.Errorf("Kind = ok, want anything else")
			}
		})
	}
}

// A None result is no body on either door, and is no failure.
func TestNoResultIsNoBodyOnEitherDoor(t *testing.T) {
	remove := named.remove.Descriptor
	recorder := &spy{TB: t}
	viaAPI := CallAPI(recorder, answering(http.StatusNoContent, ""),
		remove, nil)
	viaMCP := CallTool(recorder, answering(http.StatusOK, toolResult(
		`{"content":[{"type":"text","text":"null"}]}`)), remove, nil)
	if len(recorder.failures) > 0 || viaAPI.Kind != "ok" ||
		viaMCP.Kind != "ok" || viaAPI.Body != nil || viaMCP.Body != nil {
		t.Errorf("API %+v, MCP %+v, reported %v; want ok, no body and "+
			"nothing reported", viaAPI, viaMCP, recorder.failures)
	}
}

func TestToolNamesReportsAListItCannotRead(t *testing.T) {
	refused := `{"jsonrpc":"2.0","id":1,` +
		`"error":{"code":-32603,"message":"boom"}}`
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"a refusal", http.StatusUnauthorized, `{"error":{}}`},
		{"not JSON", http.StatusOK, `<html>`},
		{"a JSON-RPC error", http.StatusOK, refused},
	}
	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			recorder := &spy{TB: t}
			names := ToolNames(recorder, answering(each.status, each.body))
			want := fmt.Sprintf(msgListUnread, each.status, each.body)
			if !slices.Equal(recorder.failures, []string{want}) {
				t.Errorf("reported:\n\t%s\nwant:\n\t%s",
					strings.Join(recorder.failures, "\n\t"), want)
			}
			if len(names) != 0 {
				t.Errorf("ToolNames = %v, want none", names)
			}
		})
	}
}
