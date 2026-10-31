package scaffoldcheck

import (
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// manifest/README.md is followed by hand, so a step that no longer fits
// the app's shape fails a person, not a build. This follows it as
// written on both variants, the example resource and each step's commands
// and code at the place its prose names; then the app's gates pass, and
// its generated screens and API answer under one policy that refuses one
// operation.
func TestTheManifestRecipeServesAResourceUnderOnePolicy(t *testing.T) {
	t.Parallel()
	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			t.Parallel()
			followTheRecipe(t, variant)
		})
	}
}

func followTheRecipe(t *testing.T, variant variant) {
	directory := buildableApp(t, "recipeapp", variant.flags...)
	steps := readRecipe(t, filepath.Join(directory, "manifest", "README.md"))
	follow(t, directory, steps)
	output, err := budgetCodeBeforeGitInit(directory)
	if err != nil {
		t.Errorf("budget code after the recipe: %v\n%s", err, output)
	}
	routes := run(t, directory, rastrilloBinary, "routes", "--check")
	mount, listed := findRoute(readRoutes(routes), "*", "/admin/*")
	if !listed || !slices.Contains(mount.guards, "csrf") {
		t.Errorf("routes lists no /admin/* mount behind csrf:\n%s", routes)
	}
	runTaggedTests(t, directory, "perf", "./internal/recipeapptest/",
		"TestPerfScreens")
	refuseOneOperation(t, filepath.Join(directory,
		"internal", "recipeapp", "api.go"))
	write(t, filepath.Join(directory, "internal", "recipeapp",
		"recipe_extra_test.go"), policyTestFor(variant))
	runTests(t, directory, "./internal/recipeapp/",
		"TestThePagesAndTheAPIShareOnePolicy")
}

// recipe is manifest/README.md cut into what a reader acts on: the
// example manifest, and each numbered step's prose and indented code.
type recipe struct {
	example string
	steps   map[int]step
}

type step struct {
	prose  string
	blocks []string
}

func readRecipe(t *testing.T, path string) recipe {
	t.Helper()
	readme := readFile(t, path)
	intro, list, found := strings.Cut(readme, "\n1. ")
	if !found {
		t.Fatalf("%s has no numbered steps", path)
	}
	read := recipe{steps: map[int]step{}}
	if blocks := codeIn(intro, "    "); len(blocks) > 0 {
		read.example = blocks[0]
	}
	list = "1. " + list
	for number := 1; ; number++ {
		next := "\n" + strconv.Itoa(number+1) + ". "
		text, rest, more := strings.Cut(list, next)
		if !more {
			text = afterTheList.Split(text, 2)[0]
		}
		read.steps[number] = step{prose: proseIn(text, "       "),
			blocks: codeIn(text, "       ")}
		if !more {
			return read
		}
		list = strings.TrimPrefix(next, "\n") + rest
	}
}

// afterTheList is the gap before the first paragraph after the last
// step, which starts at the left margin where a step's lines are
// indented.
var afterTheList = regexp.MustCompile(`\n\n[^ \n]`)

// codeIn is each run of lines indented by at least indent, with the
// indent taken off; a blank line inside a run belongs to it.
func codeIn(text, indent string) []string {
	var blocks, current []string
	for _, line := range append(strings.Split(text, "\n"), "end") {
		switch {
		case strings.HasPrefix(line, indent):
			current = append(current, strings.TrimPrefix(line, indent))
		case line == "" && len(current) > 0:
			current = append(current, "")
		case len(current) > 0:
			code := strings.TrimRight(strings.Join(current, "\n"), "\n")
			blocks, current = append(blocks, code+"\n"), nil
		}
	}
	return blocks
}

func proseIn(text, indent string) string {
	var words []string
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, indent) {
			words = append(words, strings.Fields(line)...)
		}
	}
	return strings.Join(words, " ")
}

// follow does each step. A step's code goes where its prose says, and
// the prose is checked for those words, so moving the code without
// rewording the step fails here.
func follow(t *testing.T, directory string, steps recipe) {
	t.Helper()
	app := func(file string) string {
		return filepath.Join(directory, "internal", "recipeapp", file)
	}
	manifest := filepath.Join(directory, "manifest", "ticket_types.toml")
	write(t, manifest, steps.example)
	for _, number := range []int{1, 2} {
		runCommandsOf(t, directory, steps.step(t, number, 0))
	}
	write(t, filepath.Join(directory, "genassets.go"),
		steps.step(t, 3, 1, "genassets.go at the module root").blocks[0])
	pages := steps.step(t, 4, 1, "render.go's newPages, before its return")
	insertBefore(t, app("render.go"), "\treturn ui.NewPages(", pages.blocks[0])
	catalog := regexp.MustCompile(`(Catalog: \S+) to its ui.PagesConfig`).
		FindStringSubmatch(pages.prose)
	if catalog == nil {
		t.Fatalf("step 4 adds no Catalog to ui.PagesConfig: %s", pages.prose)
	}
	insertAfter(t, app("render.go"), "ui.PagesConfig{", catalog[1]+",\n")
	addImports(t, app("render.go"), importsIn(pages.prose))
	followTheSchemaStep(t, app("migrations.go"), steps.step(t, 5, 1,
		"add generatedSchema() to BootSchema's Merge, just before Schema"))
	followTheBootStep(t, directory, steps.step(t, 6, 5,
		"add a field to App",
		"after app is built and before the Routes call",
		"at the end of app.go", "after guarded.Get(homePage, app.home)",
		"perf_test.go, add to its perftest.ScreenConfig"))
	appendTo(t, manifest, "\n"+steps.step(t, 7, 1).blocks[0])
	runCommandsOf(t, directory, steps.step(t, 8, 0))
	formatEvery(t, app("render.go"), app("migrations.go"), app("app.go"),
		app("routes.go"), filepath.Join(directory, "genassets.go"),
		filepath.Join(directory, "internal", "recipeapptest", "perf_test.go"))
}

// step is step number, failing unless it has blocks pieces of code and
// its prose says each of words.
func (steps recipe) step(
	t *testing.T, number, blocks int, words ...string,
) step {
	t.Helper()
	found, ok := steps.steps[number]
	if !ok || len(found.blocks) != blocks {
		t.Fatalf("step %d: want %d pieces of code, got %d:\n%v", number,
			blocks, len(found.blocks), found.blocks)
	}
	for _, phrase := range words {
		if !strings.Contains(found.prose, phrase) {
			t.Fatalf("step %d no longer says %q: %s", number, phrase,
				found.prose)
		}
	}
	return found
}

var command = regexp.MustCompile("`((?:go|rastrillo) [^`]+)`")

// runCommandsOf runs each command a step quotes, with this checkout's
// rastrillo for the one a person installed.
func runCommandsOf(t *testing.T, directory string, step step) {
	t.Helper()
	quoted := command.FindAllStringSubmatch(step.prose, -1)
	if len(quoted) == 0 {
		t.Fatalf("no command to run in: %s", step.prose)
	}
	for _, match := range quoted {
		words := strings.Fields(match[1])
		if words[0] == "rastrillo" {
			words[0] = rastrilloBinary
		}
		run(t, directory, words[0], words[1:]...)
	}
}

// followTheSchemaStep works on both BootSchemas: a plain app's on one
// line, and --auth=magic's, which wraps.
func followTheSchemaStep(t *testing.T, path string, step step) {
	t.Helper()
	imports, code, found := strings.Cut(step.blocks[0], ")\n")
	if !strings.HasPrefix(imports, "import (") || !found {
		t.Fatalf("step 5 is not an import block then code:\n%s",
			step.blocks[0])
	}
	source := readFile(t, path)
	merge := strings.Index(source, "var BootSchema = migrate.Merge(")
	last := strings.Index(source[max(merge, 0):], ", Schema)")
	if merge < 0 || last < 0 {
		t.Fatalf("%s has no BootSchema Merge ending in Schema", path)
	}
	at := merge + last + len(", ")
	write(t, path, source[:at]+"generatedSchema(), "+source[at:]+code)
	addImports(t, path, strings.TrimPrefix(imports, "import ("))
}

func followTheBootStep(t *testing.T, directory string, step step) {
	t.Helper()
	app := filepath.Join(directory, "internal", "recipeapp", "app.go")
	insertAfter(t, app, "type App struct {", step.blocks[0])
	insertAfter(t, app, "api: api}", step.blocks[1])
	addImports(t, app, importsIn(step.prose))
	appendTo(t, app, "\n"+step.blocks[2])
	insertAfter(t, filepath.Join(directory, "internal", "recipeapp",
		"routes.go"), "guarded.Get(homePage, app.home)", step.blocks[3])
	insertAfter(t, filepath.Join(directory, "internal", "recipeapptest",
		"perf_test.go"), "perftest.ScreenConfig{", step.blocks[4])
}

func insertBefore(t *testing.T, path, anchor, code string) {
	t.Helper()
	source := readFile(t, path)
	at := strings.LastIndex(source[:indexOf(t, path, source, anchor)],
		"\n") + 1
	write(t, path, source[:at]+code+source[at:])
}

// insertAfter starts code on a line of its own straight after anchor,
// which may end a line or open a literal that goes on.
func insertAfter(t *testing.T, path, anchor, code string) {
	t.Helper()
	source := readFile(t, path)
	at := indexOf(t, path, source, anchor) + len(anchor)
	if strings.HasPrefix(source[at:], "\n") {
		at++
	} else if !strings.HasSuffix(anchor, "{") {
		at += strings.Index(source[at:], "\n") + 1
	} else {
		code = "\n" + code
	}
	write(t, path, source[:at]+code+source[at:])
}

func indexOf(t *testing.T, path, source, anchor string) int {
	t.Helper()
	at := strings.Index(source, anchor)
	if at < 0 {
		t.Fatalf("%s has no %q:\n%s", path, anchor, source)
	}
	return at
}

var (
	importSentence = regexp.MustCompile(
		`[Ii]mport((?:,? (?:and )?(?:\w+ )?"[^"]+")+)`)
	importedPath = regexp.MustCompile(`(?:(\w+) )?("[^"]+")`)
)

// importsIn is the import lines a step's prose asks for, such as
// `Import "io/fs" and assets "app"`.
func importsIn(prose string) string {
	var lines strings.Builder
	for _, sentence := range importSentence.FindAllStringSubmatch(prose, -1) {
		for _, each := range importedPath.FindAllStringSubmatch(
			sentence[1], -1) {
			lines.WriteString(strings.TrimSpace(each[1]+" "+each[2]) + "\n")
		}
	}
	return lines.String()
}

func addImports(t *testing.T, path, lines string) {
	t.Helper()
	if strings.TrimSpace(lines) == "" {
		t.Fatalf("no imports to add to %s", path)
	}
	insertAfter(t, path, "import (", lines)
}

// formatEvery does what an editor does on save, which the recipe leaves
// to it: gofmt, so its spaces become the file's tabs.
func formatEvery(t *testing.T, paths ...string) {
	t.Helper()
	for _, path := range paths {
		formatted, err := format.Source([]byte(readFile(t, path)))
		if err != nil {
			t.Fatalf("%s after the recipe: %v\n%s", path, err,
				readFile(t, path))
		}
		if err := os.WriteFile(path, formatted, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// refuseOneOperation makes api.go's authorize, the app's one policy,
// allow every operation to anyone but the one refusedOperation names.
// Tokens are allowed so that the API answers at all.
func refuseOneOperation(t *testing.T, path string) {
	t.Helper()
	const defaultPolicy = "\treturn op.DefaultPolicy(" +
		"ctx, actor, operation)\n}\n"
	source := readFile(t, path)
	if !strings.Contains(source, defaultPolicy) {
		t.Fatalf("%s's authorize no longer ends in DefaultPolicy:\n%s",
			path, source)
	}
	source = strings.Replace(source, defaultPolicy,
		"\tif operation.ID == refusedOperation {\n"+
			"\t\treturn op.Forbidden{Reason: \"refused by the test\"}\n"+
			"\t}\n\treturn nil\n}\n\nvar refusedOperation string\n", 1)
	write(t, path, source)
}

// policyTestFor is policyTest calling the variant's New. The sign-in
// sweeps run on the background group, so it stops before the database
// closes.
func policyTestFor(variant variant) string {
	newApp := "\thandler, err := New(database, origin, logger)\n"
	imports := ""
	if slices.Contains(variant.flags, "--auth=magic") {
		newApp = "\tgroup := &background.Group{}\n\tdefer group.Stop()\n" +
			"\thandler, err := New(database, origin, group,\n" +
			"\t\tmail.Logged(logger), logger)\n"
		imports = "\t\"amadan.net/rastrillo/rastrillo/background\"\n" +
			"\t\"amadan.net/rastrillo/rastrillo/mail\"\n"
	}
	return strings.NewReplacer("\t// the app's New\n", newApp,
		"\t// the app's imports\n", imports).Replace(policyTest)
}

// policyTest is written into the app's own package, where newTokens and
// refusedOperation are. Creating a ticket type is allowed first, so the
// 403s after are the policy's and not the CSRF check's.
const policyTest = `package recipeapp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/apitoken"
	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/dbtest"
	// the app's imports
)

const origin = "http://app.test"

func TestThePagesAndTheAPIShareOnePolicy(t *testing.T) {
	schema := dbtest.FromSet(BootSchema)
	defer schema.Remove()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	database, err := db.Open(schema.Path(t), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	// the app's New
	if err != nil {
		t.Fatal(err)
	}
	secret := mint(t, database)
	page := func(method string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, "/admin/ticket_types",
			strings.NewReader("Name=General&Price=12.50"+
				"&Price.locale=root&Price.currency=USD"))
		request.Header.Set("Content-Type",
			"application/x-www-form-urlencoded")
		request.Header.Set("Origin", origin)
		return serve(handler, request)
	}
	api := func(method string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, "/api/v1/ticket_types",
			strings.NewReader(` + "`" + `{"name": "General"}` + "`" + `))
		request.Header.Set("Authorization", "Bearer "+secret)
		request.Header.Set("Content-Type", "application/json")
		return serve(handler, request)
	}
	expect(t, "the list page", page("GET"), http.StatusOK)
	expect(t, "the API's list", api("GET"), http.StatusOK)
	expect(t, "a create from the page", page("POST"), http.StatusSeeOther)
	expect(t, "a create from the API", api("POST"), http.StatusCreated)
	refusedOperation = "ticket_types.create"
	expect(t, "a refused page create", page("POST"), http.StatusForbidden)
	expect(t, "a refused API create", api("POST"), http.StatusForbidden)
}

func mint(t *testing.T, database *db.DB) string {
	tokens, err := newTokens(database)
	if err != nil {
		t.Fatal(err)
	}
	secret, _, err := tokens.Mint(context.Background(), "ada@example.com",
		apitoken.MintInput{Agent: "Claude", Access: rastrillo.Write})
	if err != nil {
		t.Fatal(err)
	}
	return secret
}

func serve(
	handler http.Handler, request *http.Request,
) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func expect(
	t *testing.T, what string, answer *httptest.ResponseRecorder, want int,
) {
	t.Helper()
	if answer.Code != want {
		t.Errorf("%s: %d, want %d:\n%s", what, answer.Code, want,
			answer.Body)
	}
}
`
