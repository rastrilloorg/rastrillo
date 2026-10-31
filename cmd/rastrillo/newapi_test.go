package main

import (
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
	app := readScaffold(t, "123-app", "internal", "app123app", "app.go") +
		readScaffold(t, "123-app", "internal", "app123app", "routes.go")
	for _, want := range []string{
		`routes.Mount("/api/v1", app.api, "api")`,
		"api, err := newAPI(database, logger)",
		"routes.Group(func(guarded *action.Routes) {",
	} {
		if !strings.Contains(app, want) {
			t.Errorf("app.go and routes.go missing %q:\n%s", want, app)
		}
	}
	// The mount is beside the page group, not in it: inside it the API
	// would sit behind csrf.Protect, which refuses a POST with no Origin,
	// and an agent's request has none.
	if strings.Index(app, `Mount("/api/v1"`) > strings.Index(app, "Group(") {
		t.Errorf("the API is mounted in the page group, behind csrf:\n%s", app)
	}

	apiGo := readScaffold(t, "123-app", "internal", "app123app", "api.go")
	for _, want := range []string{
		// apitoken.DerivePrefix("123-app"), the spec's own example.
		`const tokenPrefix = "app123app"`,
		"DB: database.Writer(), Prefix: tokenPrefix,",
		"gen.APIRoutes()...",
		"Authorize: authorize,",
		"return op.DefaultPolicy(ctx, actor, operation)",
		"Redact:    tokens.Redact,",
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
		`signedIn.Guard("session", page.sessions.Require)`,
		`signedIn.Handle("GET", tokensPage,`,
		`fresh.Guard("fresh", page.sessions.RequireFresh(10*time.Minute))`,
		"http.HandlerFunc(handlers.Create)",
		`"/tokens/revoke",`, `"/tokens/revoke-agent",`, `"/tokens/revoke-all",`,
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
