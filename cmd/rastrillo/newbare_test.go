package main

import (
	"go/format"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/locales"
)

// walkFiles is every file under dir, slash-separated and relative.
func walkFiles(t *testing.T, dir string) []string {
	t.Helper()
	var got []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry,
		err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		got = append(got, filepath.ToSlash(rel))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	return got
}

// goIn runs go with args in dir and returns its combined output.
func goIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// bare is the small shape: what it writes is the whole promise, so the
// default scaffold's file set is pinned in full. A file that creeps in
// (a Makefile, a manifest/, a gen/) makes it a different, bigger app.
func TestBareWritesOnlyItsFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"--scaffold", "bare", "dash"}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		".gitignore",
		"README.md",
		"cmd/dash/main.go",
		"go.mod",
		"internal/dash/app.go",
		"internal/dash/app_test.go",
		"internal/dash/icons/icons.go",
		"internal/dash/locale.go",
		"internal/dash/render.go",
		"internal/dash/static/anchor.js",
		"internal/dash/static/busy.js",
		"internal/dash/static/calendar.js",
		"internal/dash/static/copy.js",
		"internal/dash/static/datetime.js",
		"internal/dash/static/fiesta.js",
		"internal/dash/static/light.js",
		"internal/dash/static/live.js",
		"internal/dash/static/rastrillo.js",
		"internal/dash/static/select.js",
		"internal/dash/static/shell.css",
		"internal/dash/static/shell.js",
		"internal/dash/static/theme.css",
		"internal/dash/static/tokens.css",
		"internal/dash/static/transition.js",
		"internal/dash/templates/errors.html",
		"internal/dash/templates/index.html",
		"internal/dash/templates/layout.html",
	}
	if got := walkFiles(t, "dash"); !slices.Equal(got, want) {
		t.Errorf("bare wrote\n%s\nwant\n%s", strings.Join(got, "\n"),
			strings.Join(want, "\n"))
	}
	// The scaffold's own Go has to be what gofmt would write, or the
	// first gofmt -l in someone's editor flags a file they never touched.
	for _, rel := range []string{"cmd/dash/main.go", "internal/dash/app.go",
		"internal/dash/app_test.go", "internal/dash/locale.go",
		"internal/dash/render.go"} {
		checkGofmt(t, rel, readScaffold(t, "dash", rel))
	}
}

// checkGofmt fails unless src is what gofmt would write.
func checkGofmt(t *testing.T, rel, src string) {
	t.Helper()
	out, err := format.Source([]byte(src))
	if err != nil || string(out) != src {
		t.Errorf("%s is not gofmt-clean (err %v)", rel, err)
	}
}

// An icon set whose licence needs attribution brings its file; one
// that does not brings none.
func TestBareWritesTheIconAttribution(t *testing.T) {
	t.Chdir(t.TempDir())
	err := runNew([]string{"--scaffold", "bare", "--icons=font-awesome",
		"dash"})
	if err != nil {
		t.Fatal(err)
	}
	usual := []string{".gitignore", "README.md", "go.mod"}
	var extra []string
	for _, file := range walkFiles(t, "dash") {
		if !strings.Contains(file, "/") && !slices.Contains(usual, file) {
			extra = append(extra, file)
		}
	}
	if len(extra) != 1 {
		t.Errorf("font-awesome bare app has root files %v besides the "+
			"usual three, want exactly its attribution file", extra)
	}
}

// go.mod is the point of bare for someone who found the full scaffold
// too much: Rastrillo and what Rastrillo itself needs, with no router
// library and no ORM of its own, and no tool line (nothing in a bare
// app's gate runs the CLI).
func TestBareGoModRequiresOnlyRastrillo(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds and tidies a whole app")
	}
	setSandboxGoEnv(t)
	t.Chdir(t.TempDir())
	dir := scaffoldWithReplace(t, "dash", "--scaffold", "bare")
	mod := readScaffold(t, dir, "go.mod")
	for _, banned := range []string{"go-chi", "gorm.io", "\ntool "} {
		if strings.Contains(mod, banned) {
			t.Errorf("go.mod contains %q after tidy:\n%s", banned, mod)
		}
	}
}

// The first go test and go vet are green, and the two tests the shape
// ships actually ran: a 200 with the fingerprinted stylesheet, and a 404
// for a path that is no route.
func TestBarePassesItsOwnTests(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds, tidies and tests a whole app")
	}
	setSandboxGoEnv(t)
	t.Chdir(t.TempDir())
	dir := scaffoldWithReplace(t, "dash", "--scaffold", "bare")
	out, err := goTestIn(dir, "-v", "./...")
	if err != nil {
		t.Fatalf("go test ./...:\n%s", out)
	}
	for _, name := range []string{"TestIndexRenders", "TestUnknownPathIs404"} {
		if !strings.Contains(out, "--- PASS: "+name) {
			t.Errorf("%s did not run and pass:\n%s", name, out)
		}
	}
	if vet, err := goIn(dir, "vet", "./..."); err != nil {
		t.Fatalf("go vet ./...:\n%s", vet)
	}
}

// The sidebar, console and topbar shells have a phone index, so bare
// gives them the same second page single does, the section or the
// Menu page; its route, handler and test come with it.
func TestBareTwoPageShellsHaveTheirSecondPage(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds, tidies and tests a whole app")
	}
	for shell, test := range map[string]string{
		"sidebar": "TestOverviewRenders", "topbar": "TestMenuRenders",
	} {
		t.Run(shell, func(t *testing.T) {
			setSandboxGoEnv(t)
			t.Chdir(t.TempDir())
			dir := scaffoldWithReplace(t, "dash", "--scaffold", "bare",
				"--shell="+shell)
			out, err := goTestIn(dir, "-v", "./...")
			if err != nil {
				t.Fatalf("go test ./...:\n%s", out)
			}
			if !strings.Contains(out, "--- PASS: "+test) {
				t.Errorf("%s did not run and pass:\n%s", test, out)
			}
		})
	}
}

// The locale lives in the app package, so the app's own tests link the
// pack newPages' CheckConfig needs when the package loads.
func TestBareWithLocale(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds, tidies and tests a whole app")
	}
	for _, code := range []string{"es", "pt-PT", "zh-Hans"} {
		t.Run(code, func(t *testing.T) {
			setSandboxGoEnv(t)
			t.Chdir(t.TempDir())
			dir := scaffoldWithReplace(t, "dash", "--scaffold", "bare",
				"--locale="+code)
			src := readScaffold(t, dir, "internal", "dash", "locale.go")
			if !strings.Contains(src, `const Locale = "`+code+`"`) {
				t.Errorf("locale.go does not set Locale to %q:\n%s", code, src)
			}
			packs, err := locales.PacksFor([]string{code})
			if err != nil {
				t.Fatal(err)
			}
			for _, pack := range packs {
				line := `_ "amadan.net/rastrillo/rastrillo/locales/` +
					pack + `"`
				if !strings.Contains(src, line) {
					t.Errorf("locale.go does not import the %s pack:\n%s",
						pack, src)
				}
			}
			if out, err := goTestIn(dir, "./..."); err != nil {
				t.Fatalf("go test ./... with --locale=%s:\n%s", code, out)
			}
		})
	}
}

// --ux only chooses the conventions written into AGENTS.md, and bare
// writes none. The typed flag counts even at its default value.
func TestBareRefusesUX(t *testing.T) {
	for _, args := range [][]string{{"--ux", "standard"}, {"--ux=considered"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			refusedBeforeWriting(t, "--ux does nothing with --scaffold "+
				"bare, which writes no AGENTS.md: leave it out",
				append([]string{"--scaffold", "bare"}, args...)...)
		})
	}
}

// refusedBeforeWriting runs rastrillo new x in a fresh directory and
// checks it refused with want before x existed: a half-made x makes
// the next, corrected, run fail with "already exists".
func refusedBeforeWriting(t *testing.T, want string, args ...string) {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := runNew(append(args, "x")); err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
	if _, err := os.Stat("x"); err == nil {
		t.Fatal("x exists after a refused flag")
	}
}

// bare's first line says what it is, so the shorter file list under it
// does not read as a failure.
func TestBareSaysWhatItScaffolded(t *testing.T) {
	t.Chdir(t.TempDir())
	out := captureStdout(t, func() {
		if err := runNew([]string{"--scaffold", "bare", "dash"}); err != nil {
			t.Fatal(err)
		}
	})
	first, _, _ := strings.Cut(out, "\n")
	if first != "rastrillo new: scaffolded dash/ (bare: a page and a route)" {
		t.Errorf("first line = %q", first)
	}
	if !strings.Contains(out, "go test ./...") {
		t.Errorf("summary does not say how to run the tests:\n%s", out)
	}
}

// dev regenerates before it builds, and an app with nothing to generate
// from must not grow a gen/ for the pleasure of it. Anything that is
// generation input (manifest/, actions/) or output (gen/) keeps the step:
// dropping it for an app that has them would build against stale code.
func TestDevLeavesABareAppBare(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds, tidies and builds whole apps")
	}
	setSandboxGoEnv(t)
	// -buildvcs=false: rebuildApp's go build stamps VCS info, and a temp
	// directory under someone else's .git fails that before compiling.
	t.Setenv("GOFLAGS", "-mod=mod -buildvcs=false")
	t.Chdir(t.TempDir())
	build := func(dir, name string) {
		t.Helper()
		bin := filepath.Join(t.TempDir(), name)
		if err := rebuildApp(dir, "./cmd/"+name, bin); err != nil {
			t.Fatalf("rebuild %s: %v", name, err)
		}
	}
	exists := func(parts ...string) bool {
		_, err := os.Stat(filepath.Join(parts...))
		return err == nil
	}

	bare := scaffoldWithReplace(t, "plain", "--scaffold", "bare")
	build(bare, "plain")
	if exists(bare, "gen") {
		t.Error("a rebuild wrote gen/ into a bare app")
	}

	// actions/ and no manifest/: generate copies the actions, so it runs.
	actions := "//go:build rastrillo_actions\n\npackage actions\n\n" +
		"import (\n\t\"net/http\"\n\n" +
		"\t\"amadan.net/rastrillo/rastrillo\"\n)\n\n" +
		"func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, " +
		"r *http.Request) {}\n"
	mustWrite(t, filepath.Join(bare, "actions", "index.GET.go"), actions)
	build(bare, "plain")
	if !exists(bare, "gen", "router.go") {
		t.Error("a rebuild with actions/ and no manifest/ did not run generate")
	}

	// single has manifest/ and gen/: a deleted generated file comes back.
	single := scaffoldWithReplace(t, "full")
	if err := os.Remove(filepath.Join(single, "gen", "api.go")); err != nil {
		t.Fatal(err)
	}
	build(single, "full")
	if !exists(single, "gen", "api.go") {
		t.Error("a rebuild of a single app did not run generate")
	}
}

// --design-system=false is bare for an app that writes its own HTML or
// JSON: no templates, static files, icons, render.go or attribution.
// Any of them left behind is a design system the app did not ask for
// and has to find and delete by hand.
func TestBarePlainWritesNoDesignSystem(t *testing.T) {
	t.Chdir(t.TempDir())
	err := runNew([]string{"--scaffold", "bare", "--design-system=false",
		"dash"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		".gitignore",
		"README.md",
		"cmd/dash/main.go",
		"go.mod",
		"internal/dash/app.go",
		"internal/dash/app_test.go",
		"internal/dash/locale.go",
	}
	if got := walkFiles(t, "dash"); !slices.Equal(got, want) {
		t.Errorf("plain bare wrote\n%s\nwant\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// Each of these names something only the design system defines: the
	// app would not compile.
	designOnly := []string{"ErrorPage", "pages.", "render(", "assets.",
		"templates/"}
	for _, rel := range []string{"cmd/dash/main.go", "internal/dash/app.go",
		"internal/dash/app_test.go", "internal/dash/locale.go"} {
		src := readScaffold(t, "dash", rel)
		checkGofmt(t, rel, src)
		for _, gone := range designOnly {
			if strings.Contains(src, gone) {
				t.Errorf("%s mentions %q, which a plain app does not "+
					"have:\n%s", rel, gone, src)
			}
		}
	}
	readme := readScaffold(t, "dash", "README.md")
	if strings.Contains(readme, "templates/") {
		t.Errorf("README.md points at a templates/ the app does not "+
			"have:\n%s", readme)
	}
}

// The plain app's own tests are green, ran, and check the page is HTML
// in the app's language; vet passes with the browser and perf tags too,
// so a tagged file nobody builds by default cannot hide a break.
func TestBarePlainPassesItsOwnTests(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds, tidies and tests a whole app")
	}
	setSandboxGoEnv(t)
	t.Chdir(t.TempDir())
	dir := scaffoldWithReplace(t, "dash", "--scaffold", "bare",
		"--design-system=false", "--locale=es")
	out, err := goTestIn(dir, "-v", "./...")
	if err != nil {
		t.Fatalf("go test ./...:\n%s", out)
	}
	for _, name := range []string{"TestIndexRenders", "TestUnknownPathIs404"} {
		if !strings.Contains(out, "--- PASS: "+name) {
			t.Errorf("%s did not run and pass:\n%s", name, out)
		}
	}
	vet, err := goIn(dir, "vet", "-tags", "browser,perf", "./...")
	if err != nil {
		t.Fatalf("go vet -tags browser,perf ./...:\n%s", vet)
	}
}

// The four flags that only choose design-system files do nothing with
// it off, so each is refused by name, even typed at its default value,
// and before the app's directory exists.
func TestDesignFlagsRefusedWithoutDesignSystem(t *testing.T) {
	for _, flag := range []string{"icons=lucide", "icon-delivery=inline",
		"theme=day", "shell=column"} {
		name, _, _ := strings.Cut(flag, "=")
		t.Run(name, func(t *testing.T) {
			refusedBeforeWriting(t, "--"+name+" does nothing with "+
				"--design-system=false: leave it out",
				"--scaffold", "bare", "--design-system=false", "--"+flag)
		})
	}
}

// single and accounts build their pages from the design system, so
// only bare may leave it out. Typed --design-system=true is fine.
func TestDesignSystemOffRefusedForSingleAndAccounts(t *testing.T) {
	for _, args := range [][]string{
		{"--scaffold", "single"}, {"--scaffold", "accounts"}, nil,
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			refusedBeforeWriting(t, "--design-system=false works with "+
				"--scaffold bare only: single and accounts build their "+
				"pages from the design system",
				append(args, "--design-system=false")...)
		})
	}
	t.Chdir(t.TempDir())
	err := runNew([]string{"--scaffold", "bare", "--design-system=true",
		"--theme=plain", "x"})
	if err != nil {
		t.Errorf("bare with the design system typed on: %v", err)
	}
}

// doctor on a plain bare app has no vendored files to compare, and that
// is not an error: it says so in one line and still runs the template
// and locale checks, --probe's build-and-run included. Failing there
// left a plain app no way to check its locale setup.
func TestDoctorChecksAPlainBareApp(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds, tidies and builds a whole app")
	}
	dir := scaffoldApp(t, "dash", "--scaffold", "bare", "--design-system=false")
	for _, args := range [][]string{{dir}, {"--probe", dir}, {"--fix", dir}} {
		var err error
		out := captureStdout(t, func() { err = runDoctor(args) })
		if err != nil {
			t.Fatalf("doctor %v: %v\n%s", args, err, out)
		}
		if !strings.Contains(out, doctorNoDesignMsg) {
			t.Errorf("doctor %v does not say the design system is "+
				"absent:\n%s", args, out)
		}
		if args[0] == "--fix" {
			continue
		}
		for _, want := range []string{"Templates:", "Locales:"} {
			if !strings.Contains(out, want) {
				t.Errorf("doctor %v skipped %q:\n%s", args, want, out)
			}
		}
		if args[0] == "--probe" && !strings.Contains(out, `"ok":true`) {
			t.Errorf("doctor --probe did not run the app's locale "+
				"check:\n%s", out)
		}
	}
	// No comparison, so no version to be skewed against.
	plain := &report{noDesign: true, appVersion: "v0.1.0",
		cliVersion: "v9.9.9"}
	if err := plain.exit(); err != nil {
		t.Errorf("a plain app on another version: exit %v, want none", err)
	}
}
