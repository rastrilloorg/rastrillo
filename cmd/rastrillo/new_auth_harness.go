package main

import "path/filepath"

// The edits to the scaffold's tests, apart from new_auth.go to keep each
// file short.

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
	api := filepath.Join(edit.testDir, "api_test.go")
	edit.swap(api, edit.filled("\tmux, err := %s.New(\n"+
		"\t\tdatabase, testOrigin, slog.New(slog.DiscardHandler))\n"),
		"\tlogger := slog.New(slog.DiscardHandler)\n"+
			"\tgroup := &background.Group{}\n\tt.Cleanup(group.Stop)\n"+
			edit.filled("\tmux, err := %s.New(\n"+
				"\t\tdatabase, testOrigin, group, mail.Logged(logger), "+
				"logger)\n"))
	edit.importAfter(api, `"amadan.net/rastrillo/rastrillo/db"`,
		"background", "mail")
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
