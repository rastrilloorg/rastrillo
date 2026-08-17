package screens

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	rastrillo "github.com/carlosframework/rastrillo"
	"github.com/carlosframework/rastrillo/blobs"
	"github.com/carlosframework/rastrillo/eventlog"
)

func ticketTypes(store rastrillo.StoreKind) rastrillo.Resource {
	return rastrillo.Resource{
		Name:  "ticket_types",
		Route: "/admin/ticket_types",
		Store: store,
		List: rastrillo.List{
			Columns: []rastrillo.Column{
				{Field: "Name", Kind: rastrillo.Text},
				{Field: "Price", Kind: rastrillo.Money},
				{Field: "Status", Kind: rastrillo.Select},
			},
			Search: true,
			Filter: []string{"Status"},
		},
		Form: rastrillo.Form{
			Basics: []rastrillo.Field{
				{Name: "Name", Kind: rastrillo.Text, Required: true},
				{Name: "Price", Kind: rastrillo.Money},
				{Name: "Status", Kind: rastrillo.Select, Options: []string{"draft", "live"}},
			},
			Advanced: []rastrillo.Field{
				{Name: "MaxPerOrder", Kind: rastrillo.Money},
			},
		},
	}
}

type testScope struct{ deps Deps }

func (s testScope) ScreenDeps() Deps { return s.deps }

// harness wires the generated-action shape by hand: a mux whose
// handlers call the screens package exactly as emitted actions do.
func harness(t *testing.T, res rastrillo.Resource, deps Deps) *http.ServeMux {
	t.Helper()
	db, err := rastrillo.OpenDB(filepath.Join(t.TempDir(), "app.db"),
		append([]string{res.Migration()}, eventlog.Migrations...))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if res.Store == rastrillo.Mergeable {
		log, err := eventlog.Open(db, "edge-test")
		if err != nil {
			t.Fatal(err)
		}
		deps.Events = log
	}

	ctx := &rastrillo.Ctx{DB: db, Actor: rastrillo.Actor{Human: true}, Scope: testScope{deps}}
	mux := http.NewServeMux()
	h := func(f func(*rastrillo.Ctx, http.ResponseWriter, *http.Request, rastrillo.Resource)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { f(ctx, w, r, res) }
	}
	base := res.Route
	mux.HandleFunc("GET "+base, h(List))
	mux.HandleFunc("GET "+base+"/new", h(NewForm))
	mux.HandleFunc("POST "+base, h(Create))
	mux.HandleFunc("GET "+base+"/{id}", h(Show))
	mux.HandleFunc("GET "+base+"/{id}/edit", h(EditForm))
	mux.HandleFunc("POST "+base+"/{id}", h(Save))
	mux.HandleFunc("POST "+base+"/{id}/edit-basics", h(SaveBasics))
	mux.HandleFunc("POST "+base+"/{id}/edit-advanced", h(SaveAdvanced))
	mux.HandleFunc("GET "+base+"/{id}/delete", h(ConfirmDelete))
	mux.HandleFunc("POST "+base+"/{id}/delete", h(Delete))
	return mux
}

func do(t *testing.T, mux *http.ServeMux, method, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if form != nil {
		r = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// firstShowURL pulls the first row link out of a rendered list.
func firstShowURL(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, `<td><a href="`)
	if i < 0 {
		t.Fatalf("no row link in list:\n%s", body)
	}
	rest := body[i+len(`<td><a href="`):]
	return rest[:strings.Index(rest, `"`)]
}

func TestScreensCRUD(t *testing.T) {
	for _, store := range []rastrillo.StoreKind{rastrillo.Exclusive, rastrillo.Mergeable} {
		name := "exclusive"
		if store == rastrillo.Mergeable {
			name = "mergeable"
		}
		t.Run(name, func(t *testing.T) {
			mux := harness(t, ticketTypes(store), Deps{})

			// Blank state.
			w := do(t, mux, "GET", "/admin/ticket_types", nil)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "No ticket types yet.") {
				t.Fatalf("blank list: %d\n%s", w.Code, w.Body.String())
			}

			// New form renders basics only.
			w = do(t, mux, "GET", "/admin/ticket_types/new", nil)
			if !strings.Contains(w.Body.String(), `name="name"`) || strings.Contains(w.Body.String(), `name="max_per_order"`) {
				t.Fatalf("new form should carry basics only:\n%s", w.Body.String())
			}

			// Create.
			w = do(t, mux, "POST", "/admin/ticket_types", url.Values{
				"name": {"Early bird"}, "price": {"25.00"}, "status": {"draft"},
			})
			if w.Code != http.StatusSeeOther {
				t.Fatalf("create: %d\n%s", w.Code, w.Body.String())
			}

			// Required field enforced, with the form re-rendered.
			w = do(t, mux, "POST", "/admin/ticket_types", url.Values{"name": {""}})
			if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "Name: is required") {
				t.Fatalf("required: %d\n%s", w.Code, w.Body.String())
			}

			// Money rejects floats-by-hand garbage.
			w = do(t, mux, "POST", "/admin/ticket_types", url.Values{"name": {"X"}, "price": {"12.345"}})
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("bad money accepted: %d", w.Code)
			}

			// Select rejects undeclared values.
			w = do(t, mux, "POST", "/admin/ticket_types", url.Values{"name": {"X"}, "status": {"sneaky"}})
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("bad select accepted: %d", w.Code)
			}

			// List shows the row, money formatted, cell linked.
			w = do(t, mux, "GET", "/admin/ticket_types", nil)
			body := w.Body.String()
			if !strings.Contains(body, "Early bird") || !strings.Contains(body, "25.00") {
				t.Fatalf("list:\n%s", body)
			}
			showURL := firstShowURL(t, body)

			// Show.
			w = do(t, mux, "GET", showURL, nil)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "Early bird") {
				t.Fatalf("show %s: %d", showURL, w.Code)
			}

			// Edit renders two sections with two actions (§3 invariant).
			w = do(t, mux, "GET", showURL+"/edit", nil)
			body = w.Body.String()
			if !strings.Contains(body, "/edit-basics") || !strings.Contains(body, "/edit-advanced") {
				t.Fatalf("edit forms:\n%s", body)
			}

			// A basics save cannot clobber an advanced field: first set
			// the advanced field, then save basics, then check it held.
			w = do(t, mux, "POST", showURL+"/edit-advanced", url.Values{"max_per_order": {"4.00"}})
			if w.Code != http.StatusSeeOther {
				t.Fatalf("advanced save: %d\n%s", w.Code, w.Body.String())
			}
			w = do(t, mux, "POST", showURL+"/edit-basics", url.Values{
				"name": {"Late bird"}, "price": {"40.00"}, "status": {"live"},
			})
			if w.Code != http.StatusSeeOther {
				t.Fatalf("basics save: %d\n%s", w.Code, w.Body.String())
			}
			w = do(t, mux, "GET", showURL, nil)
			body = w.Body.String()
			if !strings.Contains(body, "Late bird") || !strings.Contains(body, "4.00") {
				t.Fatalf("after both saves, advanced must survive the basics save:\n%s", body)
			}

			// Search and filter as GET round trips.
			w = do(t, mux, "GET", "/admin/ticket_types?q=late", nil)
			if !strings.Contains(w.Body.String(), "Late bird") {
				t.Fatal("search missed the row")
			}
			w = do(t, mux, "GET", "/admin/ticket_types?q=zzz", nil)
			if strings.Contains(w.Body.String(), "Late bird") {
				t.Fatal("search matched what it should not")
			}
			w = do(t, mux, "GET", "/admin/ticket_types?status=draft", nil)
			if strings.Contains(w.Body.String(), "Late bird") {
				t.Fatal("filter status=draft should exclude a live row")
			}

			// Delete: confirm page first, then the POST, then gone.
			w = do(t, mux, "GET", showURL+"/delete", nil)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "cannot be undone") {
				t.Fatalf("confirm page: %d\n%s", w.Code, w.Body.String())
			}
			w = do(t, mux, "POST", showURL+"/delete", nil)
			if w.Code != http.StatusSeeOther {
				t.Fatalf("delete: %d", w.Code)
			}
			w = do(t, mux, "GET", showURL, nil)
			if w.Code != http.StatusNotFound {
				t.Fatalf("after delete: %d, want 404", w.Code)
			}

			// Unknown ids are 404s, not explanations.
			if w := do(t, mux, "GET", "/admin/ticket_types/999999/edit", nil); w.Code != http.StatusNotFound {
				t.Fatalf("unknown id: %d", w.Code)
			}
		})
	}
}

func TestCrossSiteWriteRefused(t *testing.T) {
	mux := harness(t, ticketTypes(rastrillo.Exclusive), Deps{})
	r := httptest.NewRequest("POST", "/admin/ticket_types", strings.NewReader("name=X"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-site create: %d, want 403", w.Code)
	}
}

func TestMergeableDeleteIsATombstone(t *testing.T) {
	res := ticketTypes(rastrillo.Mergeable)
	db, err := rastrillo.OpenDB(filepath.Join(t.TempDir(), "m.db"), eventlog.Migrations)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	log, _ := eventlog.Open(db, "edge-test")
	ctx := &rastrillo.Ctx{DB: db, Actor: rastrillo.Actor{Human: true}, Scope: testScope{Deps{Events: log}}}

	eng, err := engineFor(ctx, Deps{Events: log}, res)
	if err != nil {
		t.Fatal(err)
	}
	id, err := eng.create(map[string]any{"Name": "gone soon"}, "human")
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.delete(id, "agent:reaper"); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.get(id); err != errRowNotFound {
		t.Fatalf("deleted row still derivable: %v", err)
	}
	// The history keeps the fact: three events, the last a tombstone
	// attributed to the agent.
	events, err := log.Events(t.Context(), "resource/ticket_types")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want created + deleted", len(events))
	}
	last := events[len(events)-1]
	if last.Kind != "deleted" || last.Actor != "agent:reaper" {
		t.Fatalf("tombstone = %s by %s", last.Kind, last.Actor)
	}
}

func TestBlobFieldStoresRefAndKeepsOnEmptySave(t *testing.T) {
	res := rastrillo.Resource{
		Name:  "docs",
		Route: "/admin/docs",
		Store: rastrillo.Exclusive,
		List:  rastrillo.List{Columns: []rastrillo.Column{{Field: "Title", Kind: rastrillo.Text}}},
		Form: rastrillo.Form{Basics: []rastrillo.Field{
			{Name: "Title", Kind: rastrillo.Text, Required: true},
			{Name: "File", Kind: rastrillo.Blob},
		}},
	}
	store, err := blobs.Dir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mux := harness(t, res, Deps{Blobs: store})

	// Multipart create with a file.
	var b bytes.Buffer
	wtr := multipart.NewWriter(&b)
	wtr.WriteField("title", "Contract")
	fw, _ := wtr.CreateFormFile("file", "contract.txt")
	fw.Write([]byte("very important bytes"))
	wtr.Close()

	r := httptest.NewRequest("POST", "/admin/docs", &b)
	r.Header.Set("Content-Type", wtr.FormDataContentType())
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("create with blob: %d\n%s", w.Code, w.Body.String())
	}

	// The show page displays the ref, not the bytes.
	list := do(t, mux, "GET", "/admin/docs", nil)
	i := strings.Index(list.Body.String(), `<td><a href="`)
	if i < 0 {
		t.Fatalf("no row link:\n%s", list.Body.String())
	}
	rest := list.Body.String()[i+len(`<td><a href="`):]
	showURL := rest[:strings.Index(rest, `"`)]
	show := do(t, mux, "GET", showURL, nil)
	if !strings.Contains(show.Body.String(), "bytes)") {
		t.Fatalf("show should describe the blob ref:\n%s", show.Body.String())
	}

	// A save without a new file keeps the existing blob.
	if w := do(t, mux, "POST", showURL, url.Values{"title": {"Contract v2"}}); w.Code != http.StatusSeeOther {
		t.Fatalf("save without file: %d\n%s", w.Code, w.Body.String())
	}
	show = do(t, mux, "GET", showURL, nil)
	if !strings.Contains(show.Body.String(), "Contract v2") || !strings.Contains(show.Body.String(), "bytes)") {
		t.Fatalf("save without a new file must keep the existing blob:\n%s", show.Body.String())
	}
}
