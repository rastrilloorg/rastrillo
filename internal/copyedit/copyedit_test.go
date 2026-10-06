package copyedit

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
)

const prose = "package designsystem\n\nvar prose = map[string]map[string]string{\n\t`Old {name}`: {\n\t\t`ga`: `Sean {name}`,\n\t},\n}\n"

func tr(s string) map[string]string {
	out := map[string]string{}
	for _, l := range Locales {
		out[l] = s
	}
	return out
}

// An edit carries a translation for every shipped locale but the key,
// and for no other: a list kept by hand here fell behind the day the
// framework shipped a twelfth.
func TestLocalesAreTheShippedOnesButEnglish(t *testing.T) {
	want := slices.DeleteFunc(rastrillo.BaseLocales(), func(l string) bool { return l == "en" })
	if !slices.Equal(Locales, want) || len(want) == 0 {
		t.Errorf("Locales %q, want %q", Locales, want)
	}
}

func TestApplyProseAddsRemovesAndFormats(t *testing.T) {
	approved := map[string]string{"x.new": "New {name}"}
	got, err := ApplyProse(prose, approved, []string{"Old {name}"}, []Entry{{ID: "x.new", EN: "New {name}", TR: tr("Nua {name}")}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "`Old {name}`") || !strings.Contains(got, "\t`New {name}`: {\n") || !strings.Contains(got, "\t\t`zh-Hans`: `Nua {name}`,") {
		t.Errorf("not applied:\n%s", got)
	}
}

func TestApplyProseRefusesWhatWasNotApproved(t *testing.T) {
	approved := map[string]string{"x.new": "New {name}", "x.caps": "Hi {Name} {count_2}"}
	bad := map[string]Entry{
		"an id with no approved text":     {ID: "x.other", EN: "New {name}", TR: tr("Nua {name}")},
		"English other than the approved": {ID: "x.new", EN: "Newer {name}", TR: tr("Nua {name}")},
		"an em dash":                      {ID: "x.new", EN: "New {name}", TR: tr("Nua — {name}")},
		"a backtick":                      {ID: "x.new", EN: "New {name}", TR: tr("Nua `{name}`")},
		"a dropped placeholder":           {ID: "x.new", EN: "New {name}", TR: tr("Nua")},
		"a missing locale":                {ID: "x.new", EN: "New {name}", TR: map[string]string{"ga": "Nua {name}"}},
		"a dropped capitalised one":       {ID: "x.caps", EN: "Hi {Name} {count_2}", TR: tr("Dia {count_2}")},
	}
	for why, e := range bad {
		if _, err := ApplyProse(prose, approved, nil, []Entry{e}); err == nil {
			t.Errorf("%s was written", why)
		}
	}
	if _, err := ApplyProse(prose, approved, []string{"Gone"}, nil); err == nil {
		t.Error("removing a key that is not there succeeded")
	}
}

func TestFillReplacesMarkersWithApprovedText(t *testing.T) {
	approved := map[string]string{"b.save": "Save", "b.bad": `Say "hi"`}
	got, n, err := Fill("x := `<b>⟦b.save⟧</b>` + \"⟦b.save⟧\"", approved)
	if err != nil || n != 2 || got != "x := `<b>Save</b>` + \"Save\"" {
		t.Errorf("Fill = %q, %d, %v", got, n, err)
	}
	for _, src := range []string{"⟦b.missing⟧", "⟦b.bad⟧"} {
		if _, _, err := Fill(src, approved); err == nil {
			t.Errorf("Fill(%q) succeeded", src)
		}
	}
}

// What a marker may become depends on where it is. Inside a raw Go
// string a double quote is plain text and only a backtick would end the
// literal; inside an interpreted one a quote or a backslash would. In a
// Markdown file there is no literal at all, and backticks are how the
// docs mark code. The em dash is refused everywhere: the operator does
// not want it in copy.
func TestFillFileKnowsWhatKindOfStringItIsIn(t *testing.T) {
	approved := map[string]string{
		"b.quote": `See "Upgrading" in the guide.`,
		"b.code":  "Put it in `profile`.",
		"b.dash":  "One \u2014 two",
		"b.slash": `C:\path`,
	}
	for _, c := range []struct {
		name, file, src string
		ok              bool
		want            string
	}{
		{"a quote in a raw Go string", "a.go", "const a = `⟦b.quote⟧`\n", true, "const a = `See \"Upgrading\" in the guide.`\n"},
		{"a quote in an interpreted Go string", "a.go", "const a = \"⟦b.quote⟧\"\n", false, ""},
		{"a backtick in a raw Go string", "a.go", "const a = `⟦b.code⟧`\n", false, ""},
		{"a backtick in an interpreted Go string", "a.go", "const a = \"⟦b.code⟧\"\n", true, "const a = \"Put it in `profile`.\"\n"},
		{"a quote and code in Markdown", "a.md", "Say ⟦b.quote⟧ ⟦b.code⟧\n", true, "Say See \"Upgrading\" in the guide. Put it in `profile`.\n"},
		{"an em dash in Markdown", "a.md", "⟦b.dash⟧\n", false, ""},
		{"a backslash in an interpreted Go string", "a.go", "const a = \"⟦b.slash⟧\"\n", false, ""},
		{"a backslash in a raw Go string", "a.go", "const a = `⟦b.slash⟧`\n", true, "const a = `C:\\path`\n"},
	} {
		got, n, err := FillFile(c.file, c.src, approved)
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok %v", c.name, err, c.ok)
			continue
		}
		if c.ok && (got != c.want || n == 0) {
			t.Errorf("%s: FillFile = %q (%d), want %q", c.name, got, n, c.want)
		}
	}
}

// A key whose entry never closes is refused by name rather than cut at
// a guessed offset, which would delete the wrong bytes and leave a
// format error that names nothing.
func TestApplyProseRefusesAnEntryThatNeverCloses(t *testing.T) {
	src := "package designsystem\n\nvar prose = map[string]map[string]string{\n\t`Open`: {\n\t\t`ga`: `Oscail`,\n}\n"
	_, err := ApplyProse(src, nil, []string{"Open"}, nil)
	if err == nil || !strings.Contains(err.Error(), `"Open"`) {
		t.Errorf("err = %v, want one naming the unclosed key", err)
	}
}

// Two entries for one key in a single edit would write a duplicate map
// key, which gofmt accepts and only the compiler refuses, after the
// file is already written.
func TestApplyProseRefusesADuplicateWithinOneAdd(t *testing.T) {
	approved := map[string]string{"x.new": "New {name}", "x.same": "New {name}"}
	for why, add := range map[string][]Entry{
		"one id twice":     {{ID: "x.new", EN: "New {name}", TR: tr("Nua {name}")}, {ID: "x.new", EN: "New {name}", TR: tr("Nua {name}")}},
		"one text two ids": {{ID: "x.new", EN: "New {name}", TR: tr("Nua {name}")}, {ID: "x.same", EN: "New {name}", TR: tr("Nua {name}")}},
	} {
		if _, err := ApplyProse(prose, approved, nil, add); err == nil {
			t.Errorf("%s was written", why)
		}
	}
}

// A marker the id pattern does not match would otherwise stay in the
// source as literal text and ship to the page.
func TestFillRefusesAMarkerItCannotRead(t *testing.T) {
	approved := map[string]string{"b.save": "Save"}
	for _, bad := range []string{"⟦gallery.locale-menu.note⟧", "⟦Gallery.x⟧", "⟦ x ⟧", "⟦b.save"} {
		_, _, err := Fill("a := \"⟦b.save⟧\"\nb := \""+bad+"\"\n", approved)
		if err == nil || !strings.Contains(err.Error(), bad) {
			t.Errorf("%s: err = %v, want one naming it", bad, err)
		}
	}
}

// tree lays out a scratch checkout: a prose.go, an approved result
// file and the given fill files, and returns their paths by name.
func tree(t *testing.T, fills map[string]string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	paths := map[string]string{"prose": filepath.Join(dir, "prose.go"), "approved": filepath.Join(dir, "result.json")}
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(paths["prose"], prose)
	write(paths["approved"], `{"action":"approve","strings":[{"id":"x.new","text":"New {name}"},{"id":"b.save","text":"Save"}]}`)
	for name, src := range fills {
		paths[name] = filepath.Join(dir, name)
		write(paths[name], src)
	}
	return paths
}

func editFile(t *testing.T, dir string, e any) string {
	t.Helper()
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "edit.json")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func snapshot(t *testing.T, paths map[string]string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for name, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		out[name] = string(b)
	}
	return out
}

// An edit that fails part-way must leave the tree as it found it: a
// re-run after fixing the edit would otherwise meet its own half-written
// output and refuse with "already has".
func TestRunWritesNothingWhenAnyPartFails(t *testing.T) {
	paths := tree(t, map[string]string{
		"good.go": "package x\n\nvar a = \"⟦b.save⟧\"\n",
		"bad.go":  "package x\n\nvar b = \"⟦b.unapproved⟧\"\n",
	})
	before := snapshot(t, paths)
	edit := editFile(t, filepath.Dir(paths["prose"]), Edit{
		Approved: []string{paths["approved"]},
		Prose:    paths["prose"],
		Remove:   []string{"Old {name}"},
		Add:      []Entry{{ID: "x.new", EN: "New {name}", TR: tr("Nua {name}")}},
		Fill:     []string{paths["good.go"], paths["bad.go"]},
	})
	if err := Run(edit, io.Discard); err == nil {
		t.Fatal("an edit with an unapproved marker succeeded")
	}
	for name, was := range before {
		if got := snapshot(t, map[string]string{name: paths[name]})[name]; got != was {
			t.Errorf("%s changed although the edit failed:\n%s", name, got)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(paths["prose"]))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(paths)+1 {
		t.Errorf("the directory holds %d files, want %d: a temp file was left behind", len(entries), len(paths)+1)
	}
}

func TestRunAppliesAGoodEdit(t *testing.T) {
	paths := tree(t, map[string]string{"good.go": "package x\n\nvar a = \"⟦b.save⟧\"\n"})
	edit := editFile(t, filepath.Dir(paths["prose"]), Edit{
		Approved: []string{paths["approved"]},
		Prose:    paths["prose"],
		Add:      []Entry{{ID: "x.new", EN: "New {name}", TR: tr("Nua {name}")}},
		Fill:     []string{paths["good.go"]},
	})
	var log strings.Builder
	if err := Run(edit, &log); err != nil {
		t.Fatal(err)
	}
	if want := "copyedit: prose.go: +1 -0\ncopyedit: " + paths["good.go"] + ": 1 filled\n"; log.String() != want {
		t.Errorf("Run printed\n%s\nwant\n%s", log.String(), want)
	}
	after := snapshot(t, paths)
	if !strings.Contains(after["prose"], "`New {name}`") || after["good.go"] != "package x\n\nvar a = \"Save\"\n" {
		t.Errorf("not applied:\n%s\n%s", after["prose"], after["good.go"])
	}
}

// A misspelt field ("fil" for "fill") would decode to an edit that
// quietly does less than its author meant.
func TestRunRefusesAnUnknownField(t *testing.T) {
	paths := tree(t, map[string]string{"good.go": "package x\n\nvar a = \"⟦b.save⟧\"\n"})
	edit := editFile(t, filepath.Dir(paths["prose"]), map[string]any{
		"approved": []string{paths["approved"]},
		"fil":      []string{paths["good.go"]},
	})
	if err := Run(edit, io.Discard); err == nil || !strings.Contains(err.Error(), "fil") {
		t.Errorf("err = %v, want one naming the unknown field", err)
	}
}
