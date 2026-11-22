package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"
)

func TestServedListsEveryDescriptorNewTook(t *testing.T) {
	handler, err := New(testConfig(), bookmarks()...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	served, err := Served(handler)
	if err != nil {
		t.Fatalf("Served: %v", err)
	}
	want := []string{"bookmarks.create", "bookmarks.list", "bookmarks.get",
		"bookmarks.update", "bookmarks.delete"}
	if len(served) != len(want) {
		t.Fatalf("Served = %d descriptors, want %v", len(served), want)
	}
	for index, descriptor := range served {
		if descriptor.ID != want[index] {
			t.Errorf("descriptor %d = %s, want %s", index, descriptor.ID,
				want[index])
		}
	}
	if _, err := Served(http.NotFoundHandler()); err == nil {
		t.Error("Served of another handler = nil error, want an error")
	}
}

// scribble overwrites schema in place, as an app reusing its buffer
// would.
func scribble(schema []byte) {
	for index := range schema {
		schema[index] = 'x'
	}
}

// TestServedIsASnapshotOfWhatMCPServes: neither the bytes New was given
// nor the bytes Served returns reach what the endpoint serves or what
// the next Served reports.
func TestServedIsASnapshotOfWhatMCPServes(t *testing.T) {
	descriptors := bookmarks()
	input := slices.Clone(descriptors[0].Input)
	result := slices.Clone(descriptors[0].Result)
	handler, err := New(testConfig(), descriptors...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	scribble(descriptors[0].Input)
	scribble(descriptors[0].Result)
	for round := range 2 {
		served, err := Served(handler)
		if err != nil {
			t.Fatalf("Served: %v", err)
		}
		if string(served[0].Input) != string(input) ||
			string(served[0].Result) != string(result) {
			t.Errorf("Served, round %d = %s and %s, want %s and %s",
				round, served[0].Input, served[0].Result, input, result)
		}
		scribble(served[0].Input)
		scribble(served[0].Result)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	session, _ := connect(t, server.URL, writerToken, "2025-06-18")
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	for _, tool := range listed.Tools {
		if tool.Name != ToolName(descriptors[0].ID) {
			continue
		}
		listedInput, _ := json.Marshal(tool.InputSchema)
		var got, want any
		_ = json.Unmarshal(listedInput, &got)
		_ = json.Unmarshal(input, &want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("tools/list input schema = %s, want %s",
				listedInput, input)
		}
	}
}
