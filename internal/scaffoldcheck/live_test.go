package scaffoldcheck

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// docs/site/live.md's "In a new app" is followed by hand, so a step that
// no longer fits the scaffold fails a person. This follows it in a new
// app of each variant, its code exactly as written, then a test inside
// the app proves the region, the fragment answer and the notice.
func TestANewAppGoesLiveAsTheGuideSays(t *testing.T) {
	t.Parallel()
	guide := newAppGuide(t)
	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			t.Parallel()
			directory := buildableApp(t, "liveapp", variant.flags...)
			followTheLiveGuide(t, directory, guide)
			write(t, filepath.Join(directory, "internal", "liveapp",
				"live_extra_test.go"), withTheVariantsNew(variant, liveTest))
			runTests(t, directory, "./internal/liveapp/",
				"TestTheMessagesRegionGoesLive")
		})
	}
}

// liveGuide is the section's code, in the order a reader meets it.
type liveGuide struct {
	hub, region, useRegion, notify, form string
}

func newAppGuide(t *testing.T) liveGuide {
	t.Helper()
	page := readFile(t, filepath.Join(repositoryRoot(), "docs", "site",
		"live.md"))
	_, section, found := strings.Cut(page, "\n## In a new app\n")
	if !found {
		t.Fatal("docs/site/live.md has no `## In a new app` section")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	fences := fence.FindAllStringSubmatch(section, -1)
	if len(fences) != 5 {
		t.Fatalf("`## In a new app` has %d code blocks, want 5: the "+
			"topic, the region, its use, the notify and the form",
			len(fences))
	}
	return liveGuide{hub: fences[0][1], region: fences[1][1],
		useRegion: fences[2][1], notify: fences[3][1], form: fences[4][1]}
}

var fence = regexp.MustCompile("(?s)```[a-z]+\n(.*?)```")

// followTheLiveGuide puts each block where the guide's prose says: the
// hub in Router, the region and form in the home page, the notify in
// the handler that saves.
func followTheLiveGuide(t *testing.T, directory string, guide liveGuide) {
	t.Helper()
	app := func(file string) string {
		return filepath.Join(directory, "internal", "liveapp", file)
	}
	source := readFile(t, app("app.go"))
	start := strings.Index(source, "\tapp.live, err = live.New(")
	end := strings.Index(source[max(start, 0):], "\n\t})\n")
	if start < 0 || end < 0 {
		t.Fatal("app.go's Router builds no hub to declare a topic in")
	}
	end += start + len("\n\t})\n")
	source = source[:start] + indent(guide.hub) + source[end:]
	if !strings.Contains(source, "\t\"net/http\"\n") {
		t.Fatal("app.go no longer imports net/http, which the guide's " +
			"topic needs")
	}
	write(t, app("app.go"), source)
	write(t, app("templates/index.html"), "{{define \"content\"}}\n"+
		guide.useRegion+strings.TrimSuffix(guide.form, "\n")+
		"\n<input name=\"text\"><button>Send</button>\n</form>\n{{end}}\n"+
		guide.region)
	write(t, app("messages.go"), strings.Replace(messagesHandler,
		"\t// the guide's notify\n", indent(guide.notify), 1))
	insertAfter(t, app("routes.go"), "guarded.Get(homePage, app.home)",
		"\t\tguarded.Post(\"/messages\", app.postMessage)\n")
	write(t, app("handlers.go"), homeWithMessages)
	formatEvery(t, app("app.go"), app("messages.go"), app("routes.go"))
}

func indent(code string) string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSuffix(code, "\n"),
		"\n") {
		lines = append(lines, "\t"+line)
	}
	return strings.Join(lines, "\n") + "\n"
}

// messagesHandler keeps the posted messages in memory: the guide is
// about the page going live, not about storing them.
const messagesHandler = `package liveapp

import (
	"sync"

	"amadan.net/rastrillo/rastrillo/action"
)

var (
	messagesMutex sync.Mutex
	messages      []string
)

func (app *App) postMessage(visit *action.Visit) {
	messagesMutex.Lock()
	messages = append(messages, visit.Request().FormValue("text"))
	messagesMutex.Unlock()
	// the guide's notify
}

func sentMessages() []string {
	messagesMutex.Lock()
	defer messagesMutex.Unlock()
	return append([]string(nil), messages...)
}
`

const homeWithMessages = `package liveapp

import (
	"net/http"

	"amadan.net/rastrillo/rastrillo/action"
)

func (app *App) home(visit *action.Visit) {
	visit.Render("index", http.StatusOK,
		map[string]any{"Messages": sentMessages()})
}
`

// liveTest is written into the app's own package. It posts as a browser
// on the app's origin would, so the CSRF check passes.
const liveTest = `package liveapp

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/db"
	// the app's imports
)

const origin = "http://app.test"

func TestTheMessagesRegionGoesLive(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	database, err := db.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	// the app's New
	if err != nil {
		t.Fatal(err)
	}
	page := send(handler, get("/"))
	for _, want := range []string{` + "`" + `name="rastrillo-live"` + "`" + `,
		` + "`" + `data-live="messages@0` + "`" + `, ` + "`" + `<div id="notice" role="status"></div>` + "`" + `} {
		if !strings.Contains(page.Body.String(), want) {
			t.Errorf("GET / lacks %s:\n%s", want, page.Body)
		}
	}
	post := httptest.NewRequest(http.MethodPost, origin+"/messages",
		strings.NewReader(url.Values{"text": {"hello"}}.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("Origin", origin)
	post.Header.Set("Sec-Fetch-Site", "same-origin")
	posted := send(handler, post)
	if posted.Code != http.StatusSeeOther {
		t.Fatalf("POST /messages = %d, want 303:\n%s", posted.Code,
			posted.Body)
	}
	next := get("/")
	for _, cookie := range posted.Result().Cookies() {
		next.AddCookie(cookie)
	}
	after := send(handler, next).Body.String()
	for _, want := range []string{"hello", ` + "`" + `data-live="messages@1` + "`" + `,
		"<p rst-notice>Sent.</p>"} {
		if !strings.Contains(after, want) {
			t.Errorf("GET / after posting lacks %s:\n%s", want, after)
		}
	}
	fragment := get("/")
	fragment.Header.Set("Rastrillo-Fragment", "messages")
	region := send(handler, fragment)
	if got := region.Header().Get("Rastrillo-Regions"); got != "messages" {
		t.Errorf("Rastrillo-Regions = %q, want messages", got)
	}
	body := region.Body.String()
	if !strings.HasPrefix(body, ` + "`" + `<ul id="messages"` + "`" + `) ||
		strings.Contains(body, "<html") {
		t.Errorf("a fragment request got more than its region:\n%s", body)
	}
}

func get(path string) *http.Request {
	return httptest.NewRequest(http.MethodGet, origin+path, nil)
}

func send(
	handler http.Handler, request *http.Request,
) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
`
