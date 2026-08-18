package rastrillo

import (
	"strings"
	"testing"
)

func validResource() Resource {
	return Resource{
		Name:  "notes",
		Route: "/admin/notes",
		Store: Exclusive,
		List: List{
			Columns: []Column{{Field: "Title"}, {Field: "Price", Kind: Money}},
			Search:  true,
			Filter:  []string{"Title"},
		},
		Form: Form{
			Basics:   []Field{{Name: "Title"}, {Name: "Body", Kind: Textarea}},
			Advanced: []Field{{Name: "Price", Kind: Money}},
		},
	}
}

func TestValidateAcceptsTheFixture(t *testing.T) {
	r := validResource()
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRejections(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Resource)
		want string // substring of the error
	}{
		{"empty name", func(r *Resource) { r.Name = "" }, "name"},
		{"non-snake name", func(r *Resource) { r.Name = "TicketTypes" }, "snake_case"},
		{"empty route", func(r *Resource) { r.Route = "" }, "route"},
		{"trailing slash", func(r *Resource) { r.Route = "/admin/notes/" }, "trailing"},
		{"no leading slash", func(r *Resource) { r.Route = "admin/notes" }, "route"},
		{"mergeable", func(r *Resource) { r.Store = Mergeable }, "not yet built"},
		{"unknown store", func(r *Resource) { r.Store = "weird" }, "store"},
		{"unknown kind", func(r *Resource) { r.List.Columns[0].Kind = "meter" }, "kind"},
		{"filter not a column", func(r *Resource) { r.List.Filter = []string{"Status"} }, "filter"},
		{"nothing declared", func(r *Resource) { r.List = List{}; r.Form = Form{} }, "at least one"},
		{"duplicate field", func(r *Resource) { r.Form.Basics = append(r.Form.Basics, Field{Name: "Title"}) }, "duplicate"},
		{"non-identifier field", func(r *Resource) { r.Form.Basics[0].Name = "my-field" }, "identifier"},
		{"non-canonical field lowercase", func(r *Resource) { r.Form.Basics[0].Name = "title" }, "canonical"},
		{"non-canonical field consecutive capitals", func(r *Resource) { r.Form.Basics[0].Name = "IPAddress" }, "canonical"},
		// Columns[1] ("Price"), not Columns[0] ("Title") — Columns[0] is
		// the declared Filter target, and mutating its spelling would
		// trip the earlier "filter: not a declared column" check first
		// (Filter's ["Title"] lookup is an exact-string map hit against
		// columnFields, so lowercasing the column out from under it
		// masks the canonical check this case means to exercise).
		{"non-canonical column", func(r *Resource) { r.List.Columns[1].Field = "price" }, "canonical"},
		{"reserved column name Id", func(r *Resource) { r.List.Columns[1].Field = "Id" }, "reserved"},
		{"reserved field name CreatedAt", func(r *Resource) { r.Form.Basics[0].Name = "CreatedAt" }, "reserved"},
		{"reserved field name is case-insensitive", func(r *Resource) { r.Form.Advanced[0].Name = "updatedat" }, "reserved"},
		{"two filters", func(r *Resource) {
			r.List.Filters = []Filter{{Field: "Title", Values: []string{"a"}}, {Field: "Price", Values: []string{"b"}}}
		}, "one filter"},
		{"filter field not a column", func(r *Resource) {
			r.List.Filters = []Filter{{Field: "Nope", Values: []string{"a"}}}
		}, "column"},
		{"filter no values", func(r *Resource) {
			r.List.Filters = []Filter{{Field: "Title", Values: nil}}
		}, "value"},
		{"filter bad value", func(r *Resource) {
			r.List.Filters = []Filter{{Field: "Title", Values: []string{"On Sale"}}}
		}, "value"},
		{"filter duplicate value", func(r *Resource) {
			r.List.Filters = []Filter{{Field: "Title", Values: []string{"a", "a"}}}
		}, "value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := validResource()
			tc.mut(&r)
			err := r.Validate()
			if err == nil {
				t.Fatal("Validate accepted it")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q missing %q", err, tc.want)
			}
		})
	}
}

func TestValidateAcceptsFiltersAndRequired(t *testing.T) {
	r := Resource{
		Name:  "notes",
		Route: "/admin/notes",
		Store: Exclusive,
		List: List{
			Columns: []Column{{Field: "Title"}, {Field: "Price", Kind: Money}},
			Search:  true,
			Filter:  []string{"Title"},
			Filters: []Filter{{Field: "Title", Values: []string{"a", "b"}}},
		},
		Form: Form{
			Basics:   []Field{{Name: "Title", Required: true}, {Name: "Body", Kind: Textarea}},
			Advanced: []Field{{Name: "Price", Kind: Money}},
		},
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// TestCanonicalIdent pins identSQLName/identPascalCase/canonicalIdent's
// exact behavior — this is the mechanical, non-guessing check
// Validate now runs (see canonicalIdent's doc): it computes the one
// spelling sqlc's own column-name derivation would produce, it does
// not try to intelligently reformat a name into "the word boundaries
// the author probably meant". "IPAddress" is the case that matters
// most here: consecutive capital letters collapse under identSQLName
// (no underscore is inserted between two adjacent uppercase letters,
// only at a lower-to-upper transition), so its canonical form is
// "Ipaddress", not the more human "IpAddress" — that "IpAddress"
// spelling is itself already canonical (round-trips to itself) and is
// the one Validate's rejection message would actually have to suggest
// spelling it as if a caller wanted the word boundary preserved.
func TestCanonicalIdent(t *testing.T) {
	cases := []struct {
		name          string
		wantCanonical string
		wantSelf      bool
	}{
		{"Title", "Title", true},
		{"Body", "Body", true},
		{"MaxPerOrder", "MaxPerOrder", true},
		{"IpAddress", "IpAddress", true},
		{"title", "Title", false},
		{"IPAddress", "Ipaddress", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := canonicalIdent(tc.name)
			if got != tc.wantCanonical {
				t.Errorf("canonicalIdent(%q) = %q, want %q", tc.name, got, tc.wantCanonical)
			}
			if isCanonicalIdent(tc.name) != tc.wantSelf {
				t.Errorf("isCanonicalIdent(%q) = %v, want %v", tc.name, isCanonicalIdent(tc.name), tc.wantSelf)
			}
		})
	}
}
