package mcp

import (
	"net/http"
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
