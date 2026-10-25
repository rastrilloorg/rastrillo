package op

import "testing"

func TestCursorRoundTrip(t *testing.T) {
	c := EncodeCursor("2026-10-05T18:00:00Z", 42)
	if at, id, ok := DecodeCursor(c); !ok || at != "2026-10-05T18:00:00Z" || id != 42 {
		t.Fatalf("DecodeCursor(%q) = %q, %d, %v", c, at, id, ok)
	}
	if at, id, ok := DecodeCursor(""); !ok || at != "" || id != 0 {
		t.Fatal(`"" must be the first page`)
	}
	for _, bad := range []string{"!!", EncodeCursor("yesterday", 1), EncodeCursor("2026-10-05T18:00:00Z", 0), "MjAyNi0xMC0wNVQxODowMDowMFo"} {
		if _, _, ok := DecodeCursor(bad); ok {
			t.Errorf("DecodeCursor(%q) accepted a cursor EncodeCursor could not have made", bad)
		}
	}
}
