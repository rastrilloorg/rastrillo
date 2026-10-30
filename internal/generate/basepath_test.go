package generate

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/manifest"
)

// emitTicketTypes generates examples/tickets' ticket_types into a temp
// dir and returns every file it wrote, keyed by its path under gen.
func emitTicketTypes(t *testing.T) map[string]string {
	t.Helper()
	rs, err := manifest.Load("../../examples/tickets", "../../examples/tickets/manifest")
	if err != nil {
		t.Fatal(err)
	}
	root, gen := newActionsAppRoot(t), t.TempDir()
	out := map[string]string{}
	for _, r := range rs {
		if r.Name != "ticket_types" {
			continue
		}
		tw, _, err := EmitTemplates(root, gen, r)
		if err != nil {
			t.Fatal(err)
		}
		aw, _, err := EmitActions(root, gen, r)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range append(tw, aw...) {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			rel, _ := filepath.Rel(gen, p)
			out[filepath.ToSlash(rel)] = string(b)
		}
	}
	return out
}

// A screen under /acme that writes "/admin/ticket_types" links to the
// hub, so every route a template writes goes through path.
func TestGeneratedTemplatesLinkThroughBasePath(t *testing.T) {
	bare := regexp.MustCompile(`[^$] "/admin/ticket_types[^"]*"`)
	files := emitTicketTypes(t)
	for name, src := range files {
		if strings.HasSuffix(name, ".html") {
			for _, m := range bare.FindAllString(src, -1) {
				t.Errorf("%s writes a route the base path never reaches: %s", name, m)
			}
		}
	}
	if !strings.Contains(files["templates/ticket_types/list.html"], `(path $ "/admin/ticket_types/new")`) {
		t.Errorf("list.html has no (path $ \"/admin/ticket_types/new\"):\n%s", files["templates/ticket_types/list.html"])
	}
}

// Body links the actions build go through basepath.Path; a redirect
// does not, because the middleware prefixes its Location and a second
// prefix would send the reader to /acme/acme/….
func TestGeneratedActionsPrefixBodyLinksAndNotRedirectsBasePath(t *testing.T) {
	field := regexp.MustCompile(`\b\w*(Href|Action): `)
	files := emitTicketTypes(t)
	const list, create = "actions/admin/ticket_types/index_get/index.GET.go", "actions/admin/ticket_types/index_post/index.POST.go"
	for _, want := range []string{`basepath.Path(r, fmt.Sprintf("/admin/ticket_types/%d"`, `basepath.Path(r, href(`, `basepath.Path(r, filterStatusHref(`} {
		if !strings.Contains(files[list], want) {
			t.Errorf("index.GET.go has no %s", want)
		}
	}
	if want := `http.Redirect(w, r, fmt.Sprintf("/admin/ticket_types/%d", n.ID)`; !strings.Contains(files[create], want) {
		t.Errorf("index.POST.go has no %s", want)
	}
	for name, src := range files {
		for _, line := range strings.Split(src, "\n") {
			if strings.Contains(line, "http.Redirect(") && strings.Contains(line, "basepath.Path") {
				t.Errorf("%s prefixes a redirect the middleware prefixes again: %s", name, line)
			}
			if strings.HasSuffix(name, ".go") && field.MatchString(line) && !strings.Contains(line, "basepath.Path(r, ") {
				t.Errorf("%s hands a view a link without the base: %s", name, strings.TrimSpace(line))
			}
		}
	}
}
