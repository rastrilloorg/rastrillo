package designsystem

import (
	"encoding/json"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/nodetest"
	"amadan.net/rastrillo/rastrillo/ui"
)

// The formatting the spec rules (§6-v2.9): KiB of 1024, one decimal
// below 10 KiB so a 634-byte file is not "1" or "0", whole numbers
// above, in the page's own number format.
func TestKiBIsOneDecimalBelowTenAndWholeAbove(t *testing.T) {
	for _, c := range []struct {
		locale string
		n      int
		want   string
	}{
		{"en", 634, "0.6"},
		{"en", 1024, "1.0"},
		{"en", 10*1024 - 1, "10.0"},
		{"en", 10 * 1024, "10"},
		{"en", 107279, "105"},
		{"en", 2048 * 1024, "2,048"},
		{"es", 634, "0,6"},
		{"ru", 4300, "4,2"},
	} {
		if got := kib(c.locale, c.n); got != c.want {
			t.Errorf("kib(%s, %d) = %q, want %q", c.locale, c.n, got, c.want)
		}
	}
}

// The stripped figure is the line's last part in every language, so a
// file with no comments loses exactly that part. A translation that
// moved {stripped} forward, or used another separator, would cut the
// wrong figure off or none at all.
func TestTheWeightLineDropsItsLastPartCleanly(t *testing.T) {
	full := weight{Raw: 20000, Gzip: 6000, Stripped: 2000}
	bare := weight{Raw: 20000, Gzip: 6000}
	for _, locale := range rastrillo.BaseLocales() {
		line := proseIn(locale, weightLine)
		parts := strings.Split(line, weightSep)
		if len(parts) != 3 {
			t.Errorf("%s: the weight line has %d parts split on %q, want 3: %q", locale, len(parts), weightSep, line)
			continue
		}
		if !strings.Contains(parts[2], "{stripped}") || strings.Contains(parts[0]+parts[1], "{stripped}") {
			t.Errorf("%s: {stripped} is not alone in the last part: %q", locale, line)
		}
		if got := weightIn(locale, full); strings.Contains(got, "{") {
			t.Errorf("%s: a placeholder survived: %q", locale, got)
		}
		got := weightIn(locale, bare)
		if strings.Count(got, weightSep) != 1 || strings.Contains(got, "{") {
			t.Errorf("%s: with nothing to strip the line should be two figures: %q", locale, got)
		}
	}
}

// Every shipped file gets smaller under gzip, and smaller again without
// its comments. The literal figures are not asserted anywhere: they are
// the compressor's, not this package's.
func TestEveryShippedFileWeighsLessCompressedAndLessAgainStripped(t *testing.T) {
	files := map[string][]byte{
		"tokens.css": ui.TokensCSS(), "shell.css": ui.ShellCSS(),
		"rastrillo.js": ui.ShimJS(), "busy.js": ui.BusyJS(), "anchor.js": ui.AnchorJS(),
		"shell.js": ui.ShellJS(), "select.js": ui.SelectJS(), "datetime.js": ui.DatetimeJS(),
		"calendar.js": ui.CalendarJS(),
	}
	for _, name := range ui.ThemeNames() {
		css, _ := ui.ThemeCSS(name)
		files["theme-"+name+".css"] = css
	}
	for name, body := range files {
		w := weigh(name, body)
		if w.Gzip >= w.Raw {
			t.Errorf("%s: gzipped %d is not below raw %d", name, w.Gzip, w.Raw)
		}
		if w.Stripped == 0 {
			t.Errorf("%s: no comments found to strip; every shipped file carries some", name)
		} else if w.Stripped > w.Gzip {
			t.Errorf("%s: stripped %d is above gzipped %d", name, w.Stripped, w.Gzip)
		}
	}
}

// The JavaScript stripper's hard cases: everything that only looks like
// a comment stays, and code either side of a removed comment still
// parses as the same code.
func TestStripJSCommentsLeavesWhatOnlyLooksLikeAComment(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"var a = 1; // note\nvar b = 2;\n", "var a = 1; \nvar b = 2;\n"},
		{"// alone\nvar a;\n", "var a;\n"},
		{"  /* alone */\nvar a;\n", "var a;\n"},
		{"var s = 'http://x/*y*/';\n", "var s = 'http://x/*y*/';\n"},
		{`var s = "a\"//b";` + "\n", `var s = "a\"//b";` + "\n"},
		{"var r = /\\/*x/g; // c\n", "var r = /\\/*x/g; \n"},
		{"var r = /[/]/.test(s);\n", "var r = /[/]/.test(s);\n"},
		{"var d = a / b / c; // c\n", "var d = a / b / c; \n"},
		{"return /x/.test(s);\n", "return /x/.test(s);\n"},
		{"var t = `a ${ {b: 1}.b /* c */ } // d`;\n", "var t = `a ${ {b: 1}.b  } // d`;\n"},
		{"var t = `${`${'//'}`}`; // e\n", "var t = `${`${'//'}`}`; \n"},
		{"a/**/b\n", "a b\n"},
		{"a /* one\ntwo */ b\n", "a \n b\n"},
	} {
		if got := string(stripJSComments([]byte(c.in))); got != c.want {
			t.Errorf("stripJSComments(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

func TestStripCSSCommentsLeavesStringsAlone(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"/* head */\na { color: red; }\n", "a { color: red; }\n"},
		{"a { content: \"/* no */\"; } /* yes */\n", "a { content: \"/* no */\"; } \n"},
		{"a { b: c; /* d */ e: f; }\n", "a { b: c;  e: f; }\n"},
	} {
		if got := string(stripCSSComments([]byte(c.in))); got != c.want {
			t.Errorf("stripCSSComments(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

// nodeEval runs each script in a fresh node vm context and returns what
// it came to: its completion value as JSON (awaited, so an async case
// reports what it resolved to), or the error it threw.
// Equivalence is the question a stripper has to answer, and node
// --check cannot: a+/**/++b and a+++b both parse.
const nodeEval = `
const vm = require("vm");
const srcs = JSON.parse(require("fs").readFileSync(0, "utf8"));
(async () => {
  const out = [];
  for (const src of srcs) {
    try { out.push("value " + JSON.stringify(await vm.runInNewContext(src, {}))); }
    catch (e) { out.push("throws " + e.name + ": " + e.message); }
  }
  process.stdout.write(JSON.stringify(out));
})();
`

func evalInNode(t *testing.T, srcs []string) []string {
	t.Helper()
	in, err := json.Marshal(srcs)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	if err := json.Unmarshal(nodetest.Run(t, nodetest.Cmd{Args: []string{"-e", nodeEval}, Stdin: in}), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// The cases a character-level stripper gets wrong, each run in node as
// written and as stripped: a regex after a control-flow header's ")",
// where a / starts a regex rather than dividing, and a comment between
// two operators, whose removal must not fuse them into a third.
func TestStrippedScriptsEvaluateAsWritten(t *testing.T) {
	cases := []string{
		`var r = 0; if (true) /[/*]/.test("/") && (r = 1); r`,
		`var n = 0; while (n < 1) /a/.test("a") && n++; n`,
		`var k = 0; for (var i = 0; i < 2; i++) /[*/]/.test("*") && k++; k`,
		`let a = 1, b = 2; [a+/**/++b, a, b]`,
		`let a = 5, b = 2; [a-/* x */-b, a /**/ / b]`,
		`var x = 4, g = 2; [x / 2 / g, (x) / 2, x++ / 2]`,
		`var s = "/*", t = '//', u = ` + "`${s}/*${t}*/`" + `; [s, t, u] // tail`,
		`var o = {a: /}/.source}; /* gone */ o.a`,
		`(async () => { var r = 0; for await (const x of [1]) /[/*]/.test("/") && (r = 1); return r })()`,
	}
	var both []string
	for _, c := range cases {
		both = append(both, c, string(stripJSComments([]byte(c))))
	}
	got := evalInNode(t, both)
	for i, c := range cases {
		if got[2*i] != got[2*i+1] {
			t.Errorf("stripping changed what a script does:\n  script: %s\nstripped: %s\n  before: %s\n   after: %s", c, both[2*i+1], got[2*i], got[2*i+1])
		}
		if strings.HasPrefix(got[2*i], "throws") {
			t.Errorf("the case itself does not run: %s: %s", c, got[2*i])
		}
	}
}

// shapeEval loads each shipped script into an empty context, with a
// CommonJS module and no document, and reports what it left behind: the
// globals it defined, what it exported, and the error it stopped at.
// The scripts export their pure halves to node behind a document guard,
// so this runs real code from each, not only its declarations.
const shapeEval = `
const vm = require("vm");
const srcs = JSON.parse(require("fs").readFileSync(0, "utf8"));
const shape = v => v === null ? "null" : typeof v === "function" ? "function/" + v.length : typeof v;
const describe = o => Object.keys(o).sort().map(k => k + ":" + shape(o[k])).join(",");
process.stdout.write(JSON.stringify(srcs.map(src => {
  const module = {exports: {}};
  const ctx = {module, exports: module.exports};
  let err = "";
  try { vm.runInNewContext(src, ctx); } catch (e) { err = e.name + ": " + e.message; }
  return "globals " + describe(ctx) + " | exports " + describe(module.exports) + " | " + err;
})));
`

// Every shipped script, stripped, still evaluates to the same thing:
// the same globals and exports of the same kinds and arities, and the
// same error where it reaches for a document node does not have. A
// stripper that ate code a regex was hiding would change one of them.
func TestEveryShippedScriptStripsToTheSameShape(t *testing.T) {
	scripts := map[string][]byte{
		"rastrillo.js": ui.ShimJS(), "busy.js": ui.BusyJS(), "anchor.js": ui.AnchorJS(), "shell.js": ui.ShellJS(),
		"select.js": ui.SelectJS(), "datetime.js": ui.DatetimeJS(), "calendar.js": ui.CalendarJS(),
	}
	names := make([]string, 0, len(scripts))
	var srcs []string
	for name, body := range scripts {
		names = append(names, name)
		srcs = append(srcs, string(body), string(stripJSComments(body)))
	}
	in, err := json.Marshal(srcs)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(nodetest.Run(t, nodetest.Cmd{Args: []string{"-e", shapeEval}, Stdin: in}), &got); err != nil {
		t.Fatal(err)
	}
	for i, name := range names {
		if got[2*i] != got[2*i+1] {
			t.Errorf("%s evaluates differently stripped:\n  as written: %s\n    stripped: %s", name, got[2*i], got[2*i+1])
		}
		t.Logf("%s: %s", name, got[2*i])
		if strings.Contains(got[2*i], "SyntaxError") {
			t.Errorf("%s does not even parse as written: %s", name, got[2*i])
		}
	}
}
