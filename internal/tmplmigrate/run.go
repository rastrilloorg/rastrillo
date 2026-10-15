package tmplmigrate

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Appendix C.5's flag help and C.2's op.migrate.* report lines.
const (
	flagWrite     = "rewrite the templates (without it, report what would change and change nothing)"
	msgGenStale   = "%s holds generated templates in the old syntax; run rastrillo generate"
	msgFileLine   = "%-64s calls %3d  dicts %2d  wrapped %2d  preludes %2d"
	msgIOError    = "migrate-templates: %v"
	modeDry       = "dry run"
	modeWritten   = "written"
	msgSummary    = "%s: %d templates; %d locale calls, %d dict calls, %d wrapped calls, %d preludes"
	msgRefused    = "refused: %s"
	msgReview     = "review (if this data is a struct, it needs an L field): %s"
	msgNoData     = "no data: %s"
	goLabel       = "go"
	goReviewLabel = "go (review)"
	msgGoLine     = "%s: %s: %s; %s"
	msgPending    = "pending: %d templates need the automatic rewrite; run with -write"
)

// Run is `rastrillo migrate-templates [-write] PATH...`, and the
// template preflight `rastrillo doctor` runs as a dry run for every
// app, money or not. It rewrites the .html files under each PATH (see
// Rewrite), and type-checks any Go there (CheckGo) for what editing
// templates cannot fix.
//
// It exits 1 while anything is left to do, which is what lets doctor
// fail on it: in a dry run, any template it would rewrite counts, as do
// a refused file, a Go finding, a struct data site to review, a
// template call with no data and a stale gen/. After -write, the
// templates it rewrote no longer count. It exits 2 for a usage or I/O
// error, and 0 when nothing is left.
func Run(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("migrate-templates", flag.ContinueOnError)
	fl.SetOutput(stderr)
	write := fl.Bool("write", false, flagWrite)
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if fl.NArg() == 0 {
		fl.Usage()
		return 2
	}
	m := migration{out: stdout, write: *write}
	for _, root := range fl.Args() {
		if err := m.walk(root); err != nil {
			fmt.Fprintf(stderr, msgIOError+"\n", err)
			return 2
		}
		if hasGo(root) {
			found, err := CheckGo(root)
			if err != nil {
				fmt.Fprintf(stderr, msgIOError+"\n", err)
				return 2
			}
			for _, g := range found {
				g.Pos = filepath.Join(root, g.Pos)
				m.goFindings = append(m.goFindings, g)
			}
		}
	}
	return m.report()
}

// migration is one Run's findings, across every PATH.
type migration struct {
	out        io.Writer
	write      bool
	files      int // templates the rewrite changes (or changed, with -write)
	total      Stats
	refused    []string
	genStale   []string
	goFindings []GoFinding
}

// walk rewrites root's templates, skipping gen/ and static/.
//
// gen/ is skipped because rastrillo generate owns it: an edit there is
// overwritten by the next generate, which is also what migrates it, so
// the only useful report is to run generate. static/ holds vendored
// copies whose bytes doctor compares with the library's, so a rewrite
// there would read as drift.
func (m *migration) walk(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "static":
				return filepath.SkipDir
			case "gen":
				if genPending(path) {
					m.genStale = append(m.genStale, fmt.Sprintf(msgGenStale, path))
				}
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".html") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// Any rewrite error leaves this file unchanged and is reported
		// with the rest, rather than ending the run: one template that
		// is refused, does not parse or trips a bug must not hide what
		// the run found in every other file.
		out, st, err := rewriteContained(path, string(src))
		if err != nil {
			m.refused = append(m.refused, err.Error())
			return nil
		}
		m.total.Add(st)
		if out == string(src) {
			return nil
		}
		m.files++
		fmt.Fprintf(m.out, msgFileLine+"\n", path, st.Calls, st.Dicts, st.Wrapped, st.Preludes)
		if m.write {
			return os.WriteFile(path, []byte(out), 0o644)
		}
		return nil
	})
}

// genPending reports whether a gen/ directory holds templates the
// rewrite would change. An error counts too: a generated template that
// is refused or does not parse is fixed by the same regeneration.
func genPending(dir string) bool {
	_, st, err := RewriteFS(os.DirFS(dir), ".")
	return err != nil || st.Calls+st.Dicts+st.Wrapped+st.Preludes > 0
}

// report prints the summary and everything left to do, and returns the
// exit code.
func (m *migration) report() int {
	mode := modeDry
	if m.write {
		mode = modeWritten
	}
	w := m.out
	fmt.Fprintf(w, msgSummary+"\n", mode, m.files, m.total.Calls, m.total.Dicts, m.total.Wrapped, m.total.Preludes)
	for _, r := range m.refused {
		fmt.Fprintf(w, "  "+msgRefused+"\n", r)
	}
	for _, r := range m.total.Review {
		fmt.Fprintf(w, "  "+msgReview+"\n", r)
	}
	for _, r := range m.total.NoData {
		fmt.Fprintf(w, "  "+msgNoData+"\n", r)
	}
	for _, g := range m.goFindings {
		label := goLabel
		if g.Review {
			label = goReviewLabel
		}
		fmt.Fprintf(w, "  "+msgGoLine+"\n", label, g.Pos, g.What, g.Fix)
	}
	for _, g := range m.genStale {
		fmt.Fprintln(w, "  "+g)
	}
	pending := 0
	if !m.write {
		pending = m.files
	}
	if pending > 0 {
		fmt.Fprintf(w, "  "+msgPending+"\n", pending)
	}
	if pending+len(m.refused)+len(m.total.Review)+len(m.total.NoData)+len(m.goFindings)+len(m.genStale) > 0 {
		return 1
	}
	return 0
}

// hasGo reports whether the directory root holds any Go source, so a
// templates-only directory (or a single template) is not type-checked.
func hasGo(root string) bool {
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return false
	}
	found := false
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".go") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}
