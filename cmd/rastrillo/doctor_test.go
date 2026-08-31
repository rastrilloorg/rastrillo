package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"amadan.net/rastrillo/rastrillo/ui"
)

// doctorApp writes the smallest tree doctor recognises as an app: a
// go.mod, an internal/<pkg>/static with the vendored files in it, and
// the pin test beside it. Hand-built rather than scaffolded so a test
// can put one thing out of place and change nothing else.
func doctorApp(t *testing.T, version, theme string) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), fmt.Sprintf(
		"module demoapp\n\ngo 1.24\n\nrequire (\n\t%s %s\n\tgorm.io/gorm v1.31.2\n)\n",
		rastrilloModule, version))

	assets, ok := ui.VendoredAssets(theme)
	if !ok {
		t.Fatalf("unknown theme %q", theme)
	}
	for name, body := range assets {
		mustWrite(t, filepath.Join(dir, "internal", "demoapp", "static", name), string(body))
	}
	mustWrite(t, filepath.Join(dir, "internal", "demoapptest", "vendored_test.go"),
		fmt.Sprintf(`package demoapptest

func TestVendoredAssetsMatchTheLibrary(t *testing.T) {}

const vendoredTheme = %q

var vendoredIsMine = map[string]bool{
	// "tokens.css": true,
}
`, theme))
	return dir
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exitCode(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	var ex exitError
	if errors.As(err, &ex) {
		return ex.code
	}
	t.Fatalf("not an exit error: %v", err)
	return -1
}

// TestDoctorIsCleanOnAFreshScaffold is the baseline claim: run against
// what `rastrillo new` just wrote, doctor finds nothing. Everything
// else in this file is a departure from this state, so if this is
// wrong every other result is meaningless.
func TestDoctorIsCleanOnAFreshScaffold(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"freshapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	dir, err := filepath.Abs("freshapp")
	if err != nil {
		t.Fatal(err)
	}
	rep, err := diagnose(dir, "")
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	if rep.drifted() || rep.skewed() {
		t.Fatalf("a fresh scaffold is not clean:\n%s", printed(rep, false))
	}
	if rep.theme != "day" || rep.themeFrom != "pinned by the app" {
		t.Errorf("theme %q from %q, want day pinned by the app", rep.theme, rep.themeFrom)
	}
	if len(rep.files) != len(ui.VendoredNames()) {
		t.Errorf("reported %d files, the library vendors %d", len(rep.files), len(ui.VendoredNames()))
	}
	for _, f := range rep.files {
		if f.state != fileOK {
			t.Errorf("%s: state %v on a fresh scaffold", f.name, f.state)
		}
	}
}

// TestDoctorNamesTheDriftedFile: the whole value of the tool is saying
// WHICH file and HOW, so both are asserted rather than the exit code
// alone.
func TestDoctorNamesTheDriftedFile(t *testing.T) {
	dir := doctorApp(t, rastrilloVersion(), "day")
	path := filepath.Join(dir, "internal", "demoapp", "static", "tokens.css")
	mustWrite(t, path, "/* hand edit */\n"+string(ui.TokensCSS()))

	rep, err := diagnose(dir, "")
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	if !rep.drifted() {
		t.Fatal("an edited tokens.css is not reported as drift")
	}
	if got := exitCode(t, rep.exit()); got != exitDrift {
		t.Errorf("exit %d, want %d", got, exitDrift)
	}
	out := printed(rep, false)
	for _, want := range []string{"drift    tokens.css", "first difference at line 1", "/* hand edit */", "--fix"} {
		if !strings.Contains(out, want) {
			t.Errorf("report does not contain %q:\n%s", want, out)
		}
	}
	// And it must not implicate the files that are fine.
	if strings.Contains(out, "drift    select.js") {
		t.Errorf("an untouched file was reported as drift:\n%s", out)
	}
}

// TestDoctorFixReCopies closes the loop --fix exists for: after it, the
// same app is clean.
func TestDoctorFixReCopies(t *testing.T) {
	dir := doctorApp(t, rastrilloVersion(), "day")
	path := filepath.Join(dir, "internal", "demoapp", "static", "select.js")
	mustWrite(t, path, "// gone\n")

	rep, err := diagnose(dir, "")
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	var buf bytes.Buffer
	if err := rep.applyFix(&buf, false); err != nil {
		t.Fatalf("applyFix: %v", err)
	}
	if !strings.Contains(buf.String(), "re-copied") {
		t.Errorf("--fix did not say what it wrote:\n%s", buf.String())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, ui.SelectJS()) {
		t.Fatal("select.js was not restored to the library copy")
	}
	again, err := diagnose(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if again.drifted() {
		t.Fatalf("still drifted after --fix:\n%s", printed(again, false))
	}
}

// TestDoctorDoesNotCallAnAbsentFileDrift: the framework documents
// deleting datetime.js as a supported choice for an app with no date
// field, and examples/blog has never had the three scripts at all. An
// absent file gets a line saying the library ships it — not a failing
// exit code. --fix still adds it, because --fix is an explicit request.
func TestDoctorDoesNotCallAnAbsentFileDrift(t *testing.T) {
	dir := doctorApp(t, rastrilloVersion(), "day")
	path := filepath.Join(dir, "internal", "demoapp", "static", "datetime.js")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	rep, err := diagnose(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.drifted() {
		t.Fatalf("a deliberately deleted file was reported as drift:\n%s", printed(rep, false))
	}
	if got := exitCode(t, rep.exit()); got != 0 {
		t.Errorf("exit %d for an absent file, want 0", got)
	}
	out := printed(rep, false)
	for _, want := range []string{"absent   datetime.js", "not drift"} {
		if !strings.Contains(out, want) {
			t.Errorf("report does not contain %q:\n%s", want, out)
		}
	}
	var buf bytes.Buffer
	if err := rep.applyFix(&buf, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("--fix did not write the missing file: %v", err)
	}
}

// TestDoctorSeparatesSkewFromDrift is the trap the design turns on. An
// app on a different version has files that are correct for ITS
// version, and calling that damage is the failure mode that teaches
// people to ignore the tool.
func TestDoctorSeparatesSkewFromDrift(t *testing.T) {
	dir := doctorApp(t, "v0.0.1-not-this-one", "day")
	rep, err := diagnose(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if !rep.skewed() {
		t.Fatal("a different required version is not reported as skew")
	}
	if got := exitCode(t, rep.exit()); got != exitSkew {
		t.Errorf("exit %d, want %d — skew must not be reported as drift", got, exitSkew)
	}
	out := printed(rep, false)
	// The mismatch is the headline: it is on the second line, above
	// every file.
	head := strings.Split(out, "\n")
	if len(head) < 3 || !strings.Contains(head[1], "v0.0.1-not-this-one") {
		t.Errorf("the version mismatch is not the headline:\n%s", out)
	}
	if !strings.Contains(out, "these differences are expected") {
		t.Errorf("the report does not frame the differences as expected:\n%s", out)
	}
}

// TestDoctorFixRefusesAcrossVersions: copying this binary's assets into
// an app that compiles against an older module creates the exact fault
// doctor detects, and then reports it clean. It must not do that
// without being told to twice.
func TestDoctorFixRefusesAcrossVersions(t *testing.T) {
	dir := doctorApp(t, "v0.0.1-not-this-one", "day")
	path := filepath.Join(dir, "internal", "demoapp", "static", "tokens.css")
	mustWrite(t, path, "/* an old version's tokens */\n")

	rep, err := diagnose(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err = rep.applyFix(&buf, false)
	if got := exitCode(t, err); got != exitSkew {
		t.Fatalf("--fix across versions exited %d, want %d", got, exitSkew)
	}
	if !strings.Contains(buf.String(), "Refusing to --fix") {
		t.Errorf("--fix refused without saying so:\n%s", buf.String())
	}
	body, _ := os.ReadFile(path)
	if string(body) != "/* an old version's tokens */\n" {
		t.Fatal("--fix overwrote a file across a version mismatch")
	}

	// --force is the second telling.
	buf.Reset()
	if err := rep.applyFix(&buf, true); err != nil {
		t.Fatalf("--fix --force: %v", err)
	}
	body, _ = os.ReadFile(path)
	if !bytes.Equal(body, ui.TokensCSS()) {
		t.Fatal("--force did not re-copy")
	}
}

// TestDoctorLeavesADeliberateEditAlone: a file the app recorded as its
// own is not drift and is not overwritten. This is the whole reason
// vendoredIsMine exists.
func TestDoctorLeavesADeliberateEditAlone(t *testing.T) {
	dir := doctorApp(t, rastrilloVersion(), "day")
	pin := filepath.Join(dir, "internal", "demoapptest", "vendored_test.go")
	src, _ := os.ReadFile(pin)
	mustWrite(t, pin, strings.Replace(string(src), `// "tokens.css": true,`, `"tokens.css": true,`, 1))
	path := filepath.Join(dir, "internal", "demoapp", "static", "tokens.css")
	mustWrite(t, path, "/* mine, on purpose */\n")

	rep, err := diagnose(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.drifted() {
		t.Fatalf("a recorded deliberate edit was reported as drift:\n%s", printed(rep, false))
	}
	var buf bytes.Buffer
	if err := rep.applyFix(&buf, false); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if string(body) != "/* mine, on purpose */\n" {
		t.Fatal("--fix overwrote a file the app recorded as a deliberate edit")
	}
	if !strings.Contains(buf.String(), "left alone  tokens.css") {
		t.Errorf("--fix did not say it left the file alone:\n%s", buf.String())
	}
}

// TestDoctorReadsTheOlderPinShape: apps scaffolded before vendoredIsMine
// recorded a deliberate edit by DELETING the file's line from the pin's
// map literal. They followed the instruction they were given, and
// doctor must read it — otherwise its first act on an older app is to
// report that app's own decisions as damage.
func TestDoctorReadsTheOlderPinShape(t *testing.T) {
	dir := doctorApp(t, rastrilloVersion(), "day")
	mustWrite(t, filepath.Join(dir, "internal", "demoapptest", "vendored_test.go"),
		`package demoapptest

func TestVendoredAssetsMatchTheLibrary(t *testing.T) {
	const vendoredTheme = "day"
	for name, lib := range map[string][]byte{
		"theme.css":    themeCSS,
		"rastrillo.js": ui.ShimJS(),
		"select.js":    ui.SelectJS(),
		"datetime.js":  ui.DatetimeJS(),
	} {
	}
}
`)
	mustWrite(t, filepath.Join(dir, "internal", "demoapp", "static", "tokens.css"), "/* ours since 2025 */\n")

	rep, err := diagnose(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.drifted() {
		t.Fatalf("a deleted pin line was not read as a deliberate edit:\n%s", printed(rep, false))
	}
}

// TestDoctorDoesNotGuessAtACustomTheme is the false-positive rule made
// mechanical. A hand-written theme matches nothing shipped, and
// choosing the closest one to diff against would report an app's own
// design as damage.
func TestDoctorDoesNotGuessAtACustomTheme(t *testing.T) {
	dir := doctorApp(t, rastrilloVersion(), "day")
	// No pin file, so there is nothing to say which theme it should be.
	if err := os.Remove(filepath.Join(dir, "internal", "demoapptest", "vendored_test.go")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "internal", "demoapp", "static", "theme.css"),
		":root { --rst-bg: hotpink; }\n")

	rep, err := diagnose(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.drifted() {
		t.Fatalf("a custom theme was reported as drift:\n%s", printed(rep, false))
	}
	out := printed(rep, false)
	if !strings.Contains(out, "custom or drifted") {
		t.Errorf("the report does not say custom or drifted:\n%s", out)
	}
	// And --fix must not replace it with a guess.
	var buf bytes.Buffer
	if err := rep.applyFix(&buf, true); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "internal", "demoapp", "static", "theme.css"))
	if string(body) != ":root { --rst-bg: hotpink; }\n" {
		t.Fatal("--fix --force replaced a custom theme with a guess")
	}
}

// TestDoctorMatchesAThemeByContent: an app with no pin file, running a
// shipped theme unedited, gets told which one it is rather than
// "custom".
func TestDoctorMatchesAThemeByContent(t *testing.T) {
	dir := doctorApp(t, rastrilloVersion(), "signal")
	if err := os.Remove(filepath.Join(dir, "internal", "demoapptest", "vendored_test.go")); err != nil {
		t.Fatal(err)
	}
	rep, err := diagnose(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.theme != "signal" || rep.themeFrom != "matched by content" {
		t.Fatalf("theme %q from %q, want signal matched by content", rep.theme, rep.themeFrom)
	}
	if rep.drifted() {
		t.Fatalf("an unedited signal app is not clean:\n%s", printed(rep, false))
	}
}

// TestDoctorThemeFlagOverridesThePin: the escape hatch for an app that
// swapped its theme and did not update the pin.
func TestDoctorThemeFlagOverridesThePin(t *testing.T) {
	dir := doctorApp(t, rastrilloVersion(), "plain")
	// The pin says plain; the flag says the truth is elsewhere.
	rep, err := diagnose(dir, "signal")
	if err != nil {
		t.Fatal(err)
	}
	if rep.theme != "signal" || rep.themeFrom != "--theme" {
		t.Fatalf("theme %q from %q, want signal from --theme", rep.theme, rep.themeFrom)
	}
	if !rep.drifted() {
		t.Fatal("a plain theme compared against signal is not reported as differing")
	}
}

// TestDoctorFindsARootStaticDirectory: examples/blog and apps that
// predate the internal/<pkg>/static layout keep their vendored files at
// the root, and doctor is for exactly those apps.
func TestDoctorFindsARootStaticDirectory(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"),
		fmt.Sprintf("module oldapp\n\ngo 1.24\n\nrequire %s %s\n", rastrilloModule, rastrilloVersion()))
	mustWrite(t, filepath.Join(dir, "static", "tokens.css"), string(ui.TokensCSS()))

	rep, err := diagnose(dir, "day")
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	if rep.staticDir != "static" {
		t.Fatalf("static dir %q, want static", rep.staticDir)
	}
	// The files it does not have are absences, not findings — but
	// tokens.css, the one that matters, is clean.
	for _, f := range rep.files {
		if f.name == "tokens.css" && f.state != fileOK {
			t.Errorf("tokens.css: state %v, want ok", f.state)
		}
	}
}

// TestDoctorSaysWhatIsTrueOfAnAppWithNothingVendored: the failure has
// to name what it looked for, because "no" without a reason sends
// someone hunting through their own directory layout — and it must not
// doubt the app. examples/notes is manifest-shaped, has no static/ at
// all, and is a perfectly real rastrillo app; the honest answer there
// is "nothing to compare", not "is this a rastrillo app?".
func TestDoctorSaysWhatIsTrueOfAnAppWithNothingVendored(t *testing.T) {
	dir := t.TempDir()
	if _, err := diagnose(dir, ""); err == nil {
		t.Fatal("diagnose accepted a directory with no go.mod")
	}
	mustWrite(t, filepath.Join(dir, "go.mod"), "module bare\n\ngo 1.24\n")
	_, err := diagnose(dir, "")
	if err == nil {
		t.Fatal("diagnose accepted a module with nothing vendored in it")
	}
	if !strings.Contains(err.Error(), "nothing vendored to check") {
		t.Errorf("error %v, want one saying there is nothing to compare", err)
	}
	for _, want := range []string{"tokens.css", "internal", "static"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %v does not name %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "a rastrillo app?") {
		t.Errorf("the error doubts the app rather than reporting the state: %v", err)
	}
}

// TestDoctorReplaceDirectiveIsNotSkew: a replace points the app at a
// checkout, so there is no second version to disagree with. Treating it
// as skew would refuse --fix for everyone developing against a local
// rastrillo, which is every example app in this repository.
func TestDoctorReplaceDirectiveIsNotSkew(t *testing.T) {
	dir := doctorApp(t, "v0.1.0", "day")
	src, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
	mustWrite(t, filepath.Join(dir, "go.mod"),
		string(src)+"\nreplace "+rastrilloModule+" => ../..\n")

	rep, err := diagnose(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.skewed() {
		t.Fatal("a replace directive was read as version skew")
	}
	out := printed(rep, false)
	if !strings.Contains(out, "replaces rastrillo with ../..") {
		t.Errorf("the report does not say what the app was replaced with:\n%s", out)
	}
}

func TestModuleRequirement(t *testing.T) {
	for _, tc := range []struct {
		name, body, version, replaced string
	}{
		{"single line", "module x\n\nrequire " + rastrilloModule + " v0.20.0\n", "v0.20.0", ""},
		{"block", "module x\n\nrequire (\n\tgorm.io/gorm v1.0.0\n\t" + rastrilloModule + " v0.19.0 // indirect\n)\n", "v0.19.0", ""},
		{"replace", "module x\n\nrequire " + rastrilloModule + " v0.1.0\n\nreplace " + rastrilloModule + " => ../..\n", "v0.1.0", "../.."},
		{"replace block", "module x\n\nreplace (\n\t" + rastrilloModule + " v0.1.0 => ../..\n)\n", "", "../.."},
		{"replace to a version", "module x\n\nreplace " + rastrilloModule + " => " + rastrilloModule + " v0.9.9\n", "", rastrilloModule + " v0.9.9"},
		{"absent", "module x\n\nrequire gorm.io/gorm v1.0.0\n", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			mustWrite(t, filepath.Join(dir, "go.mod"), tc.body)
			v, r, err := moduleRequirement(dir, rastrilloModule)
			if err != nil {
				t.Fatal(err)
			}
			if v != tc.version || r != tc.replaced {
				t.Errorf("got (%q, %q), want (%q, %q)", v, r, tc.version, tc.replaced)
			}
		})
	}
}

func TestDescribeDiff(t *testing.T) {
	got := strings.Join(describeDiff([]byte("a\nCHANGED\nc\n"), []byte("a\nb\nc\n")), "\n")
	for _, want := range []string{"first difference at line 2", "yours      2: CHANGED", "library    2: b"} {
		if !strings.Contains(got, want) {
			t.Errorf("describeDiff does not contain %q:\n%s", want, got)
		}
	}
	// A file that differs only in its final newline differs, and the
	// sizes say so — but printing a sample of "" as the offending line
	// would send someone hunting for a change that is not there.
	got = strings.Join(describeDiff([]byte("a\nb"), []byte("a\nb\n")), "\n")
	if !strings.Contains(got, "trailing whitespace") {
		t.Errorf("a whitespace-only difference is not described as one:\n%s", got)
	}
}

// printed renders a report the way the command does, for tests that
// assert on the wording — which several do, because the wording IS the
// feature here.
func printed(r *report, fixing bool) string {
	var buf bytes.Buffer
	r.print(&buf, fixing)
	return buf.String()
}

// oldPinShape is the pin `rastrillo new` scaffolded at 36ee472 (#73),
// verbatim: three files, no theme.css, no datetime.js, no
// vendoredTheme. Both missing names joined the vendored set later
// (#104, #106), which is the whole point of this fixture — an app
// scaffolded in that window has a pin that never mentioned them, and
// absence there means "predates", not "deleted on purpose".
const oldPinShape = `package demoapptest

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"amadan.net/rastrillo/rastrillo/ui"
)

// The scaffold delivered these files once; they are app-owned from
// then on. If you edit one DELIBERATELY, delete its line below — the
// file is yours.
func TestVendoredAssetsMatchTheLibrary(t *testing.T) {
	for name, lib := range map[string][]byte{
		"tokens.css":   ui.TokensCSS(),
		"rastrillo.js": ui.ShimJS(),
		"select.js":    ui.SelectJS(),
	} {
		vendored, err := os.ReadFile(filepath.Join("..", "demoapp", "static", name))
		if err != nil {
			t.Errorf("read vendored %s: %v", name, err)
			continue
		}
		if !bytes.Equal(vendored, lib) {
			t.Errorf("static/%s differs from the library copy", name)
		}
	}
}
`

// TestDoctorOldPinPredatingAFileIsNotAClaim is the 2025 population, and
// the reason the old-shape fallback needs a second condition.
//
// The original pin listed three files. theme.css and datetime.js joined
// the vendored set afterwards, so a pin from that window is silent
// about both — and silence there cannot mean "the app deleted this
// pin line on purpose", because there was never a line to delete.
//
// The fixture is the shape those apps actually reach doctor in: the
// three pinned files clean, a theme.css the app really did hand-write
// (present, unlisted — a genuine claim), and no datetime.js at all
// (absent, unlisted — nothing was ever claimed). Calling the second one
// a deliberate edit tells an app something false about its own history
// AND makes --fix withhold a file it needs, with no override but
// --force, which would also flatten the theme.
func TestDoctorOldPinPredatingAFileIsNotAClaim(t *testing.T) {
	dir := doctorApp(t, rastrilloVersion(), "day")
	mustWrite(t, filepath.Join(dir, "internal", "demoapptest", "vendored_test.go"), oldPinShape)
	// The app hand-wrote its theme, and never had datetime.js.
	mustWrite(t, filepath.Join(dir, "internal", "demoapp", "static", "theme.css"),
		":root { --rst-bg: #fff8f0; }\n")
	if err := os.Remove(filepath.Join(dir, "internal", "demoapp", "static", "datetime.js")); err != nil {
		t.Fatal(err)
	}

	rep, err := diagnose(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]fileState{}
	for _, f := range rep.files {
		states[f.name] = f.state
	}
	if states["datetime.js"] != fileAbsent {
		t.Errorf("datetime.js: state %v, want fileAbsent — the old pin never listed it, so its absence claims nothing",
			states["datetime.js"])
	}
	if got := printed(rep, false); strings.Contains(got, "yours    datetime.js") {
		t.Errorf("a file the pin never mentioned is reported as a deliberate edit:\n%s", got)
	}
	// The hand-written theme is still protected — by the theme rule
	// here, since this pin has no vendoredTheme to identify it by.
	if states["theme.css"] != fileUnknownTheme {
		t.Errorf("theme.css: state %v, want fileUnknownTheme", states["theme.css"])
	}

	// And --fix must deliver the file without --force, without touching
	// the theme.
	var buf bytes.Buffer
	if err := rep.applyFix(&buf, false); err != nil {
		t.Fatalf("applyFix: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "internal", "demoapp", "static", "datetime.js"))
	if err != nil {
		t.Fatalf("--fix did not deliver datetime.js: %v", err)
	}
	if !bytes.Equal(got, ui.DatetimeJS()) {
		t.Error("datetime.js is not the library copy")
	}
	theme, _ := os.ReadFile(filepath.Join(dir, "internal", "demoapp", "static", "theme.css"))
	if string(theme) != ":root { --rst-bg: #fff8f0; }\n" {
		t.Error("--fix overwrote a theme the app hand-wrote")
	}
}

// TestDoctorUsageErrorsExitTwo: cli.md tables 2 as usage, and a CI
// branching on it has to be able to see it. Before this, an unknown
// flag and an unknown theme both came out as 1, indistinguishable from
// "your app is broken in a way I could not read".
func TestDoctorUsageErrorsExitTwo(t *testing.T) {
	dir := doctorApp(t, rastrilloVersion(), "day")
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"unknown flag", []string{"--badflag", dir}},
		{"unknown theme", []string{"--theme", "nosuchtheme", dir}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCode(t, runDoctor(tc.args)); got != 2 {
				t.Errorf("exit %d, want 2", got)
			}
		})
	}
}

// TestDoctorAdviceNamesSomethingTheAppHas: an app on the older pin has
// no vendoredIsMine map, so telling it to add a name to one sends
// someone looking for a thing that is not in their file.
func TestDoctorAdviceNamesSomethingTheAppHas(t *testing.T) {
	dir := doctorApp(t, rastrilloVersion(), "day")
	mustWrite(t, filepath.Join(dir, "internal", "demoapptest", "vendored_test.go"), oldPinShape)
	mustWrite(t, filepath.Join(dir, "internal", "demoapp", "static", "select.js"), "// drifted\n")

	rep, err := diagnose(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	out := printed(rep, false)
	if !strings.Contains(out, "delete the file's line from the pin in internal/demoapptest/vendored_test.go") {
		t.Errorf("an older app is not told how its own pin works:\n%s", out)
	}
	if strings.Contains(out, "vendoredIsMine") {
		t.Errorf("an older app is told to edit a map its pin does not have:\n%s", out)
	}
}

// TestClipDoesNotSplitARune: a drift sample is read by a person, and
// half a rune is not readable in any locale.
func TestClipDoesNotSplitARune(t *testing.T) {
	// Hindi, Arabic and Japanese — three of the eleven the framework
	// ships catalogs for, all multibyte.
	for _, s := range []string{
		strings.Repeat("क", 90), strings.Repeat("ب", 90), strings.Repeat("日", 90),
	} {
		got := clip(s, 68)
		if !utf8.ValidString(got) {
			t.Errorf("clip produced invalid UTF-8 from %.6s…", s)
		}
		if n := utf8.RuneCountInString(got); n != 69 { // 68 plus the ellipsis
			t.Errorf("clip kept %d runes, want 68 and an ellipsis", n)
		}
	}
	if got := clip("short", 68); got != "short" {
		t.Errorf("clip(%q) = %q, want it untouched", "short", got)
	}
}

// TestDoctorOldPinHonoursAnEditItCanSee: the other half of the same
// rule. A file an older pin does not list, which is present and DOES
// differ from the library, is the shape "delete its line" was invented
// for — an edit somebody made and wanted kept. Honour it.
func TestDoctorOldPinHonoursAnEditItCanSee(t *testing.T) {
	dir := doctorApp(t, rastrilloVersion(), "day")
	mustWrite(t, filepath.Join(dir, "internal", "demoapptest", "vendored_test.go"), oldPinShape)
	path := filepath.Join(dir, "internal", "demoapp", "static", "datetime.js")
	mustWrite(t, path, "// our own date handling, since 2025\n")

	rep, err := diagnose(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range rep.files {
		if f.name == "datetime.js" && f.state != fileMine {
			t.Errorf("datetime.js: state %v, want fileMine — present, unlisted and edited is the old convention's claim", f.state)
		}
	}
	if rep.drifted() {
		t.Errorf("an edit the old convention claims was reported as drift:\n%s", printed(rep, false))
	}
	var buf bytes.Buffer
	if err := rep.applyFix(&buf, false); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if string(body) != "// our own date handling, since 2025\n" {
		t.Fatal("--fix overwrote an edit the older pin claimed")
	}
}

// TestDoctorDoesNotExemptAFileItInstalled is why the rule resolves
// against the file's CONTENT and not merely its existence.
//
// The obvious reading — "unlisted and present means the app claimed
// it" — has a trap one step further on. --fix delivers datetime.js to a
// 2025 app; the file now exists and the old pin still does not mention
// it; so the very next run calls it a deliberate edit and doctor stops
// checking, forever, a file it installed itself. A pin line is deleted
// to protect an EDIT, so a file identical to the library has nothing to
// protect and every reason to stay checked.
func TestDoctorDoesNotExemptAFileItInstalled(t *testing.T) {
	dir := doctorApp(t, rastrilloVersion(), "day")
	mustWrite(t, filepath.Join(dir, "internal", "demoapptest", "vendored_test.go"), oldPinShape)
	path := filepath.Join(dir, "internal", "demoapp", "static", "datetime.js")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	// --fix delivers it, exactly as the previous test's app would.
	rep, err := diagnose(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := rep.applyFix(&buf, false); err != nil {
		t.Fatal(err)
	}

	// Run again: the file is present and unlisted, and must be checked.
	again, err := diagnose(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range again.files {
		if f.name == "datetime.js" && f.state != fileOK {
			t.Fatalf("datetime.js: state %v after --fix installed it, want fileOK — doctor exempted a file it wrote itself", f.state)
		}
	}
	// And it is still checked once it drifts.
	mustWrite(t, path, "// somebody edited this later\n")
	third, err := diagnose(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range third.files {
		if f.name == "datetime.js" && f.state != fileMine {
			t.Errorf("datetime.js: state %v once edited, want fileMine", f.state)
		}
	}
}

// The upgrade hazard the staged markup migration exists for, checked
// against the tool that is supposed to catch it.
//
// tokens.css is written into an app's static/ at scaffold time and
// frozen there while the partials upgrade with the module. An app
// scaffolded before the attribute spelling shipped has a class-only
// stylesheet; take the module that flipped the partials and every
// screen renders unstyled, with nothing failing and nothing to read.
// That is exactly the shape doctor is for, so it has to be the shape
// doctor reports — and after --fix, the app has to be clean.
func TestDoctorCatchesAnAppOnTheClassOnlyStylesheet(t *testing.T) {
	dir := doctorApp(t, rastrilloVersion(), "day")
	path := filepath.Join(dir, "internal", "demoapp", "static", "tokens.css")
	frozen := withoutAttributeSelectors(string(ui.TokensCSS()))
	// Outside its comments: the file's own header explains the grammar,
	// and quoting an attribute selector there styles nothing.
	if bare := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(frozen, ""); strings.Contains(bare, "[rst-") {
		t.Fatalf("the fixture still carries attribute selectors, so it is not the stylesheet an old app has: %s",
			bare[strings.Index(bare, "[rst-")-60:strings.Index(bare, "[rst-")+40])
	}
	if len(frozen) >= len(ui.TokensCSS()) {
		t.Fatal("stripping the attribute selectors made the file no smaller — the fixture is not what it claims")
	}
	mustWrite(t, path, frozen)

	rep, err := diagnose(dir, "")
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	if !rep.drifted() {
		t.Fatal("an app on the class-only stylesheet is not reported as drift; this is the case the staging exists for")
	}
	if got := exitCode(t, rep.exit()); got != exitDrift {
		t.Errorf("exit %d, want %d", got, exitDrift)
	}
	if out := printed(rep, false); !strings.Contains(out, "drift    tokens.css") || !strings.Contains(out, "--fix") {
		t.Errorf("the report does not name tokens.css and say how to fix it:\n%s", out)
	}

	var buf bytes.Buffer
	if err := rep.applyFix(&buf, false); err != nil {
		t.Fatalf("applyFix: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, ui.TokensCSS()) {
		t.Fatal("--fix did not restore the stylesheet that styles the attribute spelling")
	}
}

// withoutAttributeSelectors is the stylesheet an app scaffolded before
// the pairing landed has: every selector naming an rst- attribute
// removed, and a rule with nothing left removed with it. It descends
// into @media and @keyframes rather than copying them whole, because
// that is where the shells' rules live and copying them would leave the
// fixture styling half of what it claims not to.
func withoutAttributeSelectors(css string) string {
	var out strings.Builder
	i, seg := 0, 0
	for i < len(css) {
		switch {
		case strings.HasPrefix(css[i:], "/*"):
			j := strings.Index(css[i+2:], "*/")
			if j < 0 {
				i = len(css)
				continue
			}
			i += 2 + j + 2
		case css[i] == '{':
			lead, selectors := splitPrelude(css[seg:i])
			if strings.HasPrefix(strings.TrimSpace(selectors), "@") || strings.TrimSpace(selectors) == "" {
				out.WriteString(css[seg : i+1]) // an at-rule, or a keyframe stop: descend
				i++
				seg = i
				continue
			}
			var kept []string
			for _, sel := range strings.Split(selectors, ",") {
				if !strings.Contains(sel, "[rst-") {
					kept = append(kept, sel)
				}
			}
			k := i + strings.IndexByte(css[i:], '}')
			if len(kept) > 0 {
				out.WriteString(lead + strings.Join(kept, ",") + css[i:k+1])
			}
			i, seg = k+1, k+1
		case css[i] == '}':
			out.WriteString(css[seg : i+1])
			i++
			seg = i
		default:
			i++
		}
	}
	out.WriteString(css[seg:])
	return out.String()
}

// splitPrelude cuts a rule's prelude into whatever comes before its
// last comment and the selector list itself, so a comment that quotes
// an attribute selector cannot delete the rule it documents.
func splitPrelude(prelude string) (lead, selectors string) {
	if k := strings.LastIndex(prelude, "*/"); k >= 0 {
		return prelude[:k+2], prelude[k+2:]
	}
	return "", prelude
}
