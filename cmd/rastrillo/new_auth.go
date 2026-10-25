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

// applyMagicAuth turns a freshly assembled scaffold into one that signs
// people in by emailed link (rastrillo/auth, with ui's sign-in screen).
//
// It is the recipe first applied by hand to a fresh `rastrillo new` app
// and kept only once that app's whole `make ci` passed, with a test
// signing in through the emailed link and the perf test measuring an
// owned page behind that sign-in (discussion #97). Every agent in the
// skilleval tasks needed sign-in and read examples/notes most to build
// it; an app that starts with it gives them nothing to look up.
//
// It edits the assembled files rather than forking every template, so
// the default scaffold stays the one template set. Each edit names the
// text it expects, and a scaffold whose templates moved fails here
// loudly instead of writing an app that does not build.
func applyMagicAuth(files map[string]string, name, pkg, appDir string) error {
	testDir := filepath.Join(name, "internal", pkg+"test")
	env := strings.ToUpper(pkg)
	var errs []string
	swap := func(path, old, new string) {
		s, ok := files[path]
		if !ok {
			errs = append(errs, fmt.Sprintf("%s: not in the scaffold", path))
			return
		}
		if !strings.Contains(s, old) {
			errs = append(errs, fmt.Sprintf("%s: expected %q", path, firstLine(old)))
			return
		}
		files[path] = strings.Replace(s, old, new, 1)
	}
	importAfter := func(path, after string, imports ...string) {
		var b strings.Builder
		b.WriteString(after)
		for _, imp := range imports {
			b.WriteString("\n\t\"" + imp + "\"")
		}
		swap(path, after, b.String())
	}
	fill := func(tmpl string) string {
		return strings.NewReplacer("__PKG__", pkg, "__NAME__", name, "__ENV__", env).Replace(tmpl)
	}

	// The schema: sessions before auth (auth's backfill reads the sessions
	// table), pow for the guard's nonces, then what was there.
	migrations := filepath.Join(appDir, "migrations.go")
	swap(migrations, "var BootSchema = migrate.Merge(apitoken.Schema, Schema)",
		"var BootSchema = migrate.Merge(sessions.Schema, auth.Schema, pow.Schema, apitoken.Schema, Schema)")
	importAfter(migrations, `"amadan.net/rastrillo/rastrillo/apitoken"`,
		"amadan.net/rastrillo/rastrillo/auth", "amadan.net/rastrillo/rastrillo/pow", "amadan.net/rastrillo/rastrillo/sessions")

	// The example model becomes owned: an Owner column holding the
	// signed-in address, which the owned seam filters on. The migration
	// and schema are what `rastrillo migration generate` writes for it.
	swap(filepath.Join(appDir, "models.go"), "type Note struct {\n\tID        int64\n",
		"type Note struct {\n\tID        int64\n\tOwner     string `gorm:\"index\"`\n")
	ownedTable := "CREATE TABLE `notes` (`id` integer PRIMARY KEY AUTOINCREMENT,`owner` text,`title` text,`body` text,`created_at` datetime,`updated_at` datetime);\n\nCREATE INDEX `idx_notes_owner` ON `notes`(`owner`);\n\n"
	plainTable := "CREATE TABLE `notes` (`id` integer PRIMARY KEY AUTOINCREMENT,`title` text,`body` text,`created_at` datetime,`updated_at` datetime);\n\n"
	swap(filepath.Join(appDir, "migrations", "0001_init.sql"), plainTable, ownedTable)
	swap(filepath.Join(appDir, "migrations", "schema.sql"), plainTable, ownedTable)

	// App and Router take the background group (the guard's and the
	// plugin's sweeps run on it, and shutdown stops it) and the mailer
	// (main's from the environment, a test's outbox).
	app := filepath.Join(appDir, "app.go")
	swap(app, "func App(d *db.DB, origin string, logger *slog.Logger) (*http.ServeMux, error) {",
		"func App(d *db.DB, origin string, bg *background.Group, mailer mail.Sender, logger *slog.Logger) (*http.ServeMux, error) {")
	swap(app, "r, err := Router(d, origin, logger)", "r, err := Router(d, origin, bg, mailer, logger)")
	swap(app, "func Router(d *db.DB, origin string, logger *slog.Logger) (chi.Router, error) {",
		"func Router(d *db.DB, origin string, bg *background.Group, mailer mail.Sender, logger *slog.Logger) (chi.Router, error) {")
	swap(app, "\ta := &app{db: d, logger: logger}\n", `	a := &app{db: d, logger: logger}
	au, guard, err := newAuth(d, origin, mailer)
	if err != nil {
		return nil, err
	}
	bg.Loop(context.Background(), 10*time.Minute, func() {
		guard.Sweep(time.Now())
		au.Sweep(time.Now())
	})
	a.auth = au
`)
	swap(app, "\tr.Use(csrf.Protect(origin))\n", "\tr.Use(csrf.Protect(origin))\n\tr.Use(a.signedIn)\n\tmountAuth(r, au)\n")
	swap(app, "\treturn r, nil\n}", `	// Owned routes go in this group: a signed-out visitor is sent to the
	// sign-in screen and comes back here after following the link.
	r.Group(func(r chi.Router) {
		r.Use(au.RequireSession)
		r.Get("/account", a.account)
	})
	return r, nil
}`)
	importAfter(app, `"amadan.net/rastrillo/rastrillo/csrf"`,
		"amadan.net/rastrillo/rastrillo/background", "amadan.net/rastrillo/rastrillo/mail")

	handlers := filepath.Join(appDir, "handlers.go")
	swap(handlers, "\tlogger *slog.Logger\n}", "\tlogger *slog.Logger\n\tauth   *auth.Auth\n}")
	importAfter(handlers, "import (", "amadan.net/rastrillo/rastrillo/auth")

	// The layout offers Sign out to a signed-in visitor and Sign in to
	// anyone else, on every page; render tells it which.
	swap(filepath.Join(appDir, "render.go"),
		"page[\"L\"], page[\"Locales\"], page[\"Return\"], page[\"Scheme\"] = req.L, req.Locales, req.Return, req.Scheme\n",
		"page[\"L\"], page[\"Locales\"], page[\"Return\"], page[\"Scheme\"] = req.L, req.Locales, req.Return, req.Scheme\n\tpage[\"SignedIn\"], _ = r.Context().Value(signedInKey{}).(bool)\n")
	layout := filepath.Join(appDir, "templates", "layout.html")
	if s, ok := files[layout]; ok {
		at := strings.Index(s, `{{block "scheme" .}}`)
		end := -1
		if at >= 0 {
			if menu := strings.Index(s[at:], "scheme-menu"); menu >= 0 {
				if e := strings.Index(s[at+menu:], "{{end}}"); e >= 0 {
					end = at + menu + e + len("{{end}}")
				}
			}
		}
		if end < 0 {
			errs = append(errs, layout+": no scheme menu block to sit beside")
		} else {
			files[layout] = s[:end] + `{{if opt $ "SignedIn"}}<form method="post" action="/signout"><button rst-btn>Sign out</button></form>{{else}}<a href="/signin" rst-btn>Sign in</a>{{end}}` + s[end:]
		}
	}

	main := filepath.Join(name, "cmd", name, "main.go")
	swap(main, fmt.Sprintf("mux, err := %s.App(d, origin, logger)", pkg),
		fmt.Sprintf("bg := &background.Group{}\n\tmux, err := %s.App(d, origin, bg, mail.FromEnv(%q, logger), logger)", pkg, env))
	swap(main, fmt.Sprintf("\t%s.Configure(&opts, mux, started)\n", pkg),
		fmt.Sprintf("\t%s.Configure(&opts, mux, started)\n\topts.Background = bg\n", pkg))
	importAfter(main, `"amadan.net/rastrillo/rastrillo/db"`,
		"amadan.net/rastrillo/rastrillo/background", "amadan.net/rastrillo/rastrillo/mail")

	// Every caller in the tests passes a group it stops and a mailer: the
	// harness an outbox a test can open links from, the rest a logger.
	harness := filepath.Join(testDir, "harness_test.go")
	swap(harness, "func newApp(t *testing.T) http.Handler {\n\tt.Helper()\n", `func newApp(t *testing.T) http.Handler {
	t.Helper()
	h, _ := newAppWithMail(t)
	return h
}

// newAppWithMail is newApp with the outbox its emailed links land in.
func newAppWithMail(t *testing.T) (http.Handler, *outbox) {
	t.Helper()
`)
	swap(harness, fmt.Sprintf("\tmux, err := %s.App(d, testOrigin, logger)\n", pkg),
		fmt.Sprintf("\tbg := &background.Group{}\n\tt.Cleanup(bg.Stop) // runs before d.Close: cleanups run last-in, first-out\n\tbox := &outbox{}\n\tmux, err := %s.App(d, testOrigin, bg, box, logger)\n", pkg))
	swap(harness, "\treturn mux\n}", "\treturn mux, box\n}")
	importAfter(harness, `"amadan.net/rastrillo/rastrillo/db"`, "amadan.net/rastrillo/rastrillo/background")

	browser := filepath.Join(testDir, "browser_test.go")
	swap(browser, fmt.Sprintf("\t\tmux, err := %s.App(d, origin, logger)\n", pkg),
		fmt.Sprintf("\t\tbg := &background.Group{}\n\t\tt.Cleanup(bg.Stop)\n\t\tmux, err := %s.App(d, origin, bg, mail.Logged(logger), logger)\n", pkg))
	importAfter(browser, `"amadan.net/rastrillo/rastrillo/db"`,
		"amadan.net/rastrillo/rastrillo/background", "amadan.net/rastrillo/rastrillo/mail")

	perf := filepath.Join(testDir, "perf_test.go")
	swap(perf, fmt.Sprintf("\tr, err := %s.Router(d, testOrigin, logger)\n", pkg),
		fmt.Sprintf("\tbg := &background.Group{}\n\tbox := &outbox{}\n\tr, err := %s.Router(d, testOrigin, bg, box, logger)\n", pkg))
	swap(perf, fmt.Sprintf("\t%s.Configure(&opts, %s.Mux(r, apiTree), time.Now())\n", pkg, pkg),
		fmt.Sprintf("\t%s.Configure(&opts, %s.Mux(r, apiTree), time.Now())\n\topts.Background = bg // closeAll stops it\n", pkg, pkg))
	swap(perf, "\tperftest.Screens(t, perftest.ScreenConfig{Routes: r, Handler: h})", `	perftest.Screens(t, perftest.ScreenConfig{Routes: r, Handler: h, Signin: func(c *http.Client, base string) {
		signInOver(t, c, base, box, "perf@example.com")
	}})`)
	swap(perf, fmt.Sprintf("\t\t\tmux, err := %s.App(d, testOrigin, logger)\n\t\t\tif err != nil {\n\t\t\t\td.Close()\n", pkg),
		fmt.Sprintf("\t\t\tbg := &background.Group{}\n\t\t\tmux, err := %s.App(d, testOrigin, bg, mail.Logged(logger), logger)\n\t\t\tif err != nil {\n\t\t\t\tbg.Stop()\n\t\t\t\td.Close()\n", pkg))
	swap(perf, fmt.Sprintf("\t\t\t%s.Configure(&opts, mux, started)\n", pkg),
		fmt.Sprintf("\t\t\t%s.Configure(&opts, mux, started)\n\t\t\topts.Background = bg // closeAll stops it\n", pkg))
	importAfter(perf, `"amadan.net/rastrillo/rastrillo/db"`,
		"amadan.net/rastrillo/rastrillo/background", "amadan.net/rastrillo/rastrillo/mail")

	// The perf inventory: pow's assets and the two link landings are not
	// screens. /signin and /account are, and are measured.
	swap(filepath.Join(name, ".rastrillo", "budgets.txt"), budgetsTxtTemplate, budgetsTxtTemplate+
		"perf  GET /pow/*          skip  proof-of-work script and worker assets, not a screen\n"+
		"perf  GET /auth/callback  skip  keymail landing: answers only with a pending sign-in\n"+
		"perf  GET /auth/verify    skip  an emailed link's landing: answers only with its token\n")

	stage, ok := ui.Layout("stage")
	if !ok {
		errs = append(errs, "ui has no stage layout for the sign-in screen")
	}
	files[filepath.Join(appDir, "auth.go")] = fill(scaffoldFile("auth.go.tmpl"))
	files[filepath.Join(appDir, "templates", "signin.html")] = scaffoldFile("signin.html")
	files[filepath.Join(appDir, "templates", "stage.html")] = string(stage)
	files[filepath.Join(testDir, "signin_test.go")] = fill(scaffoldFile("signin_test.go.tmpl"))

	// The agent working in the app finds the sign-in it starts with.
	section := fmt.Sprintf(`## Sign-in

This app signs people in by emailed link (%[1]srastrillo/auth%[1]s, ui's
sign-in screen on the stage shell), wired in %[1]sinternal/%[2]s/auth.go%[1]s.
Owned rows carry %[1]sOwner string%[1]s, the signed-in address; read and
write them only through %[1]sa.owned(r)%[1]s, never %[1]ssessions.UserID%[1]s
(it reads (0, false) here). Owned routes go in the
%[1]sau.RequireSession%[1]s group in %[1]sRouter%[1]s. Tests sign in with
%[1]ssignIn(t, h, box, address)%[1]s from %[1]ssignin_test.go%[1]s. Set
%[1]s%[3]s_SMTP_HOST%[1]s and %[1]s%[3]s_SMTP_FROM%[1]s to send real mail;
without them links are logged.

## Layout`, "`", pkg, env)
	swap(filepath.Join(name, "AGENTS.md"), "\n## Layout", "\n"+section)

	if len(errs) > 0 {
		return fmt.Errorf("--auth=magic: the scaffold has moved under it:\n  %s", strings.Join(errs, "\n  "))
	}
	// The edits add imports out of order; formatting puts them where gofmt
	// would, so the new app's own fmt-check passes.
	for path, s := range files {
		if !strings.HasSuffix(path, ".go") {
			continue
		}
		b, err := format.Source([]byte(s))
		if err != nil {
			return fmt.Errorf("--auth=magic: %s does not parse: %w", path, err)
		}
		files[path] = string(b)
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
