package paritytest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"

	"amadan.net/rastrillo/rastrillo/api"
	"amadan.net/rastrillo/rastrillo/mcp"
	"amadan.net/rastrillo/rastrillo/op"
)

// agentSession is the Agent-Session CallAPI and CallTool send, so a
// write passes RequireAgentSession.
const agentSession = "paritytest"

// mcpPath is where the MCP endpoint is mounted, under the API's prefix.
const mcpPath = "/api/v1/mcp"

// Outcome is one door's answer to one call.
type Outcome struct {
	// Status is the HTTP status. MCP answers a refused tool call with
	// 200, so compare Kind, not Status, across doors.
	Status int
	Kind   string   // "ok" for 2xx, else the error's code
	Fields []string // an invalid answer's field names, sorted
	// FieldMessages is an invalid answer's message for each field.
	FieldMessages map[string]string
	Body          map[string]any // numbers as json.Number
	// Message is a refusal's message. The API and MCP both write it
	// with api.Refusal, so Run compares their messages; a page renders its
	// own HTML wording.
	Message string
}

// CallAPI calls descriptor through surface.Handler's API with
// surface.Token, as an agent with a session: a JSON POST, or a GET with
// input as the query for a read declared InQuery.
func CallAPI(t testing.TB, surface Surface, descriptor op.Descriptor,
	input map[string]any) Outcome {
	t.Helper()
	sent := call{descriptor: descriptor, token: surface.Token,
		session: agentSession}
	if descriptor.InQuery {
		sent.query = queryOf(input)
	} else {
		sent.arguments = argumentsOf(t, input)
	}
	return sent.api(t, surface.Handler)
}

// CallTool calls descriptor's tool through surface.Handler's MCP
// endpoint with surface.Token, as one stateless tools/call.
func CallTool(t testing.TB, surface Surface, descriptor op.Descriptor,
	input map[string]any) Outcome {
	t.Helper()
	sent := call{descriptor: descriptor, token: surface.Token,
		arguments: argumentsOf(t, input), session: agentSession}
	return sent.tool(t, surface.Handler)
}

// ToolNames is the MCP tool list surface.Token is shown, sorted. A list
// it can't read, a refusal included, is reported with t.Errorf, and
// ToolNames returns nil, so a broken endpoint never passes for one that
// lists nothing.
func ToolNames(t testing.TB, surface Surface) []string {
	t.Helper()
	names, _ := toolNames(t, surface)
	return names
}

// toolNames is ToolNames, and whether it read a list.
func toolNames(t testing.TB, surface Surface) ([]string, bool) {
	t.Helper()
	lister := call{token: surface.Token, session: agentSession}
	answer := send(t, surface.Handler, http.MethodPost, mcpPath,
		mcpHeader(lister.header()),
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	// Tools is a pointer, so a list left out or null is told apart from
	// an empty one.
	var listed struct {
		Result *struct {
			Tools *[]struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	err := json.Unmarshal(answer.body, &listed)
	if answer.status/100 != 2 || err != nil || listed.Result == nil ||
		listed.Result.Tools == nil {
		t.Errorf(msgListUnread, answer.status, answer.body)
		return nil, false
	}
	names := []string{}
	for _, tool := range *listed.Result.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names, true
}

// call is one request to a door, as CallAPI and CallTool make it, kept
// apart so a test can send what they never would: no Agent-Session, or
// arguments that are not a JSON object of distinct members.
type call struct {
	descriptor op.Descriptor
	token      string
	// arguments are the JSON body, or a tool's arguments. A read served
	// at GET takes query instead.
	arguments []byte
	query     url.Values
	session   string // Agent-Session; empty sends none
}

func (sent call) api(t testing.TB, handler http.Handler) Outcome {
	t.Helper()
	// The route api.Operations builds, so the call goes where the
	// declaration says the API serves it.
	served := api.Operations(sent.descriptor)[0]
	target, header := served.Pattern, sent.header()
	if sent.query != nil {
		target += "?" + sent.query.Encode()
	}
	if sent.arguments != nil {
		header.Set("Content-Type", "application/json")
	}
	answer := send(t, handler, served.Method, target, header, sent.arguments)
	if answer.status/100 != 2 {
		outcome := refusalOf(answer.body)
		outcome.Status = answer.status
		return outcome
	}
	// 204 is a None result. Any other success must carry its object.
	outcome := Outcome{Status: answer.status, Kind: "ok"}
	if answer.status == http.StatusNoContent {
		return outcome
	}
	body, read := objectOf(t, apiDoor, sent.descriptor.ID, answer.body)
	if !read {
		outcome.Kind = ""
	}
	outcome.Body = body
	return outcome
}

func (sent call) tool(t testing.TB, handler http.Handler) Outcome {
	t.Helper()
	body := fmt.Appendf(nil, `{"jsonrpc":"2.0","id":1,`+
		`"method":"tools/call","params":{"name":%q,"arguments":%s}}`,
		mcp.ToolName(sent.descriptor.ID), sent.arguments)
	answer := send(t, handler, http.MethodPost, mcpPath,
		mcpHeader(sent.header()), body)
	// The gate refuses before MCP does, in the API's own shape.
	outcome := refusalOf(answer.body)
	if answer.status/100 == 2 {
		outcome = toolOutcome(t, sent.descriptor.ID, answer.body)
	}
	outcome.Status = answer.status
	return outcome
}

// toolReply is the part of a tools/call answer an Outcome is read from.
type toolReply struct {
	Result *struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		StructuredContent json.RawMessage `json:"structuredContent"`
	} `json:"result"`
}

// toolOutcome reads a tools/call answer as an Outcome without its
// Status. One it can't read is reported, and has no Kind.
func toolOutcome(t testing.TB, id string, body []byte) Outcome {
	t.Helper()
	var reply toolReply
	if err := json.Unmarshal(body, &reply); err != nil {
		t.Errorf(msgNotJSON, mcpDoor, id, err)
		return Outcome{}
	}
	result := reply.Result
	switch {
	// A JSON-RPC error, such as an unknown tool, has no code of the
	// API's, so its Kind is "".
	case result == nil:
		return Outcome{}
	case result.IsError && len(result.Content) == 0:
		t.Errorf(msgEmptyToolError, id)
		return Outcome{}
	// A refused call carries the API's error envelope as its text.
	case result.IsError:
		return refusalOf([]byte(result.Content[0].Text))
	// A None result is the text null, with no structured content.
	case len(result.StructuredContent) == 0 && len(result.Content) == 1 &&
		result.Content[0].Text == "null":
		return Outcome{Kind: "ok"}
	}
	object, read := objectOf(t, mcpDoor, id, result.StructuredContent)
	if !read {
		return Outcome{}
	}
	return Outcome{Kind: "ok", Body: object}
}

// header is the call's credential and session.
func (sent call) header() http.Header {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+sent.token)
	if sent.session != "" {
		header.Set("Agent-Session", sent.session)
	}
	return header
}

// mcpHeader adds what a streamable HTTP client sends with each POST.
func mcpHeader(header http.Header) http.Header {
	header.Set("Content-Type", "application/json")
	header.Set("Accept", "application/json, text/event-stream")
	return header
}

// refusalOf reads the API's error envelope, {"error": {"code",
// "fields"}}, as an Outcome without its Status.
func refusalOf(body []byte) Outcome {
	var envelope struct {
		Error api.Error `json:"error"`
	}
	_ = json.Unmarshal(body, &envelope)
	outcome := Outcome{Kind: envelope.Error.Code,
		Message: envelope.Error.Message}
	if len(envelope.Error.Fields) > 0 {
		outcome.Fields = slices.Sorted(maps.Keys(envelope.Error.Fields))
		outcome.FieldMessages = envelope.Error.Fields
	}
	return outcome
}

// objectOf decodes door's answer to operation id, a JSON object, with
// numbers kept as written, so an id past 2^53 compares exactly. Nothing,
// bad JSON, anything after the value, or another kind of value is
// reported, and false.
func objectOf(
	t testing.TB, door, id string, encoded []byte,
) (map[string]any, bool) {
	t.Helper()
	if len(bytes.TrimSpace(encoded)) == 0 {
		t.Errorf(msgNoBody, door, id)
		return nil, false
	}
	// Unmarshal for its error, because it also refuses what follows the
	// value, which a Decoder leaves unread. Into a RawMessage, so a
	// number past float64's range is checked as JSON, not converted.
	if err := json.Unmarshal(encoded, new(json.RawMessage)); err != nil {
		t.Errorf(msgNotJSON, door, id, err)
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var object map[string]any
	if decoder.Decode(&object) != nil || object == nil {
		t.Errorf(msgNotObject, door, id, encoded)
		return nil, false
	}
	return object, true
}

func argumentsOf(t testing.TB, input map[string]any) []byte {
	t.Helper()
	if input == nil {
		return []byte("{}")
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

// queryOf writes input as a query string, each value as its text, which
// the API reads back as its property's type.
func queryOf(input map[string]any) url.Values {
	query := url.Values{}
	for name, value := range input {
		query.Set(name, fmt.Sprint(value))
	}
	return query
}

// received is one response, read whole.
type received struct {
	status int
	header http.Header
	body   []byte
}

// send serves one request through handler on a real server, so it
// crosses the transport a deployed app has: the gate's write deadlines
// need one. A redirect is answered, not followed.
func send(
	t testing.TB, handler http.Handler,
	method, target string, header http.Header, body []byte,
) received {
	t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()
	request, err := http.NewRequestWithContext(t.Context(), method,
		server.URL+target, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header = header
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	read, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return received{response.StatusCode, response.Header, read}
}
