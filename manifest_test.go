package rastrillo

import (
	"strings"
	"testing"
)

func validResource() Resource {
	return Resource{
		Name:     "notes",
		Route:    "/admin/notes",
		Store:    Exclusive,
		Currency: "USD",
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

func TestValidateAcceptsUserScope(t *testing.T) {
	r := validResource()
	r.Scope = UserScoped
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// TestValidateAcceptsMergeable: store = "mergeable" is real now (the
// 2026-08-22 mergeable-manifests spec) — the vocabulary was mergeable-
// ready from day one, and the refusal is gone.
func TestValidateAcceptsMergeable(t *testing.T) {
	r := validResource()
	r.Store = Mergeable
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if r.Store != Mergeable {
		t.Fatalf("Store = %q after Validate, want %q", r.Store, Mergeable)
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
		{"unknown scope", func(r *Resource) { r.Scope = "team" }, "scope"},
		{"money without currency", func(r *Resource) { r.Currency = "" }, "no currency"},
		// Owner is reserved even UNSCOPED (see isReservedColumnName's
		// doc: flipping scope = "user" on later must never invalidate a
		// manifest that was valid before), so this case deliberately
		// leaves r.Scope at its zero value.
		{"reserved field name Owner", func(r *Resource) { r.Form.Basics[0].Name = "Owner" }, "reserved"},
		// Astra's finding 4: a money field Price becomes price_minor and
		// price_currency (PriceMinor and PriceCurrency in Go), so a field
		// declared with either name would be emitted twice.
		{"text field named a money field's currency", func(r *Resource) {
			r.Form.Basics = append(r.Form.Basics, Field{Name: "PriceCurrency"})
		}, `"PriceCurrency" is also a name the money field "Price" generates`},
		{"column named a money field's amount", func(r *Resource) {
			r.List.Columns = append(r.List.Columns, Column{Field: "PriceMinor"})
		}, `"PriceMinor" is also a name the money field "Price" generates`},
		// EventVersion is a mergeable store's event-version member. It is
		// refused for an exclusive store too, as Owner is, so changing
		// store kind later never invalidates a valid manifest.
		// Astra's round-two finding 1: a mergeable event carries a money
		// field's amount under its own key and its currency under
		// <key>_currency, so a second money field PriceCurrency would
		// write price_currency twice.
		{"two money fields sharing an event key", func(r *Resource) {
			r.Form.Basics = append(r.Form.Basics, Field{Name: "PriceCurrency", Kind: Money})
		}, `"PriceCurrency" is also a name the money field "Price" generates`},
		{"field named EventVersion", func(r *Resource) { r.Form.Basics[1].Name = "EventVersion" }, `"EventVersion" is the name generated stores give the event version`},
		// Astra's finding 3: a filter compares one value from the URL, and
		// an amount has no single value without its currency.
		{"filter on a money column", func(r *Resource) { r.List.Filter = []string{"Price"} }, `filter: "Price" is a money field`},
		{"filters entry on a money column", func(r *Resource) {
			r.List.Filters = []Filter{{Field: "Price", Values: []string{"cheap"}}}
		}, `filter: "Price" is a money field`},
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
		Name:     "notes",
		Route:    "/admin/notes",
		Store:    Exclusive,
		Currency: "USD",
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

func scale(n int) *int { return &n }

// TestValidateMoneyCurrency pins the default currency and [legacy]. The
// wanted substrings are Appendix C.2's approved text without its
// leading "manifest/%s.toml", which internal/manifest.Load supplies.
func TestValidateMoneyCurrency(t *testing.T) {
	base := func() Resource {
		return Resource{Name: "ticket_types", Route: "/t", Form: Form{Basics: []Field{{Name: "Price", Kind: Money}}}}
	}
	for name, c := range map[string]struct {
		mutate func(*Resource)
		err    string
	}{
		"missing currency":        {func(r *Resource) {}, `has a money field but no currency; add currency = "EUR" (or another ISO 4217 code), the default for new records`},
		"malformed currency":      {func(r *Resource) { r.Currency = "EURO" }, `currency "EURO": currency code is not three ASCII letters`},
		"unsupported currency":    {func(r *Resource) { r.Currency = "ITL" }, `currency "ITL": currency code is not in the storage set`},
		"eligible":                {func(r *Resource) { r.Currency = " eur " }, ""},
		"stored-only default":     {func(r *Resource) { r.Currency = "SLL" }, ""}, // generates with a warning
		"legacy ok":               {func(r *Resource) { r.Currency = "JPY"; r.Legacy = &Legacy{Currency: "usd", Scale: scale(2)} }, ""},
		"legacy SLL":              {func(r *Resource) { r.Currency = "EUR"; r.Legacy = &Legacy{Currency: "SLL", Scale: scale(2)} }, ""},
		"legacy scale 0":          {func(r *Resource) { r.Currency = "EUR"; r.Legacy = &Legacy{Currency: "JPY", Scale: scale(0)} }, ""},
		"legacy scale 18":         {func(r *Resource) { r.Currency = "EUR"; r.Legacy = &Legacy{Currency: "USD", Scale: scale(18)} }, ""},
		"legacy scale omitted":    {func(r *Resource) { r.Currency = "EUR"; r.Legacy = &Legacy{Currency: "USD"} }, "[legacy] needs scale, the number of decimal places the old integers carry (2 for cents, 0 for whole units)"},
		"legacy currency omitted": {func(r *Resource) { r.Currency = "EUR"; r.Legacy = &Legacy{Scale: scale(2)} }, "[legacy] needs currency, the currency the old integers were in"},
		"legacy bad code":         {func(r *Resource) { r.Currency = "EUR"; r.Legacy = &Legacy{Currency: "DEM", Scale: scale(2)} }, `[legacy] currency "DEM": currency code is not in the storage set`},
		"legacy scale -1":         {func(r *Resource) { r.Currency = "EUR"; r.Legacy = &Legacy{Currency: "USD", Scale: scale(-1)} }, "[legacy] scale -1 must be from 0 to 18"},
		"legacy scale 19":         {func(r *Resource) { r.Currency = "EUR"; r.Legacy = &Legacy{Currency: "USD", Scale: scale(19)} }, "[legacy] scale 19 must be from 0 to 18"},
	} {
		r := base()
		c.mutate(&r)
		err := r.Validate()
		if c.err == "" && err != nil || c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)) {
			t.Errorf("%s: %v, want %q", name, err, c.err)
		}
		// Load prefixes the file; a message that already named it would
		// print the path twice.
		if err != nil && strings.Contains(err.Error(), "manifest/") {
			t.Errorf("%s: %q names a manifest path; Load adds it", name, err)
		}
	}
	r := base()
	r.Currency = " eur "
	r.Legacy = &Legacy{Currency: " usd", Scale: scale(2)}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	if r.Currency != "EUR" || r.Legacy.Currency != "USD" {
		t.Errorf("Validate left Currency %q and [legacy] currency %q; emitters must see the canonical codes", r.Currency, r.Legacy.Currency)
	}
}

// TestHasMoney: a money column alone counts, as does a money field in
// either form section; it decides whether a currency is required.
func TestHasMoney(t *testing.T) {
	for name, c := range map[string]struct {
		r    Resource
		want bool
	}{
		"none":     {Resource{List: List{Columns: []Column{{Field: "Title"}}}, Form: Form{Basics: []Field{{Name: "Title"}}}}, false},
		"column":   {Resource{List: List{Columns: []Column{{Field: "Price", Kind: Money}}}}, true},
		"basics":   {Resource{Form: Form{Basics: []Field{{Name: "Price", Kind: Money}}}}, true},
		"advanced": {Resource{Form: Form{Advanced: []Field{{Name: "Price", Kind: Money}}}}, true},
	} {
		if got := c.r.HasMoney(); got != c.want {
			t.Errorf("%s: HasMoney = %v, want %v", name, got, c.want)
		}
	}
}
