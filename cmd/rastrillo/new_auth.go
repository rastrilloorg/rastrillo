package main

import (
	"fmt"
	"go/format"
	"path/filepath"
	"strings"

	"amadan.net/rastrillo/rastrillo/ui"
)

// authKinds are the sign-ins `rastrillo new --auth` can start an app
// with. "none" is the default and leaves the scaffold exactly as it
// was: accounts are an opt-in, so no existing scaffold changes.
var authKinds = []string{"none", "magic"}

// magicAuth turns a freshly assembled scaffold into one that signs
// people in by emailed link (rastrillo/auth, with ui's sign-in screen).
// It is the recipe first applied by hand to a fresh app and kept once
// that app's whole make ci passed (discussion #97).
//
// It edits the assembled files rather than forking every template, so
// the default scaffold stays the one template set. Each edit names the
// text it expects, and a scaffold whose templates moved fails here
// loudly instead of writing an app that does not build.
type magicAuth struct {
	files    map[string]string
	name     string
	pkg      string
	env      string
	appDir   string
	testDir  string
	problems []string
}

func applyMagicAuth(
	files map[string]string, name, pkg, appDir string,
) error {
	edit := &magicAuth{
		files: files, name: name, pkg: pkg, appDir: appDir,
		env:     strings.ToUpper(pkg),
		testDir: filepath.Join(name, "internal", pkg+"test"),
	}
	edit.schema()
	edit.app()
	edit.newFiles()
	edit.layout()
	edit.main()
	edit.harness()
	edit.perf()
	edit.agents()
	if len(edit.problems) > 0 {
		return fmt.Errorf("--auth=magic: the scaffold has moved under "+
			"it:\n  %s", strings.Join(edit.problems, "\n  "))
	}
	return formatGo(files)
}

func (edit *magicAuth) swap(path, old, replacement string) {
	source, ok := edit.files[path]
	if !ok {
		edit.problems = append(edit.problems, path+": not in the scaffold")
		return
	}
	if !strings.Contains(source, old) {
		edit.problems = append(edit.problems,
			fmt.Sprintf("%s: expected %q", path, firstLine(old)))
		return
	}
	edit.files[path] = strings.Replace(source, old, replacement, 1)
}

// importAfter adds imports after a line of an import block; formatGo
// sorts them later.
func (edit *magicAuth) importAfter(path, after string, imports ...string) {
	var lines strings.Builder
	lines.WriteString(after)
	for _, imported := range imports {
		lines.WriteString("\n\t\"amadan.net/rastrillo/rastrillo/" +
			imported + "\"")
	}
	edit.swap(path, after, lines.String())
}

// fill and filled name an app's own text: __PKG__ and the rest in a
// template added whole, %s with the package in an edit.
func (edit *magicAuth) fill(template string) string {
	return strings.NewReplacer("__PKG__", edit.pkg, "__NAME__", edit.name,
		"__ENV__", edit.env).Replace(template)
}

func (edit *magicAuth) filled(template string) string {
	return strings.ReplaceAll(template, "%s", edit.pkg)
}

// schema: sessions before auth (auth's backfill reads the sessions
// table), pow for the guard's nonces, then what was there. The example
// model becomes owned, and its migration is what generate writes.
func (edit *magicAuth) schema() {
	migrations := filepath.Join(edit.appDir, "migrations.go")
	edit.swap(migrations,
		"var BootSchema = migrate.Merge(apitoken.Schema, Schema)",
		"var BootSchema = migrate.Merge(sessions.Schema, auth.Schema,\n"+
			"\tpow.Schema, apitoken.Schema, Schema)")
	edit.importAfter(migrations, `"amadan.net/rastrillo/rastrillo/apitoken"`,
		"auth", "pow", "sessions")
	edit.swap(filepath.Join(edit.appDir, "models.go"),
		"// Never bind a request body onto a model; AGENTS.md says why.\n"+
			"type Note struct {\n\tID        int64\n",
		"// Query it through owned (auth.go), and never bind a request\n"+
			"// body onto a model; AGENTS.md says why.\n"+
			"type Note struct {\n\tID        int64\n"+
			"\tOwner     string `gorm:\"index\"`\n")
	plain := "CREATE TABLE `notes` (`id` integer PRIMARY KEY " +
		"AUTOINCREMENT,`title` text,`body` text,`created_at` datetime," +
		"`updated_at` datetime);\n\n"
	owned := "CREATE TABLE `notes` (`id` integer PRIMARY KEY " +
		"AUTOINCREMENT,`owner` text,`title` text,`body` text," +
		"`created_at` datetime,`updated_at` datetime);\n\n" +
		"CREATE INDEX `idx_notes_owner` ON `notes`(`owner`);\n\n"
	for _, file := range []string{"0001_init.sql", "schema.sql"} {
		edit.swap(filepath.Join(edit.appDir, "migrations", file),
			plain, owned)
	}
}

// app: New and Router take the background group the sign-in sweeps run
// on (named group, not background, which would hide the package), which
// the caller stops, and the mailer: main's from the
// environment, a test's outbox. Owned pages sit behind the session.
func (edit *magicAuth) app() {
	app := filepath.Join(edit.appDir, "app.go")
	edit.swap(app, "ServeMux that rastrillo.Options.Mux takes.\n",
		"ServeMux that rastrillo.Options.Mux takes. Stop group before\n"+
			"// closing the database: sign-in's sweeps run on it.\n")
	for _, function := range []string{"New", "Router"} {
		edit.swap(app, "func "+function+"(\n"+
			"\tdatabase *db.DB, origin string, logger *slog.Logger,\n",
			"func "+function+"(\n"+
				"\tdatabase *db.DB, origin string, "+
				"group *background.Group,\n"+
				"\tmailer mail.Sender, logger *slog.Logger,\n")
	}
	edit.swap(app, "Router(database, origin, logger)",
		"Router(database, origin, group, mailer, logger)")
	edit.swap(app, "api: api}\n", "api: api}\n"+
		"\tif err := app.startAuth(group, mailer); err != nil {\n"+
		"\t\treturn nil, err\n\t}\n")
	edit.swap(app, "\tapi      http.Handler\n}",
		"\tapi      http.Handler\n\tauth     *auth.Auth\n}")
	edit.importAfter(app, `"amadan.net/rastrillo/rastrillo/action"`,
		"auth", "background", "mail")
	routes := filepath.Join(edit.appDir, "routes.go")
	edit.swap(routes, `const homePage action.Path = "/"`,
		`const homePage action.Path = "/"`+"\n"+
			`const accountPage action.Path = "/account"`)
	edit.swap(routes, "pages.Middleware)\n", "pages.Middleware)\n"+
		"\t\tguarded.Guard(\"signedIn\", app.signedIn)\n"+
		"\t\tapp.mountAuth(guarded)\n")
	edit.swap(routes, "\n\t})\n}\n", "\n"+
		"\t\t// Owned pages: a signed-out visitor signs in first.\n"+
		"\t\tguarded.Group(func(owned *action.Routes) {\n"+
		"\t\t\towned.Guard(\"session\", app.auth.RequireSession)\n"+
		"\t\t\towned.Get(accountPage, app.account)\n\t\t})\n\t})\n}\n")
}

// layout: every page offers Sign out to a signed-in visitor and Sign in
// to anyone else; renderPage tells it which.
func (edit *magicAuth) layout() {
	scheme := "\tvalues[\"Scheme\"], values[\"Base\"] = " +
		"localized.Scheme, localized.Base\n"
	edit.swap(filepath.Join(edit.appDir, "render.go"), scheme, scheme+
		"\tvalues[\"SignedIn\"], _ = "+
		"request.Context().Value(signedInKey{}).(bool)\n")
	layout := filepath.Join(edit.appDir, "templates", "layout.html")
	source, ok := edit.files[layout]
	if !ok {
		return
	}
	end := schemeMenuEnd(source)
	switch {
	// The stage shell has no header chrome to sit in: an app built on
	// it reaches the sign-in screen through its own links.
	case end < 0 && strings.Contains(source, `{{block "backdrop"`):
		edit.swap(filepath.Join(edit.testDir, "signin_test.go"),
			signoutCheck, stageSignoutCheck)
	case end < 0:
		edit.problems = append(edit.problems,
			layout+": no scheme menu block to sit beside")
	default:
		edit.files[layout] = source[:end] + accountControl + source[end:]
	}
}

// schemeMenuEnd is where the layout's scheme menu block ends, or -1.
func schemeMenuEnd(layout string) int {
	at := strings.Index(layout, `{{block "scheme" .}}`)
	if at < 0 {
		return -1
	}
	menu := strings.Index(layout[at:], "scheme-menu")
	if menu < 0 {
		return -1
	}
	end := strings.Index(layout[at+menu:], "{{end}}")
	if end < 0 {
		return -1
	}
	return at + menu + end + len("{{end}}")
}

// accountControl is Sign in or Sign out. Under a base path the hub signs
// people in and the plugins answer 404 here: a root-relative /signin
// would leave the tenant for the product's own sign-in screen.
const accountControl = `{{if not (opt $ "Base")}}{{$authL := localizer $}}` +
	`{{if opt $ "SignedIn"}}<form method="post" action="/signout">` +
	`<button rst-btn>{{T $authL "rastrillo.ui.account_signout"}}</button>` +
	`</form>{{else}}<a href="/signin" rst-btn>` +
	`{{T $authL "rastrillo.ui.account_signin"}}</a>{{end}}{{end}}`

// signoutCheck is the sign-in test's check for the layout's sign-out
// form, which the stage shell has no header for.
const (
	signoutCheck = "if recorder.Code != http.StatusOK ||\n" +
		"\t\t!strings.Contains(recorder.Body.String(), " +
		"`action=\"/signout\"`) {\n" +
		"\t\tt.Fatalf(\"signed-in /account = %d, want 200 with a " +
		"sign-out form:\\n%s\","
	stageSignoutCheck = "// The stage shell has no header, so no " +
		"sign-out form to look for.\n" +
		"\tif recorder.Code != http.StatusOK {\n" +
		"\t\tt.Fatalf(\"signed-in /account = %d, want 200:\\n%s\","
)

func (edit *magicAuth) main() {
	main := filepath.Join(edit.name, "cmd", edit.name, "main.go")
	edit.swap(main, edit.filled("\tmux, err := %s.New(\n"+
		"\t\tdatabase, origin, logger)\n"),
		fmt.Sprintf("\tmailer := mail.FromEnv(%q, logger)\n", edit.env)+
			"\tgroup := &background.Group{}\n"+
			edit.filled("\tmux, err := %s.New(\n"+
				"\t\tdatabase, origin, group, mailer, logger)\n"))
	configure := edit.filled("\t%s.Configure(&options, mux, started)\n")
	edit.swap(main, configure, configure+"\toptions.Background = group\n")
	edit.importAfter(main, `"amadan.net/rastrillo/rastrillo/db"`,
		"background", "mail")
}

// harness: every caller in the tests passes a group it stops and a
// mailer: the harness an outbox a test can open links from, the rest
// the logger.
func (edit *magicAuth) harness() {
	harness := filepath.Join(edit.testDir, "harness_test.go")
	edit.swap(harness, "func newApp(t *testing.T) http.Handler {\n"+
		"\tt.Helper()\n", "func newApp(t *testing.T) http.Handler {\n"+
		"\tt.Helper()\n\thandler, _ := newAppWithMail(t)\n"+
		"\treturn handler\n}\n\n"+
		"// newAppWithMail is newApp with the outbox its links land in.\n"+
		"func newAppWithMail(t *testing.T) (http.Handler, *outbox) {\n"+
		"\tt.Helper()\n")
	edit.swap(harness, edit.filled("\tmux, err := %s.New(\n"+
		"\t\tdatabase, testOrigin, logger)\n"),
		"\tgroup := &background.Group{}\n"+
			"\tt.Cleanup(group.Stop) // before database.Close: "+
			"last in, first out\n\tbox := &outbox{}\n"+
			edit.filled("\tmux, err := %s.New(\n"+
				"\t\tdatabase, testOrigin, group, box, logger)\n"))
	edit.swap(harness, "\treturn mux\n}", "\treturn mux, box\n}")
	edit.importAfter(harness, `"amadan.net/rastrillo/rastrillo/db"`,
		"background")
	browser := filepath.Join(edit.testDir, "browser_test.go")
	edit.swap(browser, edit.filled("\t\tmux, err := %s.New(\n"+
		"\t\t\tdatabase, origin, logger)\n"),
		"\t\tgroup := &background.Group{}\n\t\tt.Cleanup(group.Stop)\n"+
			"\t\tmailer := mail.Logged(logger)\n"+
			edit.filled("\t\tmux, err := %s.New(\n"+
				"\t\t\tdatabase, origin, group, mailer, logger)\n"))
	edit.importAfter(browser, `"amadan.net/rastrillo/rastrillo/db"`,
		"background", "mail")
}

// perf: /account is measured behind the perf test's own sign-in, and
// pow's assets and the two link landings are not screens.
func (edit *magicAuth) perf() {
	perf := filepath.Join(edit.testDir, "perf_test.go")
	edit.swap(perf, edit.filled("\trouter, err := %s.Router(\n"+
		"\t\tdatabase, testOrigin, logger)\n"),
		"\tgroup := &background.Group{}\n\tbox := &outbox{}\n"+
			edit.filled("\trouter, err := %s.Router(\n"+
				"\t\tdatabase, testOrigin, group, box, logger)\n"))
	stops := "\toptions.Background = group // closeAll stops it\n"
	for _, started := range []string{"time.Now()", "started"} {
		configure := edit.filled("\t%s.Configure(&options, mux, " +
			started + ")\n")
		edit.swap(perf, configure, configure+stops)
	}
	edit.swap(perf, "\tperftest.Screens(t, perftest.ScreenConfig{"+
		"Routes: router, Handler: handler})", "\tperftest.Screens(t, "+
		"perftest.ScreenConfig{\n\t\tRoutes: router, Handler: handler,\n"+
		"\t\tSignin: func(client *http.Client, base string) {\n"+
		"\t\t\tsignInOver(t, client, base, box, \"perf@example.com\")\n"+
		"\t\t},\n\t})")
	edit.swap(perf, edit.filled("\tmux, err := %s.New(\n"+
		"\t\tdatabase, testOrigin, logger)\n\tif err != nil {\n"),
		"\tgroup := &background.Group{}\n\tmailer := mail.Logged(logger)\n"+
			edit.filled("\tmux, err := %s.New(\n"+
				"\t\tdatabase, testOrigin, group, mailer, logger)\n"+
				"\tif err != nil {\n\t\tgroup.Stop()\n"))
	edit.importAfter(perf, `"amadan.net/rastrillo/rastrillo/db"`,
		"background", "mail")
	budgets := filepath.Join(edit.name, ".rastrillo", "budgets.txt")
	edit.swap(budgets, budgetsTxtTemplate, budgetsTxtTemplate+
		"perf  GET /pow/*          skip  proof-of-work script and worker "+
		"assets, not a screen\n"+
		"perf  GET /auth/callback  skip  keymail landing: answers only "+
		"with a pending sign-in\n"+
		"perf  GET /auth/verify    skip  an emailed link's page: needs a "+
		"fresh token, which signin_test.go opens\n")
}

func (edit *magicAuth) newFiles() {
	stage, ok := ui.Layout("stage")
	if !ok {
		edit.problems = append(edit.problems,
			"ui has no stage layout for the sign-in screen")
	}
	templates := filepath.Join(edit.appDir, "templates")
	edit.files[filepath.Join(edit.appDir, "auth.go")] =
		edit.fill(scaffoldFile("auth.go.tmpl"))
	edit.files[filepath.Join(templates, "signin.html")] =
		scaffoldFile("signin.html")
	edit.files[filepath.Join(templates, "stage.html")] = string(stage)
	edit.files[filepath.Join(edit.testDir, "signin_test.go")] =
		edit.fill(scaffoldFile("signin_test.go.tmpl"))
}

// agents rewrites what AGENTS.md says otherwise, and adds the sign-in.
func (edit *magicAuth) agents() {
	agents := filepath.Join(edit.name, "AGENTS.md")
	edit.swap(agents, "- Every query touching user-owned rows goes "+
		"through `scope.Owned` —\n  reads AND writes, transactions included.",
		"- Every query touching user-owned rows goes through "+
			"`app.owned(request)`\n  (auth.go), reads and writes, and a new "+
			"row sets\n  `Owner: ownerOf(request)`. Inside a transaction, "+
			"scope the callback's\n  `tx` instead, "+
			"`scope.OwnedBy(tx, \"owner\", ownerOf(request))`:\n"+
			"  `app.owned(request)` there waits on the writer connection "+
			"the\n  transaction holds.")
	edit.swap(agents, "- Screens work with JavaScript disabled;",
		"- Screens work with JavaScript disabled, except the sign-in "+
			"screen,\n  whose proof of work needs it;")
	edit.swap(agents, "never dependent on JavaScript to be usable.",
		"never dependent on JavaScript to be usable,\nexcept to sign in.")
	edit.swap(filepath.Join(edit.appDir, "tokens.go"),
		"It needs sign-in, which a new\n// app does not have yet; "+
			"AGENTS.md says how to mount it.",
		"It needs *sessions.Sessions,\n// which this app's sign-in "+
			"(auth.go) does not keep.")
	edit.swap(agents, "- Sign-in: `skill/signin.md`. With "+
		"`*sessions.Sessions`, keep\n  `NewTokenPage(database, sessions)` "+
		"on `App` and call\n  `app.tokens.Mount(guarded)` in `Routes`' "+
		"page group.", "- More sign-in, such as 2FA: `skill/signin.md`. "+
		"tokens.go's page\n  needs `*sessions.Sessions`, which this sign-in "+
		"does not keep.")
	edit.swap(agents, "\n## Layout", "\n"+fmt.Sprintf(signinSection,
		"`", edit.pkg, edit.env)+"\n\n## Layout")
}

// signinSection is the agent's note on the sign-in the app starts with.
const signinSection = `## Sign-in

This app signs people in by emailed link (%[1]srastrillo/auth%[1]s, ui's
sign-in screen on the stage shell), in %[1]sinternal/%[2]s/auth.go%[1]s.
Owned rows carry %[1]sOwner string%[1]s, the signed-in address, never
%[1]ssessions.UserID%[1]s (it reads (0, false) here). Owned pages go in
the %[1]sowned%[1]s group in %[1]sRoutes%[1]s; tests sign in with
%[1]ssignIn(t, handler, box, address)%[1]s. Set %[1]s%[3]s_SMTP_HOST%[1]s
and %[1]s%[3]s_SMTP_FROM%[1]s to send mail, or links are logged. Off
CARLOS, set %[1]s%[3]s_INSTANCE_KEY%[1]s to keep links across restarts.`

// formatGo puts the imports the edits added where gofmt would, so the
// new app's own fmt-check passes.
func formatGo(files map[string]string) error {
	for path, source := range files {
		if !strings.HasSuffix(path, ".go") {
			continue
		}
		formatted, err := format.Source([]byte(source))
		if err != nil {
			return fmt.Errorf("--auth=magic: %s does not parse: %w", path, err)
		}
		files[path] = string(formatted)
	}
	return nil
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return line
}
