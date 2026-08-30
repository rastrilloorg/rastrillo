package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/carlosframework/rastrillo/ui"
)

// runDoctor implements `rastrillo doctor [--fix] [--force] [--theme
// <name>] [dir]`: compare an app's vendored static files against the
// library copies this binary carries, report what differs, and with
// --fix re-copy it (design doc: 2026-08-28-design-system-design
// §6-v2.3).
//
// # What it is for, and what it is not
//
// tokens.css and the theme are written into an app's static/ directory
// at scaffold time and frozen there, while the partials they style keep
// upgrading with the module — so an app can run new markup against old
// CSS and see nothing worse than a slightly wrong screen. Re-copying is
// a step in a runbook, which means a step people skip.
//
// It is NOT the only thing standing between an app and silent drift,
// and the help text says so. `rastrillo new` already scaffolds
// TestVendoredAssetsMatchTheLibrary into every app, and that test is
// the standing gate: it runs in CI, on every commit, without anyone
// remembering to. What doctor adds is the four things the test cannot
// do — re-copy rather than telling a human to, work on an app that
// never had the test or deleted it, say HOW a file differs rather than
// that it does, and run from outside the app, which is what asking "is
// this app safe to upgrade yet?" about someone else's repository needs.
//
// # The trap this exists to avoid
//
// This CLI carries its own compiled-in copy of the ui package; the app
// has its own required version in go.mod. They are frequently
// different, and that difference is NOT drift. An app deliberately on
// v0.19.0, checked by a v0.20.0 CLI, has files that correctly match
// v0.19.0. Reporting that as damage would be worse than saying nothing,
// because it teaches people to ignore the tool. So doctor reads both
// versions, says which one the comparison is against, and makes a
// mismatch the headline rather than a footnote.
//
// A false positive costs this tool everything, so three things are
// deliberately NOT reported as drift: a theme.css that matches no
// shipped theme (a hand-edited theme is supported — see themeIdentity),
// a file the app has recorded as a deliberate edit (vendoredIsMine),
// and any difference at all when the versions disagree.
func runDoctor(args []string) error {
	fset := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fix := fset.Bool("fix", false, "re-copy each drifted file from this binary's library copy")
	force := fset.Bool("force", false, "with --fix: re-copy across a version mismatch, and over files recorded as deliberate edits")
	theme := fset.String("theme", "", "the theme static/theme.css should match (default: the app's own pin, else whichever shipped theme it matches)")
	// A usage mistake exits 2, which is what this command's own
	// exit-code table promises and what a CI branching on it will look
	// for. flag.ContinueOnError has already printed the error and the
	// usage, so the code travels out with no second message.
	if err := fset.Parse(args); err != nil {
		return exitError{code: 2}
	}
	dir := "."
	if rest := fset.Args(); len(rest) > 0 {
		dir = rest[0]
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if *theme != "" {
		if _, ok := ui.ThemeCSS(*theme); !ok {
			return exitError{code: 2, msg: fmt.Sprintf("unknown theme %q: known themes are %s",
				*theme, strings.Join(ui.ThemeNames(), ", "))}
		}
	}

	rep, err := diagnose(dir, *theme)
	if err != nil {
		return err
	}
	rep.print(os.Stdout, *fix)
	if *fix {
		return rep.applyFix(os.Stdout, *force)
	}
	return rep.exit()
}

// The exit codes, which are the CI-facing surface. Each means one
// thing, so a script can act on it: 0 clean, 1 an error, 2 usage (main
// already uses it), 3 drift, 4 a version mismatch that makes the
// comparison inconclusive.
//
// Skew gets its own code rather than being folded into drift because
// the two call for opposite actions. Drift means "re-copy these files".
// Skew means "do not re-copy anything yet — upgrade the module, or
// pin the CLI, and ask again." A CI that treated them alike would
// answer an upgrade question with a file list.
const (
	exitDrift = 3
	exitSkew  = 4
)

// exitError carries an exit code out of a subcommand. Its message may
// be empty, for a command like doctor that has already printed its own
// report and wants a status, not a second summary on stderr.
type exitError struct {
	code int
	msg  string
}

func (e exitError) Error() string { return e.msg }

// fileState is one vendored file's verdict.
type fileState int

const (
	fileOK      fileState = iota // byte-identical to the library copy
	fileDiffers                  // present, and different
	fileAbsent                   // the library ships it; this app has no copy
	fileMine                     // the app recorded this edit as deliberate
	fileUnknownTheme
	fileUnreadable
)

// vendoredFile is one row of the report.
type vendoredFile struct {
	name  string
	path  string
	state fileState
	err   error
	app   []byte
	lib   []byte
}

// report is everything doctor found, separated from printing it so the
// finding and the wording can be tested apart.
type report struct {
	dir        string
	staticDir  string // relative to dir, for display
	module     string
	cliVersion string
	cliTagged  bool   // the CLI's version came from an install tag, not the fallback
	appVersion string // what the app's go.mod requires; empty if unreadable
	replaced   string // the target of a replace directive, if any
	theme      string // the theme the comparison used; empty when unidentified
	themeFrom  string // how the theme was decided, for the one-line explanation
	pinFile    string // relative path of the app's vendored_test.go, if it has one
	pinLegacy  bool   // that pin is the older map-literal shape, with no vendoredIsMine
	files      []vendoredFile
}

// skewed reports whether the CLI and the app are on different rastrillo
// versions, which makes every difference below it expected rather than
// wrong.
//
// A replace directive is not skew. It points the app at a checkout
// rather than a tag, so there is no second version to disagree with —
// there is only the question of whether this binary was built from that
// checkout, which doctor cannot answer and therefore says out loud
// instead of guessing.
func (r *report) skewed() bool {
	return r.replaced == "" && r.appVersion != "" && r.appVersion != r.cliVersion
}

// drifted reports whether any file the app has not claimed differs from
// the library copy.
//
// An ABSENT file is not drift. The framework documents deleting
// select.js or datetime.js as a supported choice for an app with no big
// select and no date field, so an app that took that choice must not be
// told it is broken — and an app that predates a file the library has
// since added has not damaged anything either. Both get a line in the
// report saying the library ships it; neither gets a failing exit code
// for it. --fix still adds them, because --fix is an explicit request.
func (r *report) drifted() bool {
	for _, f := range r.files {
		if f.state == fileDiffers {
			return true
		}
	}
	return false
}

// exit maps the findings onto the exit codes.
func (r *report) exit() error {
	switch {
	case r.skewed():
		return exitError{code: exitSkew}
	case r.drifted():
		return exitError{code: exitDrift}
	}
	return nil
}

// diagnose does the reading: where the app keeps its vendored files,
// what version it is on, which theme it chose, and how each file
// compares. It writes nothing.
func diagnose(dir, themeFlag string) (*report, error) {
	module, err := modulePath(dir)
	if err != nil {
		return nil, fmt.Errorf("%s does not look like a Go module: %w", dir, err)
	}
	r := &report{dir: dir, module: module}
	r.cliVersion, r.cliTagged = rastrilloVersionTagged()
	r.appVersion, r.replaced, err = moduleRequirement(dir, rastrilloModule)
	if err != nil {
		return nil, err
	}

	pkg := packageName(filepath.Base(module))
	staticDir, err := findStaticDir(dir, pkg)
	if err != nil {
		return nil, err
	}
	r.staticDir = rel(dir, staticDir)

	pinPath, pin := readPin(dir, pkg)
	r.pinFile, r.pinLegacy = rel(dir, pinPath), pin.legacy

	themeCSS, _ := os.ReadFile(filepath.Join(staticDir, "theme.css"))
	r.theme, r.themeFrom = themeIdentity(themeFlag, pin.theme, themeCSS)

	// Without a theme there is no theme.css to compare against, but
	// every other file is still comparable — so the set is built from
	// the day theme (whose non-theme entries are the same bytes for
	// every theme) and theme.css is handled on its own terms below.
	lookupTheme := r.theme
	if lookupTheme == "" {
		lookupTheme = ui.ThemeNames()[0]
	}
	assets, ok := ui.VendoredAssets(lookupTheme)
	if !ok {
		return nil, fmt.Errorf("unknown theme %q", lookupTheme)
	}

	for _, name := range ui.VendoredNames() {
		f := vendoredFile{name: name, path: filepath.Join(staticDir, name), lib: assets[name]}
		switch {
		case pin.mine[name]:
			f.state = fileMine
		case name == "theme.css" && r.theme == "":
			f.state = fileUnknownTheme
		default:
			f.app, f.err = os.ReadFile(f.path)
			switch {
			case f.err != nil && os.IsNotExist(f.err):
				f.state = fileAbsent
			case f.err != nil:
				f.state = fileUnreadable
			case bytes.Equal(f.app, f.lib):
				f.state = fileOK
			case pin.unlisted[name]:
				// An older pin does not mention this file and the file
				// differs. Deleting its line is what the scaffold of
				// that era told people to do for a deliberate edit, and
				// the file having an edit in it is the reading that
				// fits. Honour it rather than overwrite it.
				f.state = fileMine
			default:
				f.state = fileDiffers
			}
		}
		r.files = append(r.files, f)
	}
	return r, nil
}

// findStaticDir locates the directory the vendored files live in.
// rastrillo new writes internal/<pkg>/static; examples/blog and apps
// that predate that layout keep a static/ at the root; and an app free
// to rename its internal package gets found by looking for the files
// themselves rather than by assuming a name.
func findStaticDir(dir, pkg string) (string, error) {
	candidates := []string{filepath.Join(dir, "internal", pkg, "static")}
	if matches, err := filepath.Glob(filepath.Join(dir, "internal", "*", "static")); err == nil {
		candidates = append(candidates, matches...)
	}
	candidates = append(candidates, filepath.Join(dir, "static"))

	seen := map[string]bool{}
	for _, c := range candidates {
		if seen[c] {
			continue
		}
		seen[c] = true
		for _, name := range ui.VendoredNames() {
			if _, err := os.Stat(filepath.Join(c, name)); err == nil {
				return c, nil
			}
		}
	}
	// Nothing found — which is a real state a real rastrillo app can be
	// in. examples/notes is manifest-shaped and has no static/ at all,
	// and doubting whether it is a rastrillo app would be both rude and
	// wrong. Say what is actually true: there is nothing here to
	// compare, and name the places that were looked in so someone whose
	// app keeps its assets elsewhere can point doctor at them.
	return "", fmt.Errorf("nothing vendored to check in %s: no %s under %s, %s or %s",
		dir, strings.Join(ui.VendoredNames(), ", "),
		rel(dir, candidates[0]), filepath.Join("internal", "*", "static"), "static")
}

// pinInfo is what the app's own vendored_test.go says: the theme it was
// scaffolded with, which files it has stopped pinning because the app
// edited them on purpose, and which of the two shapes said so — the
// advice doctor prints has to name a thing the app's own file actually
// has.
type pinInfo struct {
	theme string
	mine  map[string]bool
	// unlisted names the vendored files an OLDER pin does not mention.
	// It is not a claim on its own — see the comment on that branch of
	// readPin, and how diagnose resolves it.
	unlisted map[string]bool
	legacy   bool // the pre-vendoredIsMine shape: a map literal you deleted a line from
}

var (
	vendoredThemeRE = regexp.MustCompile(`vendoredTheme\s*(?:=|:=)\s*"([^"]*)"`)
	vendoredMineRE  = regexp.MustCompile(`vendoredIsMine\s*=\s*map\[string\]bool\{([^}]*)\}`)
	mineEntryRE     = regexp.MustCompile(`"([^"]+)"\s*:\s*true`)
)

// readPin finds and reads the app's vendored_test.go. Everything it
// returns is optional: an app that never had the test, or deleted it,
// is one of the cases doctor exists for.
//
// It reads the pin and nothing else: the older shape's unlisted names
// are resolved against the app's files by diagnose, which is where the
// library's own bytes are already in hand.
func readPin(dir, pkg string) (string, pinInfo) {
	pin := pinInfo{mine: map[string]bool{}, unlisted: map[string]bool{}}
	path := ""
	candidates := []string{filepath.Join(dir, "internal", pkg+"test", "vendored_test.go")}
	if matches, err := filepath.Glob(filepath.Join(dir, "internal", "*", "*_test.go")); err == nil {
		candidates = append(candidates, matches...)
	}
	var src string
	for _, c := range candidates {
		b, err := os.ReadFile(c)
		if err != nil {
			continue
		}
		if !strings.Contains(string(b), "TestVendoredAssetsMatchTheLibrary") {
			continue
		}
		path, src = c, string(b)
		break
	}
	if src == "" {
		return "", pin
	}
	if m := vendoredThemeRE.FindStringSubmatch(src); m != nil {
		pin.theme = m[1]
	}
	if m := vendoredMineRE.FindStringSubmatch(src); m != nil {
		// The current shape: an app records a deliberate edit by
		// naming the file here.
		for _, entry := range mineEntryRE.FindAllStringSubmatch(uncommented(m[1]), -1) {
			pin.mine[entry[1]] = true
		}
		return path, pin
	}
	// The older shape, which pinned files by listing them in a map
	// literal and told you to DELETE a line when the edit was
	// deliberate. Absence meant "this file is mine", so read it that
	// way — an app that followed the instruction it was given must not
	// have its deliberate edit reported as damage, and must not have it
	// overwritten by --fix.
	//
	// With one condition, and it is the whole reason this branch is
	// longer than it looks. The original pin (36ee472, #73) listed
	// THREE files; theme.css and datetime.js joined the vendored set
	// afterwards (#104, #106). So for an app scaffolded in that window,
	// a name missing from its pin means "predates it", not "deleted on
	// purpose" — there was never a line there to delete. Reading it as
	// a claim tells that app something false about its own history, and
	// makes --fix withhold a file it genuinely needs.
	//
	// So an unlisted name here is a CANDIDATE, not a claim. diagnose
	// resolves it against the file itself, which is the only evidence
	// that can tell the two readings apart:
	//
	//	absent            → `absent`. True under either reading, and the
	//	                    one state --fix acts on without --force.
	//	present, matching → compared normally. A pin line is deleted to
	//	                    protect an edit; a file identical to the
	//	                    library has no edit to protect, so there is
	//	                    nothing to honour and every reason to keep
	//	                    checking it. (This is also what stops --fix
	//	                    from writing a file and thereby exempting it
	//	                    from every future check.)
	//	present, differing → `yours`. Here the two readings genuinely
	//	                    diverge, and the safe one is the one that
	//	                    never overwrites a person's work.
	listed := false
	for _, name := range ui.VendoredNames() {
		if strings.Contains(src, `"`+name+`"`) {
			listed = true
		}
	}
	if !listed {
		return path, pin
	}
	pin.legacy = true
	for _, name := range ui.VendoredNames() {
		if !strings.Contains(src, `"`+name+`"`) {
			pin.unlisted[name] = true
		}
	}
	return path, pin
}

// uncommented strips // comments, so the commented-out example line the
// scaffold writes inside vendoredIsMine is not read as a real entry.
func uncommented(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// themeIdentity decides which theme static/theme.css is, and reports
// how it decided so the line above the file list can say. An empty name
// means "custom or drifted", which is a supported thing and not a
// finding: a hand-written theme is exactly as legitimate as a shipped
// one, and picking the closest shipped theme to diff against would
// report an app's own design as damage.
func themeIdentity(flag, pinned string, css []byte) (name, from string) {
	if flag != "" {
		return flag, "--theme"
	}
	if pinned != "" {
		if _, ok := ui.ThemeCSS(pinned); ok {
			return pinned, "pinned by the app"
		}
		// The app names a theme this binary does not ship. That is a
		// version question, not a drift question, so say which.
		return "", "the app pins " + pinned + ", which this binary does not ship"
	}
	for _, n := range ui.ThemeNames() {
		if lib, ok := ui.ThemeCSS(n); ok && bytes.Equal(css, lib) {
			return n, "matched by content"
		}
	}
	if len(css) == 0 {
		return "", "no theme.css"
	}
	return "", "custom or drifted"
}

// print writes the report. The version line comes first and always,
// because everything under it is only true relative to a version.
// fixing suppresses the closing advice, since a run that is already
// re-copying does not need to be told how to.
func (r *report) print(w io.Writer, fixing bool) {
	fmt.Fprintf(w, "rastrillo doctor: %s (%s)\n", r.module, r.staticDir)

	switch {
	case r.replaced != "":
		fmt.Fprintf(w, "  This app replaces rastrillo with %s, so it has no required version to compare.\n", r.replaced)
		fmt.Fprintf(w, "  Comparing against this binary's own copy (%s) — right only if it was built from that tree.\n", r.cliVersion)
	case r.appVersion == "":
		fmt.Fprintf(w, "  This app's go.mod does not require %s.\n", rastrilloModule)
		fmt.Fprintf(w, "  Comparing against %s, this binary's own copy.\n", r.cliVersion)
	case r.skewed():
		fmt.Fprintf(w, "  rastrillo doctor is %s; this app requires %s.\n", r.cliVersion, r.appVersion)
		fmt.Fprintf(w, "  Comparing against %s — upgrade the module first, or these differences are expected.\n", r.cliVersion)
	default:
		fmt.Fprintf(w, "  rastrillo doctor and this app are both on %s. Comparing against %s.\n", r.cliVersion, r.cliVersion)
	}
	if !r.cliTagged {
		fmt.Fprintf(w, "  (This binary carries no install tag, so %s is its fallback constant, not a fact.)\n", r.cliVersion)
	}
	if r.theme != "" {
		fmt.Fprintf(w, "  theme: %s (%s)\n", r.theme, r.themeFrom)
	} else {
		fmt.Fprintf(w, "  theme: %s — not compared\n", r.themeFrom)
	}
	fmt.Fprintln(w)

	for _, f := range r.files {
		switch f.state {
		case fileOK:
			fmt.Fprintf(w, "  ok       %s\n", f.name)
		case fileMine:
			fmt.Fprintf(w, "  yours    %s (recorded as a deliberate edit in %s)\n", f.name, r.pinFile)
		case fileUnknownTheme:
			fmt.Fprintf(w, "  yours    %s (%s — a theme of your own is supported, so nothing is compared)\n", f.name, r.themeFrom)
		case fileAbsent:
			fmt.Fprintf(w, "  absent   %s (the library ships %s; deleting one you do not use is supported)\n",
				f.name, size(len(f.lib)))
		case fileUnreadable:
			fmt.Fprintf(w, "  error    %s: %v\n", f.name, f.err)
		case fileDiffers:
			fmt.Fprintf(w, "  drift    %s\n", f.name)
			for _, line := range describeDiff(f.app, f.lib) {
				fmt.Fprintf(w, "             %s\n", line)
			}
		}
	}
	fmt.Fprintln(w)

	// The summary counts what was compared, not what exists: a file the
	// app has claimed was never a candidate for drift, and saying "5
	// files, all matching" when one of them was skipped is the kind of
	// small lie that costs a tool its credibility.
	n, compared, absent := 0, 0, 0
	for _, f := range r.files {
		switch f.state {
		case fileDiffers:
			n++
			compared++
		case fileOK:
			compared++
		case fileAbsent:
			absent++
		}
	}
	left := len(r.files) - compared - absent
	if absent > 0 {
		fmt.Fprintf(w, "%s the library ships %s absent here — not drift. `--fix` would add %s.\n",
			plural(absent, "file"), isAre(absent), them(absent))
	}
	switch {
	case n == 0 && r.skewed():
		fmt.Fprintf(w, "Nothing differs, but this app is on %s and this binary is on %s — upgrade the module and run again.\n", r.appVersion, r.cliVersion)
	case n == 0:
		fmt.Fprintf(w, "%s compared, all matching the library%s.\n", plural(compared, "file"), leftAlone(left))
	case r.skewed():
		fmt.Fprintf(w, "%d of %s differ, which is what a %s app checked by a %s binary looks like.\n", n, plural(compared, "file"), r.appVersion, r.cliVersion)
		fmt.Fprintf(w, "Upgrade the module to %s and run this again before believing any of it.\n", r.cliVersion)
	case fixing:
		fmt.Fprintf(w, "%d of %s differ from the library copy%s.\n", n, plural(compared, "file"), leftAlone(left))
	default:
		fmt.Fprintf(w, "%d of %s differ from the library copy%s.\n", n, plural(compared, "file"), leftAlone(left))
		fmt.Fprintln(w, "Run `rastrillo doctor --fix` to re-copy them, or record the edit as deliberate:")
		fmt.Fprintln(w, r.recordAdvice())
	}
}

// isAre and them keep the absent-files line grammatical for one file
// and for several, which is the difference between a sentence and a
// template someone forgot to finish.
func isAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

func them(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

// leftAlone reports the files doctor did not compare, so the count
// above it is never mistaken for the whole set.
func leftAlone(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(" (%s left alone, above)", plural(n, "file"))
}

// recordAdvice says how to claim a file, in the words of the pin this
// app actually has. Telling an older app to add a name to a
// vendoredIsMine map its file does not contain sends someone looking
// for a thing that is not there — and the older shape has its own way
// of saying the same thing, which still works.
func (r *report) recordAdvice() string {
	where := "the app's vendored_test.go"
	if r.pinFile != "" {
		where = r.pinFile
	}
	if r.pinLegacy {
		return "delete the file's line from the pin in " + where + "."
	}
	return "add the file's name to vendoredIsMine in " + where + "."
}

// applyFix re-copies the drifted files. It writes to files a person
// owns, so it says exactly what it wrote, and it refuses twice: across
// a version mismatch, and over a file the app recorded as its own.
//
// The version refusal is the important one. Copying this binary's
// v0.20.0 assets into an app that compiles against v0.19.0 produces
// exactly the situation this tool exists to detect — new CSS against
// old markup — with the difference that doctor now calls it clean. A
// tool that can create the fault it reports on, silently, is worse than
// no tool. --force is there because a person who has read that sentence
// may still have a reason.
func (r *report) applyFix(w io.Writer, force bool) error {
	if r.skewed() && !force {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Refusing to --fix: this app requires %s and I carry %s.\n", r.appVersion, r.cliVersion)
		fmt.Fprintf(w, "Copying %s files into it would leave an app that compiles against %s running new CSS\n", r.cliVersion, r.appVersion)
		fmt.Fprintln(w, "against old markup — the exact fault this checks for — and doctor would then call it clean.")
		fmt.Fprintf(w, "Upgrade the module to %s first, or pass --force.\n", r.cliVersion)
		return exitError{code: exitSkew}
	}

	fmt.Fprintln(w)
	wrote, skipped := 0, 0
	for _, f := range r.files {
		switch f.state {
		case fileMine:
			if !force {
				fmt.Fprintf(w, "  left alone  %s (recorded as a deliberate edit; --force overrides)\n", f.name)
				skipped++
				continue
			}
		case fileUnknownTheme:
			// Never guess a theme. Re-copying here would replace an
			// app's own design with a shipped one on no evidence at all.
			fmt.Fprintf(w, "  left alone  %s (%s; pass --theme <name> to re-copy a shipped theme)\n", f.name, r.themeFrom)
			skipped++
			continue
		case fileOK:
			continue
		case fileUnreadable:
			fmt.Fprintf(w, "  left alone  %s: %v\n", f.name, f.err)
			skipped++
			continue
		}
		if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(f.path, f.lib, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", f.name, err)
		}
		switch {
		case f.state == fileAbsent:
			fmt.Fprintf(w, "  wrote       %s (%s, was missing)\n", rel(r.dir, f.path), size(len(f.lib)))
		default:
			fmt.Fprintf(w, "  re-copied   %s (%s -> %s)\n", rel(r.dir, f.path), size(len(f.app)), size(len(f.lib)))
		}
		wrote++
	}
	fmt.Fprintln(w)
	if wrote == 0 {
		fmt.Fprintf(w, "Nothing re-copied (%s left alone).\n", plural(skipped, "file"))
		if skipped > 0 {
			return nil
		}
		return r.exit()
	}
	fmt.Fprintf(w, "Re-copied %s from %s — still yours, so read the diff before you commit it.\n",
		plural(wrote, "file"), r.cliVersion)
	return nil
}

// describeDiff says HOW two files differ, in the few lines a person can
// act on: the sizes, where the first difference is, and the lines
// themselves. Not a full diff — a full diff is what `git diff` is for
// once you know which file to look at, and this is the sentence that
// tells you which.
func describeDiff(app, lib []byte) []string {
	a := lines(app)
	b := lines(lib)
	out := []string{fmt.Sprintf("yours %s lines, %s; the library's %s lines, %s",
		count(len(a)), size(len(app)), count(len(b)), size(len(lib)))}

	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	am, bm := a[p:len(a)-s], b[p:len(b)-s]
	if len(am) == 0 && len(bm) == 0 {
		return append(out, "the lines are identical — they differ in trailing whitespace only")
	}
	out = append(out, fmt.Sprintf("first difference at line %d: %s yours, %s the library's",
		p+1, plural(len(am), "line"), plural(len(bm), "line")))
	out = append(out, sample("yours  ", am, p)...)
	out = append(out, sample("library", bm, p)...)
	return out
}

// sample shows the first few lines of one side of a difference.
const sampleLines = 3

func sample(label string, block []string, offset int) []string {
	var out []string
	for i, line := range block {
		if i == sampleLines {
			out = append(out, fmt.Sprintf("%s   … and %d more", label, len(block)-sampleLines))
			break
		}
		out = append(out, fmt.Sprintf("%s %4d: %s", label, offset+i+1, clip(strings.TrimRight(line, " \t"), 68)))
	}
	return out
}

// lines splits a file for comparison, ignoring a trailing newline so a
// file's last line is not reported as an empty one.
func lines(b []byte) []string {
	s := strings.TrimSuffix(string(b), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// clip truncates by RUNES, not bytes. A hand-edited file in any of the
// eleven non-Latin locales the framework ships for can easily carry a
// comment that a byte slice would cut mid-sequence, and a drift sample
// that prints mojibake is a sample nobody can act on.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n]) + "…"
}

// size prints a byte count with thousands separators: the numbers here
// are five digits and read wrong without them.
func size(n int) string { return count(n) + " bytes" }

// count groups a number in threes.
func count(n int) string {
	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// plural is the difference between "1 files" and a sentence someone
// wrote on purpose.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return count(n) + " " + noun + "s"
}

// rel renders a path relative to the app for display, falling back to
// the absolute path when it is not under it.
func rel(base, path string) string {
	if path == "" {
		return ""
	}
	r, err := filepath.Rel(base, path)
	if err != nil || strings.HasPrefix(r, "..") {
		return path
	}
	return r
}
