// announcements_test.go drives the second manifest resource — the
// mergeable one (manifest/announcements.toml, store = "mergeable") —
// through the same fully generated screens the exclusive resource
// gets: identical actions, templates, locales and router, with the
// eventlog-backed store underneath. The tombstone test is the proof
// this is an event store, not a DELETE: after the delete flow the
// record is gone from every screen, yet the eventlog still holds the
// stream's whole created/updated/deleted history.
package ticketstest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/eventlog"

	announcementsstore "tickets/gen/store/announcements"
)

// TestAnnouncementsFourStateRoundTrip walks create → 303 → show →
// edit-basics save UNCHANGED → 303 → list through the generated
// mergeable resource — mirroring TestFourStateRoundTrip so the two
// store shapes are held to the same observable screens.
func TestAnnouncementsFourStateRoundTrip(t *testing.T) {
	app, db := newApp(t)

	create := post(t, app, "/admin/announcements", url.Values{
		"Title": {"Doors open at noon"},
		"Body":  {"Gates on the east side."},
	})
	wantStatus(t, create, http.StatusSeeOther)
	loc := create.Header().Get("Location")
	if !strings.HasPrefix(loc, "/admin/announcements/") {
		t.Fatalf("Location = %q, want /admin/announcements/<id>", loc)
	}

	show := get(t, app, loc)
	wantStatus(t, show, http.StatusOK)
	showBody := show.Body.String()
	wantContains(t, showBody, "Doors open at noon")
	wantContains(t, showBody, "Gates on the east side.")

	// Edit form: current values seed the inputs.
	edit := get(t, app, loc+"/edit")
	wantStatus(t, edit, http.StatusOK)
	editBody := edit.Body.String()
	wantContains(t, editBody, `value="Doors open at noon"`)
	wantContains(t, editBody, "Gates on the east side.")
	wantContains(t, editBody, fmt.Sprintf(`action="%s/edit-basics"`, loc))

	// Edit-basics save UNCHANGED: resubmit exactly what the form seeded.
	save := post(t, app, loc+"/edit-basics", url.Values{
		"Title": {"Doors open at noon"},
		"Body":  {"Gates on the east side."},
	})
	wantStatus(t, save, http.StatusSeeOther)
	if got := save.Header().Get("Location"); got != loc {
		t.Errorf("Location = %q, want %q", got, loc)
	}

	id := announcementIDFromLocation(t, loc)
	row, err := announcementsstore.New(db).GetAnnouncement(context.Background(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if row.Title != "Doors open at noon" || row.Body != "Gates on the east side." {
		t.Errorf("row = %+v, want Title/Body unchanged after an unchanged save", row)
	}

	list := get(t, app, "/admin/announcements")
	wantStatus(t, list, http.StatusOK)
	wantContains(t, list.Body.String(), "Doors open at noon")
}

// TestAnnouncementsDeleteIsATombstone: after the generated delete flow
// the record is gone from the list AND the eventlog still holds the
// stream's created/updated/deleted events — an appended tombstone,
// never a DELETE.
func TestAnnouncementsDeleteIsATombstone(t *testing.T) {
	app, db := newApp(t)

	create := post(t, app, "/admin/announcements", url.Values{
		"Title": {"Doomed notice"},
		"Body":  {"Soon gone, never erased."},
	})
	wantStatus(t, create, http.StatusSeeOther)
	loc := create.Header().Get("Location")
	id := announcementIDFromLocation(t, loc)

	save := post(t, app, loc+"/edit-basics", url.Values{
		"Title": {"Doomed notice"},
		"Body":  {"Edited once first."},
	})
	wantStatus(t, save, http.StatusSeeOther)

	// The confirm-page flow, then the delete itself.
	confirm := get(t, app, loc+"/delete")
	wantStatus(t, confirm, http.StatusOK)
	wantContains(t, confirm.Body.String(), "Doomed notice")

	del := post(t, app, loc+"/delete", url.Values{})
	wantStatus(t, del, http.StatusSeeOther)
	if got := del.Header().Get("Location"); got != "/admin/announcements" {
		t.Errorf("Location = %q, want the list", got)
	}

	// Gone from every read: store, show, list, repeat delete.
	if _, err := announcementsstore.New(db).GetAnnouncement(context.Background(), id); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("record survived the delete: %v", err)
	}
	if w := get(t, app, loc); w.Code != http.StatusNotFound {
		t.Errorf("GET %s after delete = %d, want 404", loc, w.Code)
	}
	wantNotContains(t, get(t, app, "/admin/announcements").Body.String(), "Doomed notice")
	if w := post(t, app, loc+"/delete", url.Values{}); w.Code != http.StatusNotFound {
		t.Errorf("repeat delete = %d, want 404", w.Code)
	}

	// …and yet the history is all still there: created, updated,
	// deleted, in order, on the record's own stream.
	writer, err := eventlog.LocalWriter(context.Background(), db)
	if err != nil {
		t.Fatalf("LocalWriter: %v", err)
	}
	log, err := eventlog.Open(db, writer)
	if err != nil {
		t.Fatalf("eventlog.Open: %v", err)
	}
	events, err := log.EventsByPrefix(context.Background(), "announcements/")
	if err != nil {
		t.Fatalf("EventsByPrefix: %v", err)
	}
	var kinds []string
	stream := fmt.Sprintf("announcements/%d", id)
	for _, ev := range events {
		if ev.Stream == stream {
			kinds = append(kinds, ev.Kind)
		}
	}
	want := []string{"created", "updated", "deleted"}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("stream %s kinds = %v, want %v (tombstone, not DELETE)", stream, kinds, want)
	}
}

// TestAnnouncementsListDeriveIsDeterministic is the two-derive probe:
// the same history derives to byte-identical output every time — at
// the store level and through the rendered list screen.
func TestAnnouncementsListDeriveIsDeterministic(t *testing.T) {
	app, db := newApp(t)

	for i := 1; i <= 3; i++ {
		w := post(t, app, "/admin/announcements", url.Values{
			"Title": {fmt.Sprintf("Notice %d", i)},
			"Body":  {fmt.Sprintf("Body %d", i)},
		})
		wantStatus(t, w, http.StatusSeeOther)
	}
	// A delete in the history too, so the probe covers tombstone
	// skipping.
	del := post(t, app, "/admin/announcements/2/delete", url.Values{})
	wantStatus(t, del, http.StatusSeeOther)

	store := announcementsstore.New(db)
	params := announcementsstore.ListAnnouncementsParams{PageOffset: 0, PageLimit: 10}
	first, err := store.ListAnnouncements(context.Background(), params)
	if err != nil {
		t.Fatalf("ListAnnouncements (1st): %v", err)
	}
	second, err := store.ListAnnouncements(context.Background(), params)
	if err != nil {
		t.Fatalf("ListAnnouncements (2nd): %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("two derives differ:\n%+v\n%+v", first, second)
	}
	if len(first) != 2 {
		t.Fatalf("derived %d live rows, want 2 (one tombstoned)", len(first))
	}

	pageA := get(t, app, "/admin/announcements")
	pageB := get(t, app, "/admin/announcements")
	wantStatus(t, pageA, http.StatusOK)
	if pageA.Body.String() != pageB.Body.String() {
		t.Fatal("two renders of the derived list differ byte-for-byte")
	}
}

func announcementIDFromLocation(t *testing.T, loc string) int64 {
	t.Helper()
	var id int64
	if _, err := fmt.Sscanf(strings.TrimPrefix(loc, "/admin/announcements/"), "%d", &id); err != nil {
		t.Fatalf("parse id from %q: %v", loc, err)
	}
	return id
}
