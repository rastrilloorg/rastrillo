package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/ui"
)

// fullShim stands in for the rastrillo.js an app vendored before
// fiesta.js: doctor only reads that it differs from the slim shim.
const fullShim = "// rastrillo.js before fiesta.js: menus, switches, schemes\n"

// oldApp is doctorApp as an app from before fiesta.js has it: the full
// rastrillo.js, no fiesta.js, and the column layout that linked each
// script by hand, before pageScripts.
func oldApp(t *testing.T) (dir, static string) {
	t.Helper()
	dir = doctorApp(t, rastrilloVersion(), "day")
	static = filepath.Join(dir, "internal", "demoapp", "static")
	mustWrite(t, filepath.Join(static, "rastrillo.js"), fullShim)
	if err := os.Remove(filepath.Join(static, "fiesta.js")); err != nil {
		t.Fatal(err)
	}
	layout, err := os.ReadFile("../../ui/testdata/older/fixed-tags.html")
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, layoutPath(dir), string(layout))
	return dir, static
}

func layoutPath(dir string) string {
	return filepath.Join(dir, "internal", "demoapp", "templates",
		"layout.html")
}

// recordAsMine names files in the app's vendoredIsMine.
func recordAsMine(t *testing.T, dir string, names ...string) {
	t.Helper()
	pin := filepath.Join(dir, "internal", "demoapptest", "vendored_test.go")
	source, err := os.ReadFile(pin)
	if err != nil {
		t.Fatal(err)
	}
	entries := ""
	for _, name := range names {
		entries += fmt.Sprintf("%q: true,\n", name)
	}
	mustWrite(t, pin, strings.Replace(string(source),
		`// "tokens.css": true,`, entries, 1))
}

// fixed runs doctor --fix on dir and returns what it printed.
func fixed(t *testing.T, dir string, force bool) string {
	t.Helper()
	report, err := diagnose(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := report.applyFix(&output, force); err != nil {
		t.Fatalf("applyFix: %v\n%s", err, output.String())
	}
	return output.String()
}

func libraryCopy(t *testing.T, name string) []byte {
	t.Helper()
	assets, _ := ui.VendoredAssets("day")
	return assets[name]
}

func holds(t *testing.T, static, name string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(static, name))
	if want == nil {
		if err == nil {
			t.Errorf("%s was written", name)
		}
		return
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s is not what it should be (%v)", name, err)
	}
}

// The first --fix on an old app is the run that slims rastrillo.js, so
// it is the run that names the layout line fiesta.js needs; once the
// layout has the line, doctor says nothing more about it.
func TestDoctorFixWarnsOnTheRunThatSlimsTheShim(t *testing.T) {
	dir, static := oldApp(t)
	warning := filepath.Join("internal", "demoapp", "templates",
		"layout.html") + ": " + doctorFiestaAdvisory
	report, err := diagnose(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if out := printed(report, false); !strings.Contains(out, warning) {
		t.Errorf("the report does not name the layout line:\n%s", out)
	}
	if out := fixed(t, dir, false); !strings.Contains(out, warning) {
		t.Errorf("the run that slims the shim does not warn:\n%s", out)
	}
	holds(t, static, "rastrillo.js", ui.ShimJS())
	holds(t, static, "fiesta.js", libraryCopy(t, "fiesta.js"))

	layout, _ := os.ReadFile(layoutPath(dir))
	mustWrite(t, layoutPath(dir), strings.Replace(string(layout),
		"{{iconAssets}}", "{{iconAssets}}\n"+pageScriptsLine, 1))
	if out := fixed(t, dir, false); strings.Contains(out, "fiesta.js") {
		t.Errorf("a layout that ranges pageScripts is warned about:\n%s",
			out)
	}
}

// rastrillo.js and fiesta.js refresh together or not at all: Fiesta
// does not support fiesta.js beside the full shim. When the app owns
// either, --fix leaves both and says why, in the words of the app's own
// vendored test; --force refreshes both.
func TestDoctorKeepsAnOwnedShimAndFiestaJSApart(t *testing.T) {
	for _, owner := range []string{"rastrillo.js", "fiesta.js"} {
		for _, force := range []bool{false, true} {
			name := fmt.Sprintf("%s is the app's, force %v", owner, force)
			t.Run(name, func(t *testing.T) {
				keepsThePairApart(t, owner, force)
			})
		}
	}
}

func keepsThePairApart(t *testing.T, owner string, force bool) {
	dir, static := oldApp(t)
	recordAsMine(t, dir, owner)
	other := vendoredPairs[owner]
	output := fixed(t, dir, force)
	if force {
		holds(t, static, "rastrillo.js", ui.ShimJS())
		holds(t, static, "fiesta.js", libraryCopy(t, "fiesta.js"))
		return
	}
	holds(t, static, "rastrillo.js", []byte(fullShim))
	holds(t, static, "fiesta.js", nil)
	note := fmt.Sprintf("left alone  %s (goes with %s, left alone above: "+
		"add %s to vendoredIsMine too, or pass --force for both)",
		other, owner, other)
	if !strings.Contains(output, note) {
		t.Errorf("--fix does not say why it left %s:\n%s", other, output)
	}
	if strings.Contains(output, doctorFiestaAdvisory) {
		t.Errorf("a layout beside the full shim is warned about:\n%s",
			output)
	}
	report, _ := diagnose(dir, "")
	if out := printed(report, false); strings.Contains(out, "would add") {
		t.Errorf("the report promises --fix adds fiesta.js:\n%s", out)
	}
}

// calendar.js and datetime.js move together: the old datetime.js calls
// rastrilloCalendar, which the new calendar.js names fiestaCalendar.
func TestDoctorMovesTheDateFilesTogether(t *testing.T) {
	oldDates := func(t *testing.T) (string, string) {
		dir := doctorApp(t, rastrilloVersion(), "day")
		static := filepath.Join(dir, "internal", "demoapp", "static")
		mustWrite(t, filepath.Join(static, "calendar.js"), "// old calendar\n")
		mustWrite(t, filepath.Join(static, "datetime.js"), "// old datetime\n")
		return dir, static
	}
	t.Run("neither is the app's", func(t *testing.T) {
		dir, static := oldDates(t)
		fixed(t, dir, false)
		holds(t, static, "calendar.js", ui.CalendarJS())
		holds(t, static, "datetime.js", ui.DatetimeJS())
	})
	t.Run("datetime.js is the app's", func(t *testing.T) {
		dir, static := oldDates(t)
		recordAsMine(t, dir, "datetime.js")
		output := fixed(t, dir, false)
		holds(t, static, "calendar.js", []byte("// old calendar\n"))
		holds(t, static, "datetime.js", []byte("// old datetime\n"))
		if !strings.Contains(output, "left alone  calendar.js (goes with "+
			"datetime.js, left alone above: add calendar.js to "+
			"vendoredIsMine too, or pass --force for both)") {
			t.Errorf("--fix does not say why it left calendar.js:\n%s",
				output)
		}
	})
	t.Run("an older pin leaves out an edited datetime.js", func(t *testing.T) {
		dir, static := oldDates(t)
		mustWrite(t, filepath.Join(dir, "internal", "demoapptest",
			"vendored_test.go"), olderPinWithout("datetime.js"))
		output := fixed(t, dir, false)
		holds(t, static, "calendar.js", []byte("// old calendar\n"))
		if !strings.Contains(output, "delete calendar.js's line from the "+
			"pin too, or pass --force for both") {
			t.Errorf("--fix does not speak the older pin's words:\n%s",
				output)
		}
	})
}

// olderPinWithout is the map-literal pin, listing every vendored file
// but the one the app deleted the line of.
func olderPinWithout(mine string) string {
	var pin strings.Builder
	pin.WriteString("package demoapptest\n\n" +
		"func TestVendoredAssetsMatchTheLibrary(t *testing.T) {\n" +
		"\tfor name := range map[string]bool{\n")
	for _, name := range ui.VendoredNames() {
		if name != mine {
			fmt.Fprintf(&pin, "\t\t%q: true,\n", name)
		}
	}
	pin.WriteString("\t} {\n\t}\n}\n")
	return pin.String()
}
