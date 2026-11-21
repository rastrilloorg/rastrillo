package apppackage_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/apppackage"
)

// needs are the names rastrillo migration's loader reads.
var needs = []string{"Schema", "BootSchema", "Models"}

// writeLayout writes a module named demo whose files are given
// relative to the module root, and returns the root.
func writeLayout(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files["go.mod"] = "module demo\n"
	for path, body := range files {
		path = filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// declaring is one package that declares every name the loader needs,
// split across two files the way a scaffold is.
func declaring(name string) map[string]string {
	return map[string]string{
		"internal/" + name + "/migrations.go": "package " + name +
			"\n\nvar Schema = 1\n\nvar BootSchema = 2\n",
		"internal/" + name + "/models.go": "package " + name +
			"\n\nvar Models = []any{}\n",
	}
}

// twoApps has two packages that each declare everything the loader
// needs, so only something else can settle which is the app.
func twoApps() map[string]string {
	files := declaring("blog")
	for path, body := range declaring("wiki") {
		files[path] = body
	}
	return files
}

// patrix is Patrix's internal/: the app package beside end-to-end and
// helper packages with no app package of the same name, one with a
// Schema of its own and one declaring Models only in a test file.
func patrix() map[string]string {
	files := declaring("patrix")
	files["internal/patrix/app.go"] = "package patrix\n"
	files["internal/patrixtest/access_test.go"] =
		"package patrixtest\n\nvar Models = []any{}\n"
	files["internal/hostedtest/harness.go"] =
		"package hostedtest\n\nvar Schema = 1\n"
	files["internal/admintest/lists_test.go"] = "package admintest\n"
	files["internal/commentstest/comments_test.go"] =
		"//go:build browser\n\npackage commentstest\n"
	return files
}

// find runs Find with no override and fails the test unless it
// chooses want.
func find(t *testing.T, files map[string]string, want string) {
	t.Helper()
	if got, err := apppackage.Find(writeLayout(t, files), "", needs); err !=
		nil || got != want {
		t.Errorf("found %q, %v; want %s", got, err, want)
	}
}

// refuses runs Find with no override and fails the test unless it
// refuses with an error naming every one of wants.
func refuses(t *testing.T, files map[string]string, wants ...string) {
	t.Helper()
	got, err := apppackage.Find(writeLayout(t, files), "", needs)
	if err == nil {
		t.Errorf("chose %s, want a refusal naming %v", got, wants)
		return
	}
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to name %s", err, want)
		}
	}
}

func TestFindsPatrixAmongHelperPackages(t *testing.T) {
	find(t, patrix(), "patrix")
}

// An older app's separate internal/<pkg>test is skipped, and an app
// whose own name ends in test is still found.
func TestSkipsOnlyAnOlderAppsTests(t *testing.T) {
	for _, names := range [][]string{
		{"notes"}, {"notes", "notestest"}, {"contest"},
		{"contest", "contesttest"},
	} {
		files := map[string]string{}
		for _, name := range names {
			files["internal/"+name+"/a.go"] = "package " + name + "\n"
		}
		find(t, files, names[0])
	}
}

func TestOverrideWinsAndMustNameADirectoryThere(t *testing.T) {
	dir := writeLayout(t, patrix())
	for override, want := range map[string]string{
		"hostedtest": "hostedtest", "patrix": "patrix",
		"nosuch": "", "../patrix": "", "internal/patrix": "", ".": "",
		"..": "",
	} {
		got, err := apppackage.Find(dir, override, needs)
		if want != "" && (err != nil || got != want) {
			t.Errorf("%s: found %q, %v; want %s", override, got, err, want)
		}
		if want == "" && (err == nil ||
			!strings.Contains(err.Error(), fmt.Sprintf("%q", override))) {
			t.Errorf("%s: found %q, %v; want a refusal naming it",
				override, got, err)
		}
	}
}

func TestRefusesNamingCandidatesAndTheFlag(t *testing.T) {
	refuses(t, twoApps(), "blog", "wiki", "-package")
	refuses(t, map[string]string{
		"internal/alpha/a.go": "package alpha\n",
		"internal/beta/b.go":  "package beta\n",
	}, "alpha", "beta", "-package")
}

// Find reads the files the loader's build compiles: GOFLAGS tags pick
// between two candidates, and an assembly file is no Go file to parse.
func TestSeesTheFilesTheBuildSees(t *testing.T) {
	files := map[string]string{
		"internal/blog/migrations.go": "//go:build !custom\n\n" +
			"package blog\n\nvar Schema, BootSchema = 1, 2\n",
		"internal/blog/models.go": "package blog\n\nvar Models = 3\n",
		"internal/wiki/migrations.go": "//go:build custom\n\n" +
			"package wiki\n\nvar Schema, BootSchema = 1, 2\n",
		"internal/wiki/models.go":   "package wiki\n\nvar Models = 3\n",
		"internal/wiki/asm_amd64.s": "TEXT ·nothing(SB),0,$0\n",
	}
	for tags, want := range map[string]string{"": "blog", "custom": "wiki"} {
		t.Setenv("GOFLAGS", "-tags="+tags)
		find(t, files, want)
	}
}

// Find does not reproduce Go's cgo rules: a package with a file that
// imports C, in either spelling and whatever its body, is one it
// cannot judge, so it refuses with cgo on or off. -package still works.
func TestRefusesPackagesThatUseCgo(t *testing.T) {
	for _, spelling := range []string{`"C"`, "`C`"} {
		for _, body := range []string{"var = 1",
			"var Schema, BootSchema, Models = 1, 2, 3"} {
			files := declaring("blog")
			files["internal/wiki/doc.go"] = "package wiki\n"
			files["internal/wiki/cgo.go"] = "package wiki\n\nimport " +
				spelling + "\n\n" + body + "\n"
			for _, cgo := range []string{"0", "1"} {
				t.Setenv("CGO_ENABLED", cgo)
				refuses(t, files, "wiki", "cgo.go", "-package")
			}
			got, err := apppackage.Find(writeLayout(t, files), "wiki", needs)
			if err != nil || got != "wiki" {
				t.Errorf("-package wiki: found %q, %v; want wiki", got, err)
			}
		}
	}
}

// A package Find cannot read might be the app, so it refuses to choose
// rather than settle on the other candidate.
func TestRefusesWhenANonTestFileCannotBeRead(t *testing.T) {
	for file, body := range map[string]string{
		"broken.go":     "package wiki\n\nvar = 1\n",
		"other.go":      "package other\n",
		"import.go":     "package wiki\n\nimport \"unterminated\n",
		"migrations.go": "//go:build (\n\npackage wiki\n\nvar Schema = 1\n",
	} {
		files := twoApps()
		files["internal/wiki/"+file] = body
		refuses(t, files, "wiki", file, "-package")
	}
	// The broken constraint on wiki's only file leaves go list no file
	// to report at all, which must not read as "declares nothing".
	refuses(t, map[string]string{
		"internal/blog/app.go": "package blog\n\n" +
			"var Schema, BootSchema, Models = 1, 2, 3\n",
		"internal/wiki/app.go": "//go:build (\n\npackage wiki\n\n" +
			"var Schema, BootSchema, Models = 1, 2, 3\n",
	}, "wiki", "app.go", "-package")
}

// Test files are not part of the package the loader imports: one that
// declares every name, does not parse, or names another package
// changes nothing, and neither does a package of tests alone.
func TestIgnoresTestFiles(t *testing.T) {
	files := patrix()
	files["internal/e2etest/declares_test.go"] =
		"package e2etest\n\nvar Schema, BootSchema, Models = 1, 2, 3\n"
	files["internal/patrix/broken_test.go"] = "package patrix\n\nvar = 1\n"
	files["internal/hostedtest/import_test.go"] =
		"package hostedtest\n\nimport \"unterminated\n"
	files["internal/themestest/theme.go"] = "package themestest\n"
	files["internal/themestest/other_test.go"] = "package other\n"
	find(t, files, "patrix")
}
