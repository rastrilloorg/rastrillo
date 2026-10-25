package wirename

import "testing"

// TestName covers what the wire rule adds to sqlName: an acronym stays
// one word, at the start, the end or the whole name. Limit, After and Q
// are here because Resource.Validate refuses a filter whose wire name is
// limit, after or q, and a wrong answer for them would let one through.
func TestName(t *testing.T) {
	cases := map[string]string{
		"Title":         "title",
		"IPAddress":     "ip_address",
		"MaxPerOrder":   "max_per_order",
		"URL":           "url",
		"HTTPServerURL": "http_server_url",
		"Limit":         "limit",
		"After":         "after",
		"Q":             "q",
		"":              "",
	}
	for in, want := range cases {
		if got := Name(in); got != want {
			t.Errorf("Name(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestNameAgreesWithSQLName copies TestSqlName's table from
// internal/generate/store_test.go. The wire name and the column name
// are made by two functions; this keeps them from drifting on the names
// the generator already tests.
func TestNameAgreesWithSQLName(t *testing.T) {
	cases := map[string]string{
		"Title":       "title",
		"Price":       "price",
		"Body":        "body",
		"MaxPerOrder": "max_per_order",
	}
	for in, want := range cases {
		if got := Name(in); got != want {
			t.Errorf("Name(%q) = %q, but sqlName gives %q", in, got, want)
		}
	}
}
