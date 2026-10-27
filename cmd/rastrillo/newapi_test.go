package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"amadan.net/rastrillo/rastrillo/internal/generate/apigen"
	"amadan.net/rastrillo/rastrillo/internal/generate/contract"
)

// A new app has the JSON API from its first commit (JSON API spec §3):
// the tree on the outer mux beside the router, never inside it, where
// csrf.Protect and the page middleware would run on a bearer-token
// request; a token prefix made from the app's name; the token table at
// boot; and a policy named in writing. The scaffold also writes what
// rastrillo generate would for an app with no resources yet, gen/api.go
// and api/v1.json, so the app builds and its generate --check passes
// before the first manifest exists.
func TestNewMountsTheAPIForANewApp(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"123-app"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	app := readScaffold(t, "123-app", "internal", "app123app", "app.go")
	for _, want := range []string{
		"func Mux(r, apiTree http.Handler) *http.ServeMux",
		`mux.Handle("/api/v1", apiTree)`,
		`mux.Handle("/api/v1/", apiTree)`,
		"apiTree, err := API(d, logger)",
	} {
		if !strings.Contains(app, want) {
			t.Errorf("app.go missing %q:\n%s", want, app)
		}
	}
	// The mount is on the outer mux, not the chi router: inside it the
	// API would sit behind csrf.Protect, which refuses a POST with no
	// Origin, and an agent's request has none.
	if strings.Contains(app, `r.Handle("/api/v1`) || strings.Contains(app, `r.Mount("/api/v1`) {
		t.Errorf("app.go mounts the API inside the router, behind csrf.Protect:\n%s", app)
	}

	apiGo := readScaffold(t, "123-app", "internal", "app123app", "api.go")
	for _, want := range []string{
		// apitoken.DerivePrefix("123-app"), the spec's own example.
		`const tokenPrefix = "app123app"`,
		"apitoken.New(apitoken.Config{DB: d.Writer(), Prefix: tokenPrefix})",
		"gen.APIRoutes()...",
		"Authorize: authorize,",
		"return op.DefaultPolicy(ctx, a, o)",
		"Redact: tokens.Redact,",
	} {
		if !strings.Contains(apiGo, want) {
			t.Errorf("api.go missing %q:\n%s", want, apiGo)
		}
	}

	migrations := readScaffold(t, "123-app", "internal", "app123app", "migrations.go")
	if !strings.Contains(migrations, "var BootSchema = migrate.Merge(apitoken.Schema, Schema)") {
		t.Errorf("migrations.go must apply apitoken.Schema at boot, or the first API request finds no table:\n%s", migrations)
	}

	tokens := readScaffold(t, "123-app", "internal", "app123app", "tokens.go")
	for _, want := range []string{
		"r.Use(sess.Require)",
		`r.Get("/tokens", th.Page)`,
		`r.With(sess.RequireFresh(10*time.Minute)).Post("/tokens", th.Create)`,
		`r.Post("/tokens/revoke", th.Revoke)`,
		`r.Post("/tokens/revoke-agent", th.RevokeAgent)`,
		`r.Post("/tokens/revoke-all", th.RevokeAll)`,
	} {
		if !strings.Contains(tokens, want) {
			t.Errorf("tokens.go missing %q:\n%s", want, tokens)
		}
	}
	page := readScaffold(t, "123-app", "internal", "app123app", "templates", "tokens.html")
	if !strings.Contains(page, `{{template "api-tokens" dict "L" $L "Page" .Page "Path" (path $ "/tokens")}}`) {
		t.Errorf("tokens.html does not draw the page through the api-tokens partial:\n%s", page)
	}
	render := readScaffold(t, "123-app", "internal", "app123app", "render.go")
	if !strings.Contains(render, `"tokens"`) {
		t.Errorf("render.go does not build the tokens page:\n%s", render)
	}

	// Byte for byte what rastrillo generate writes for an app with no
	// resources, so --check finds nothing stale on a fresh scaffold.
	wantAPI, err := apigen.File("123-app", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := readScaffold(t, "123-app", "gen", "api.go"); got != string(wantAPI) {
		t.Errorf("gen/api.go is not what generate writes:\n%s\nwant:\n%s", got, wantAPI)
	}
	wantContract, err := contract.Contract(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := readScaffold(t, "123-app", "api", "v1.json"); got != string(wantContract) {
		t.Errorf("api/v1.json is not what generate writes:\n%s\nwant:\n%s", got, wantContract)
	}
}

// The scaffold's gate runs generate --check, in the Makefile's ci target
// and as a step of its own for a runner that reports steps (spec §7).
// Without it an app's gen/ or api/v1.json can go stale, or a breaking
// change to /api/v1 can ship, with every other check green.
func TestNewGateRunsGenerateCheck(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"demoapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	mk := readScaffold(t, "demoapp", "Makefile")
	if !strings.Contains(mk, "\ngenerate-check:\n\t$(RASTRILLO) generate --check\n") {
		t.Errorf("Makefile has no generate-check target running generate --check:\n%s", mk)
	}
	var ci string
	for _, line := range strings.Split(mk, "\n") {
		if strings.HasPrefix(line, "ci: ") {
			ci = line
		}
	}
	if !strings.Contains(" "+ci+" ", " generate-check ") {
		t.Errorf("the ci target does not run generate-check: %q", ci)
	}
	step := filepath.Join("demoapp", ".amadan", "ci.d", "45-generate-check")
	fi, err := os.Stat(step)
	if err != nil {
		t.Fatalf("no ci.d step for generate-check: %v", err)
	}
	if fi.Mode()&0o111 == 0 {
		t.Errorf("%s is not executable; amadan's runner would resolve it skipped", step)
	}
	if b := readScaffold(t, step); b != "#!/bin/sh\nexec make generate-check\n" {
		t.Errorf("%s must exec the Makefile target:\n%s", step, b)
	}
}

// tokenRule is the gitleaks rule for the token grammar (spec §5), the
// same in rastrillo's own config and in every scaffolded app's.
const tokenRule = `\b[a-z][a-z0-9]{0,15}_[0-9A-Za-z]{12}_[0-9A-Za-z]{43}\b`

// Both configs carry the rule, and it finds a token in a line of
// source. The token is built here at run time: one written out in this
// file would be found by the repository's own gitleaks run.
func TestGitleaksFindsAPITokens(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"demoapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	token := "demoapp_" + strings.Repeat("k3Jx", 3) + "_" + strings.Repeat("Q9pL2", 8) + "mWa"
	for _, path := range []string{filepath.Join(repoRoot(t), ".gitleaks.toml"), filepath.Join("demoapp", ".gitleaks.toml")} {
		var cfg struct {
			Extend struct{ UseDefault bool }
			Rules  []struct{ ID, Regex string }
		}
		if _, err := toml.DecodeFile(path, &cfg); err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		if !cfg.Extend.UseDefault {
			t.Errorf("%s: drops gitleaks' default rules", path)
		}
		var found bool
		for _, r := range cfg.Rules {
			if r.Regex != tokenRule {
				continue
			}
			found = true
			re := regexp.MustCompile(r.Regex)
			if !re.MatchString(`Authorization: Bearer ` + token) {
				t.Errorf("%s: rule %s misses a token in a header line", path, r.ID)
			}
			if re.MatchString(token[:len(token)-1]) {
				t.Errorf("%s: rule %s matches a 42-character secret", path, r.ID)
			}
		}
		if !found {
			t.Errorf("%s: no rule with the token regex %s", path, tokenRule)
		}
	}
}

// manifest/README.md's step 5 rewrites BootSchema, and an author copies
// it. This scaffolds an app, declares a resource, generates, and applies
// step 5's code exactly as the README prints it, then boots the app over
// a database migrated from the resulting BootSchema. A recipe that
// drops apitoken.Schema leaves api_tokens uncreated, and every API
// request then fails on a missing table.
func TestTheManifestRecipeKeepsTheTokenTable(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds, generates and tests a whole app")
	}
	dir := scaffoldApp(t, "recipeapp")
	mustWrite(t, filepath.Join(dir, "manifest", "bookmarks.toml"), `name  = "bookmarks"
route = "/bookmarks"
store = "mergeable"

[list]
columns = [{ field = "Title" }]

[form]
basics = [{ name = "Title", required = true }]
`)
	if err := runGenerate([]string{dir}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	// Step 5's code is the README's indented lines between its heading
	// and step 6, with the indent taken off.
	readme := fmt.Sprintf(manifestReadme, "recipeapp", "recipeapp")
	step := readme[strings.Index(readme, "\n5. "):strings.Index(readme, "\n6. ")]
	var code []string
	for _, line := range strings.Split(step, "\n") {
		if strings.HasPrefix(line, "       ") {
			code = append(code, strings.TrimPrefix(line, "       "))
		}
	}
	snippet := strings.Join(code, "\n")
	imports := snippet[:strings.Index(snippet, ")\n")+2]
	rest := snippet[len(imports):]
	if !strings.HasPrefix(imports, "import (") || !strings.Contains(rest, "var BootSchema = ") {
		t.Fatalf("step 5 no longer reads as an import block then code:\n%s", snippet)
	}

	path := filepath.Join(dir, "internal", "recipeapp", "migrations.go")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	boot := regexp.MustCompile(`(?m)^var BootSchema = .*\n`)
	if !boot.MatchString(src) {
		t.Fatalf("migrations.go has no BootSchema line to replace:\n%s", src)
	}
	src = boot.ReplaceAllString(src, "")
	end := strings.Index(src, ")\n") + 2
	src = src[:end] + "\n" + imports + src[end:] + "\n" + rest + "\n"
	// What an author's editor does next when the recipe no longer names
	// apitoken: the unused import goes, and the build stays green.
	if !regexp.MustCompile(`(?m)^[^/\n]*apitoken\.`).MatchString(src) {
		src = strings.Replace(src, "\t\"amadan.net/rastrillo/rastrillo/apitoken\"\n", "", 1)
	}
	mustWrite(t, path, src)

	mustWrite(t, filepath.Join(dir, "internal", "recipeapptest", "recipe_test.go"), `package recipeapptest

import (
	"testing"

	"amadan.net/rastrillo/rastrillo/dbtest"

	"recipeapp/internal/recipeapp"
)

func TestBootSchemaHasTheTokenTable(t *testing.T) {
	newApp(t)
	set := dbtest.FromSet(recipeapp.BootSchema)
	defer set.Remove()
	d := set.Open(t)
	var n int
	if err := d.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'api_tokens'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Error("BootSchema after manifest/README.md's step 5 creates no api_tokens table")
	}
}
`)
	if out, err := goTestIn(dir, "-run", "^TestBootSchemaHasTheTokenTable$", "./internal/recipeapptest/"); err != nil {
		t.Fatalf("the app after the recipe's step 5:\n%s", out)
	}
}
