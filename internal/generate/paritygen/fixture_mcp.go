package paritygen

// fixtureMCP is the part of gen/paritytest/fixture.go the MCP leg and
// the list-limit legs use. Every resource has both, so it is never left
// out. The JSON field names in its structs are matched without regard
// to case, which keeps backticks out of this raw string.
const fixtureMCP = `// compareDoors fails on steps two doors ended differently.
func compareDoors(t *testing.T, first, second string, one, two []step) {
	t.Helper()
	if len(one) != len(two) {
		t.Fatalf("the %s door ran %d steps and the %s door %d",
			first, len(one), second, len(two))
	}
	for i := range one {
		if one[i] != two[i] {
			t.Errorf("%s: %s door %+v, %s door %+v",
				one[i].Name, first, one[i], second, two[i])
		}
	}
}

// tool sends one MCP tools/call to /api/v1/mcp with token and answers
// its kind and its decoded result. The endpoint keeps no session, so
// no initialize comes first. It goes to a real server for the reason
// call does. A tool error answers its code, the API's, with the same
// envelope as the body; a success answers its structured content, and
// a delete, which has none, answers nil.
func tool(
	t *testing.T, h http.Handler, token, name string,
	arguments map[string]any,
) (string, map[string]any) {
	t.Helper()
	raw := toolPost(t, h, token, name, arguments)
	var reply struct {
		Result struct {
			IsError           bool
			StructuredContent map[string]any
			Content           []struct{ Text string }
		}
		Error map[string]any
	}
	decodeExact(t, name, raw, &reply)
	if reply.Error != nil || len(reply.Result.Content) == 0 {
		t.Fatalf("tools/call %s: no tool result: %s", name, raw)
	}
	if !reply.Result.IsError {
		return "ok", reply.Result.StructuredContent
	}
	var refusal map[string]any
	decodeExact(t, name, []byte(reply.Result.Content[0].Text), &refusal)
	failure, _ := refusal["error"].(map[string]any)
	switch code, _ := failure["code"].(string); code {
	case "invalid", "not_found":
		return code, refusal
	}
	t.Fatalf("tools/call %s: the MCP door refused: %s", name, raw)
	return "", nil
}

// toolPost is the HTTP half of tool: the JSON-RPC request in, the
// response body out.
func toolPost(
	t *testing.T, h http.Handler, token, name string,
	arguments map[string]any,
) []byte {
	t.Helper()
	request, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": arguments},
	})
	if err != nil {
		t.Fatalf("tools/call %s: %v", name, err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	req, err := http.NewRequest(http.MethodPost,
		server.URL+"/api/v1/mcp", bytes.NewReader(request))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = strings.TrimPrefix(base, "http://")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	client := *server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("tools/call %s: the MCP door answered %d: %s",
			name, res.StatusCode, raw)
	}
	return raw
}

// decodeExact decodes raw into into with numbers kept as written, as
// call does, so two ids past 2^53 stay different.
func decodeExact(t *testing.T, name string, raw []byte, into any) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(into); err != nil {
		t.Fatalf("tools/call %s: %v: %s", name, err, raw)
	}
}

// withID adds the row's id to a tool's arguments, as the number it is.
func withID(id string, arguments map[string]any) map[string]any {
	if arguments == nil {
		arguments = map[string]any{}
	}
	arguments["id"] = json.Number(id)
	return arguments
}

// toolRow is row through the MCP door: the get tool's answer without
// what differs between two apps by design.
func toolRow(
	t *testing.T, h http.Handler, token, name, id string,
) string {
	t.Helper()
	kind, record := tool(t, h, token, name, withID(id, nil))
	if kind != "ok" {
		t.Fatalf("tools/call %s %s: %s", name, id, kind)
	}
	return bare(t, record)
}

// bare is a row without its id and timestamps, as JSON.
func bare(t *testing.T, record map[string]any) string {
	t.Helper()
	delete(record, "id")
	delete(record, "created_at")
	delete(record, "updated_at")
	b, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// items is how many items a list answered under key.
func items(body map[string]any, key string) string {
	list, ok := body[key].([]any)
	if !ok {
		return "no " + key
	}
	return fmt.Sprintf("%d items", len(list))
}

`
