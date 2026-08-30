package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo/ui"
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

// TestDoctorWritesAMissingFile: an app that deleted select.js because
// it has no big selects is a supported thing, but --fix asked for is
// --fix given. The report must say the file was missing rather than
// pretending it drifted.
func TestDoctorWritesAMissingFile(t *testing.T) {
	dir := doctorApp(t, rastrilloVersion(), "day")
	path := filepath.Join(dir, "internal", "demoapp", "static", "datetime.js")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	rep, err := diagnose(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(printed(rep, false), "missing  datetime.js") {
		t.Errorf("a deleted file is not reported as missing:\n%s", printed(rep, false))
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
	// The files it does not have are missing, which is a finding, not
	// an error — but tokens.css, the one that matters, is clean.
	for _, f := range rep.files {
		if f.name == "tokens.css" && f.state != fileOK {
			t.Errorf("tokens.css: state %v, want ok", f.state)
		}
	}
}

// TestDoctorRejectsSomethingThatIsNotAnApp: the failure has to name
// what it looked for, because "no" without a reason sends someone
// hunting through their own directory layout.
func TestDoctorRejectsSomethingThatIsNotAnApp(t *testing.T) {
	dir := t.TempDir()
	if _, err := diagnose(dir, ""); err == nil {
		t.Fatal("diagnose accepted a directory with no go.mod")
	}
	mustWrite(t, filepath.Join(dir, "go.mod"), "module bare\n\ngo 1.24\n")
	_, err := diagnose(dir, "")
	if err == nil || !strings.Contains(err.Error(), "no vendored files") {
		t.Fatalf("error %v, want one naming the missing vendored files", err)
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
