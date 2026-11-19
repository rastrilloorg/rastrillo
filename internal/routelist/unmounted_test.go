package routelist

import (
	"reflect"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/action"
)

func TestUnmountedNamesTheActionNoRouteMounts(t *testing.T) {
	target, _ := findIn(t, "app", "")
	routes, err := List(t.Context(), target)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	unmounted, err := Unmounted(t.Context(), target, routes)
	if err != nil {
		t.Fatalf("Unmounted failed: %v", err)
	}
	// (*Tags).Show is mounted and (*Tickets).Show is not; tagEdit is
	// not, though a Handle carries its name.
	want := []string{
		appPackage + ".(*App).tagEdit",
		appPackage + ".(*Tickets).Show",
	}
	if !reflect.DeepEqual(unmounted, want) {
		t.Errorf("Unmounted gave %q, want %q", unmounted, want)
	}
	report := FormatUnmounted(unmounted)
	for _, identity := range want {
		if !strings.Contains(report, "\t"+identity+"\n") {
			t.Errorf("the report does not name %s:\n%s", identity, report)
		}
	}
}

func TestFormatEndsWithTheAuthorizeNote(t *testing.T) {
	table := Format(appRoutes())
	lines := strings.Split(strings.TrimSuffix(table, "\n"), "\n")
	if last := lines[len(lines)-1]; last != tableNote {
		t.Errorf("the table ends %q, want the note %q", last, tableNote)
	}
	wanted := [][]string{
		{"GET", "/", "app.(*App).index"},
		{"GET", "/admin", "app.(*App).adminHome", "admin"},
		{"POST", "/admin/tags/{id}", "app.(*Tags).Show", "admin"},
		{"GET", "/report", "app.Report.Show"},
		{"GET", "/healthz", "tagEdit"},
		{"*", "/api/*", "api"},
	}
	if len(lines) < len(wanted)+1 {
		t.Fatalf("the table has %d lines:\n%s", len(lines), table)
	}
	for position, want := range wanted {
		if got := strings.Fields(lines[position]); !reflect.DeepEqual(
			got, want,
		) {
			t.Errorf("line %d is %q, want %q", position, got, want)
		}
	}
	if empty := Format([]action.Route{}); !strings.HasPrefix(
		empty, noRoutesRegistered,
	) {
		t.Errorf("an empty table is %q", empty)
	}
}
