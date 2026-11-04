package ui

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/vendored"
)

// jsQuote is any of the three quotings of a JavaScript string, and jsText
// the run inside one, up to the next quote of any kind.
const (
	jsQuote = `["'` + "`" + `]`
	jsText  = `[^"'` + "`" + `]*`
)

// The forms in which a script puts a hook on an element it builds. A
// name a script only reads (querySelector, closest, hasAttribute, a
// selector constant) is not here: the page's markup has to carry it
// already, so the page's own templates account for it. Each form
// takes the text that carries the names, and the names are read from
// it with vendored.HooksIn, so "rst-a rst-b" gives both.
var creatingForms = []*regexp.Regexp{
	// setAttribute("rst-x", ""), toggleAttribute.
	regexp.MustCompile(`\b(?:setAttribute|toggleAttribute)\(\s*` +
		jsQuote + `(` + jsText + `)`),
	// classList.add("is-x", "rst-x"): the hook in any position.
	regexp.MustCompile(`\bclassList\.(?:add|toggle)\(([^)]*)\)`),
	// el("div", "rst-combo"): select.js's element-building helper.
	regexp.MustCompile(`\bel\(\s*` + jsQuote + `[a-z0-9]+` + jsQuote +
		`\s*,\s*` + jsQuote + `(` + jsText + `)`),
	// className = "rst-a rst-b".
	regexp.MustCompile(
		`\.className\s*\+?=\s*` + jsQuote + `(` + jsText + `)`),
	// addRow(label, meta, date, hasTime, variant, "rst-dtp-quick"):
	// datetime.js hands its last argument to setAttribute.
	regexp.MustCompile(`\baddRow\([^;]*,\s*` + jsQuote + `(` + jsText + `)` +
		jsQuote + `\)`),
}

// literal is a string literal in a script and the line it starts on.
type literal struct {
	text string
	line int
}

// scanScript splits a script into its code with the comments blanked
// out (newlines kept, so lines still count) and its string literals,
// in any of the three quotings. A comment can say "doesn't", so the
// literals cannot be found by a regular expression over the raw text.
func scanScript(source string) (code string, literals []literal) {
	var out strings.Builder
	line := 1
	previous := byte('\n') // last byte of code, to tell a regex from a divide
	for at := 0; at < len(source); {
		character := source[at]
		switch {
		case character == '/' && at+1 < len(source) && source[at+1] == '/':
			for at < len(source) && source[at] != '\n' {
				at++
			}
		case character == '/' && at+1 < len(source) && source[at+1] == '*':
			end := strings.Index(source[at+2:], "*/")
			if end < 0 {
				end = len(source) - at - 4
			}
			comment := source[at : at+end+4]
			line += strings.Count(comment, "\n")
			out.WriteString(strings.Repeat("\n", strings.Count(comment, "\n")))
			at += end + 4
		case character == '/' &&
			strings.IndexByte("(,=:[!&|?{};\n", previous) >= 0:
			start, inClass := at, false
			for at++; at < len(source) && (inClass || source[at] != '/'); at++ {
				switch source[at] {
				case '\\':
					at++
				case '[':
					inClass = true
				case ']':
					inClass = false
				}
			}
			at++
			out.WriteString(source[start:min(at, len(source))])
			previous = 'x'
		case character == '"' || character == '\'' || character == '`':
			start, startLine := at, line
			for at++; at < len(source) && source[at] != character; at++ {
				if source[at] == '\\' {
					at++
				}
				if at < len(source) && source[at] == '\n' {
					line++
				}
			}
			at++
			end := min(at, len(source))
			literals = append(literals,
				literal{source[start+1 : max(end-1, start+1)], startLine})
			out.WriteString(source[start:end])
			previous = 'x'
		default:
			if character == '\n' {
				line++
			}
			out.WriteByte(character)
			if character != ' ' && character != '\t' {
				previous = character
			}
			at++
		}
	}
	return out.String(), literals
}

// concatenated matches a literal that ends in the start of a name, so a
// script is about to finish it at run time ("rst-" + kind): no reader
// of the source can tell which hook that makes.
var concatenated = regexp.MustCompile(
	`(?:^|[^A-Za-z0-9_-])(rst-(?:[a-z0-9_-]*-)?)$`)

// createdHooks reads the hooks a script creates, sorted, each once.
func createdHooks(source string) []string {
	code, literals := scanScript(source)
	var found []string
	for _, form := range creatingForms {
		for _, match := range form.FindAllStringSubmatch(code, -1) {
			found = append(found, vendored.HooksIn([]byte(match[1]))...)
		}
	}
	for _, text := range literals {
		if strings.Contains(text.text, "<") {
			found = append(found, vendored.HooksIn([]byte(text.text))...)
		}
	}
	slices.Sort(found)
	return slices.Compact(found)
}

// unaccounted returns a message for every hook a script names in a
// string literal that is neither one it creates nor one it is listed
// as only reading, and for every literal that ends in the start of a
// name. It is what keeps the creating forms above from missing a hook
// added in a form nobody thought of.
func unaccounted(source string, created, readOnly []string) []string {
	_, literals := scanScript(source)
	var problems []string
	for _, text := range literals {
		if match := concatenated.FindStringSubmatch(text.text); match != nil &&
			!slices.Contains(readOnly, match[1]) {
			problems = append(problems, fmt.Sprintf(
				"line %d: %q ends in a name started at run time",
				text.line, text.text))
		}
		for _, hook := range vendored.HooksIn([]byte(text.text)) {
			if !slices.Contains(created, hook) &&
				!slices.Contains(readOnly, hook) {
				problems = append(problems, fmt.Sprintf(
					"line %d: %s is in %q", text.line, hook, text.text))
			}
		}
	}
	return problems
}

func TestHookReaderSeesEachCreatingForm(t *testing.T) {
	source := `
		a.setAttribute("rst-one", "");
		b.toggleAttribute("rst-two", on);
		c.classList.add("rst-three");
		d.classList.toggle("rst-four", on);
		const e = el("div", "rst-five");
		f.className = "rst-six";
		addRow(label, "", date, true, "", "rst-seven");
		g.innerHTML = "<span rst-eight class='rst-nine'></span>";
		a.setAttribute('rst-ten', "");
		b.toggleAttribute(` + "`rst-eleven`" + `, on);
		c.classList.add("is-x", "rst-twelve");
		d.classList.toggle("is-x", on, "rst-thirteen");
		f.className = "rst-fourteen rst-fifteen";
		const h = el('span', 'rst-sixteen');
` +
		// The class attribute is the case here.
		// markup-spelling: old-spelling begin
		`		g.innerHTML = '<span rst-seventeen ` +
		`class="rst-eighteen"></span>';
` +
		// markup-spelling: old-spelling end
		`		g.innerHTML = ` + "`<span rst-nineteen>${x}</span>`" + `;
		addRow(label, "", date, true, "", 'rst-twenty');
		// i.setAttribute("rst-comment", "") and doesn't end a string
		i.querySelector("[rst-read]");
		i.closest(".rst-read");
		j.hasAttribute("rst-read");
		k.removeAttribute("rst-read");
		l.style.setProperty("--rst-read", "1");
		m.setAttribute("data-rst-read", "");
		const id = "rst-read-" + n;
	`
	want := []string{"rst-eight", "rst-eighteen", "rst-eleven", "rst-fifteen",
		"rst-five", "rst-four", "rst-fourteen", "rst-nine", "rst-nineteen",
		"rst-one", "rst-seven", "rst-seventeen", "rst-six", "rst-sixteen",
		"rst-ten", "rst-thirteen", "rst-three", "rst-twelve", "rst-twenty",
		"rst-two"}
	if got := createdHooks(source); !slices.Equal(got, want) {
		t.Errorf("createdHooks = %v, want %v", got, want)
	}
}

func TestUnaccountedHooksNamesWhatTheFormsMiss(t *testing.T) {
	source := `
		a.setAttribute("rst-known", "");
		b.querySelector("[rst-selector]");
		const prefix = "rst-" + kind;
		const id = "rst-id-" + n;
		place(row, 'rst-through-a-helper');
		put(` + "`rst-in-a-template`" + `);
	`
	got := unaccounted(source, []string{"rst-known"}, []string{"rst-id-"})
	for _, want := range []string{
		"line 4: ",
		"rst-selector",
		"rst-through-a-helper",
		"rst-in-a-template",
	} {
		if !slices.ContainsFunc(got, func(problem string) bool {
			return strings.Contains(problem, want)
		}) {
			t.Errorf("unaccounted = %q, want a problem naming %q", got, want)
		}
	}
	if len(got) != 4 {
		t.Errorf("unaccounted = %q, want four problems", got)
	}
}

func TestEachScriptCreatesExactlyItsListedHooks(t *testing.T) {
	assets, ok := VendoredAssets("day")
	if !ok {
		t.Fatal(`VendoredAssets("day") reported an unknown theme`)
	}
	for _, name := range vendored.Names() {
		if !strings.HasSuffix(name, ".js") {
			continue
		}
		source, ok := assets[name]
		if !ok {
			t.Errorf("%s is vendored but VendoredAssets has no bytes", name)
			continue
		}
		got := createdHooks(string(source))
		want := vendored.CreatedHooks(name)
		if !slices.Equal(got, want) {
			t.Errorf("%s creates %v, but vendored.CreatedHooks lists %v",
				name, got, want)
		}
	}
}

// readOnlyHooks are the hooks a script names in a string and does not
// create: the selectors it finds elements by, which the page's markup
// has to carry already. A hook added to a script has to be created
// (and listed in vendored.CreatedHooks) or be listed here, so none
// slips past the creating forms. A name ending in "-" is the start of
// an id the script finishes at run time, never a hook.
var readOnlyHooks = map[string][]string{
	"rastrillo.js": {
		"rst-scheme", // the event fiesta.js fires once a scheme is chosen
	},
	"fiesta.js": {
		"rst-scheme", // SCHEME: the form that holds the scheme choice;
		// also the name of the event it fires once one is chosen
		"rst-dropdown",      // MENUS: an open menu to close on an outside click
		"rst-menu-group",    // MENUS: a nested menu
		"rst-row-menu",      // MENUS: a row's menu
		"rst-shell-topbar",  // MENUS: the narrow shell's menu sits under it
		"rst-shell-menu",    // MENUS: the shell's narrow menu card
		"rst-shell-console", // MENUS: the console shell's narrow menu
		"rst-shell-console--index",
		"rst-shell-console--page", // MENUS: the class forms of the above
		"rst-shell__menu",         // MENUS: the class form of rst-shell-menu
		"rst-shell-tail",          // TAIL: found to keep it clear of a menu
		"rst-shell__tail",         // TAIL: the class form
		"rst-switch-track",        // TRACK: the knob a pointer drags
		"rst-switch__track",       // TRACK: the class form
		"rst-switch",              // the label that holds a track
		"rst-tblock-head",         // the toggle block's head, a track's label
		"rst-tblock__head",        // the class form
	},
	"copy.js": {
		"rst-snippet",         // BOX: the snippet whose code it copies
		"rst-snippet-copy",    // BUTTON: the hidden button it shows
		"rst-snippet__copy",   // BUTTON: the class form
		"rst-snippet-status",  // STATUS: the line it speaks in
		"rst-snippet__status", // STATUS: the class form
	},
	"anchor.js": {
		"rst-dropdown",       // DROPDOWN: the menus it positions
		"rst-dropdown-menu",  // PANEL: the panel it measures
		"rst-dropdown__menu", // PANEL: the class form
	},
	"shell.js": {
		"rst-shell-sidebar",        // ROOT, INDEX, PAGE: the shell it manages
		"rst-shell-console",        // ROOT, INDEX, PAGE: the console form
		"rst-shell-sidebar--index", // INDEX: the class forms of the above
		"rst-shell-sidebar--page",  // PAGE: likewise
		"rst-shell-console--index", // INDEX: likewise
		"rst-shell-console--page",  // PAGE: likewise
		"rst-shell-back",           // BACK: the back strip's link; also a
		// sessionStorage key (WENT_BACK), which is no hook at all
		"rst-shell__back",  // BACK: the class form
		"rst-shell-nav",    // NAV: the index's rows
		"rst-shell__nav",   // NAV: the class form
		"rst-shell-return", // RETURN: a sessionStorage key
	},
	"light.js": {
		"rst-btn",   // BTN: the buttons it lights
		"rst-box",   // CARD: the cards it lights
		"rst-card",  // CARD
		"rst-stats", // CARD
		"rst-list",  // CARD
	},
	"select.js": {
		"rst-select-", // the start of the combo's id, finished with a number
	},
	"datetime.js": {
		"rst-dtp-", // the start of the field's id, finished with a number
	},
}

func TestEveryHookAScriptNamesIsCreatedOrOnlyRead(t *testing.T) {
	assets, ok := VendoredAssets("day")
	if !ok {
		t.Fatal(`VendoredAssets("day") reported an unknown theme`)
	}
	for _, name := range vendored.Names() {
		if !strings.HasSuffix(name, ".js") {
			continue
		}
		problems := unaccounted(string(assets[name]),
			vendored.CreatedHooks(name), readOnlyHooks[name])
		for _, problem := range problems {
			t.Errorf("%s, %s: create it and list it in vendored.CreatedHooks,"+
				" or list it as read-only in readOnlyHooks", name, problem)
		}
	}
}

func TestCreatedHooksKnowsNoOtherScript(t *testing.T) {
	if got := vendored.CreatedHooks("unknown.js"); got != nil {
		t.Errorf("CreatedHooks(unknown.js) = %v, want nil", got)
	}
}

func TestLightLightsExactlyLitHooks(t *testing.T) {
	source := string(LightJS())
	var lit []string
	for _, constant := range []string{"BTN", "CARD"} {
		pattern := regexp.MustCompile(`var ` + constant + ` = "([^"]*)"`)
		match := pattern.FindStringSubmatch(source)
		if match == nil {
			t.Fatalf("light.js no longer declares %s as a string", constant)
		}
		lit = append(lit, vendored.HooksIn([]byte(match[1]))...)
	}
	slices.Sort(lit)
	lit = slices.Compact(lit)
	want := slices.Clone(vendored.LitHooks())
	slices.Sort(want)
	if !slices.Equal(lit, want) {
		t.Errorf("light.js lights %v, but vendored.LitHooks lists %v",
			lit, want)
	}
}
