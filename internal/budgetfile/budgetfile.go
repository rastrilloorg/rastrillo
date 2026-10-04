// Package budgetfile reads .rastrillo/budgets.txt, the one place an app
// records the exceptions to its budgets. One parser serves the CLI and
// perf/perftest, so the two can never disagree about what a line means.
//
// The defaults appear here and nowhere else. A record exists only where
// something cannot meet them, and every record carries a reason, because
// an exemption nobody can explain is one nobody will ever remove.
package budgetfile

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	FileName      = ".rastrillo/budgets.txt"
	DefaultSource = 5000
	DefaultTest   = 8000
	DefaultTime   = 10 * time.Second
	// BootChildEnv marks perftest.Boot's child process. budget.Main reads
	// it to stay silent there: the child's own measurement line would
	// otherwise be judged as a second run of the package.
	BootChildEnv = "RASTRILLO_BOOT_CHILD"
)

type Size struct {
	Dir          string
	Source, Test int // 0 means the record wrote "-": that column uses its default
	Line         int
	Reason       string
}

type Time struct {
	Dir    string
	Limit  time.Duration
	Line   int
	Reason string
}

type Perf struct {
	Screen string // "GET /notes/{id}", the name perf gives the route in production
	Limit  time.Duration
	Skip   bool
	Line   int
	Reason string
}

type Boot struct {
	Limit  time.Duration
	Line   int
	Reason string
}

type File struct {
	Sizes map[string]Size
	Times map[string]Time
	Perfs map[string]Perf
	Boot  *Boot
}

func empty() *File {
	return &File{Sizes: map[string]Size{}, Times: map[string]Time{}, Perfs: map[string]Perf{}}
}

// Parse refuses rather than guesses: every malformed line is reported
// with its number, all at once, so one fix-up pass clears the file.
func Parse(r io.Reader) (*File, error) {
	f := empty()
	var errs []error
	sc := bufio.NewScanner(r)
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if err := f.add(n, fields); err != nil {
			errs = append(errs, fmt.Errorf("%s line %d: %w", FileName, n, err))
		}
	}
	if err := sc.Err(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return f, nil
}

func (f *File) add(n int, fields []string) error {
	reason := func(from int) (string, error) {
		if len(fields) <= from {
			return "", errors.New("missing reason: every record says why it exists")
		}
		return strings.Join(fields[from:], " "), nil
	}
	switch fields[0] {
	case "size":
		if len(fields) < 4 {
			return errors.New("want: size <dir> <source>|- <test>|- <reason>")
		}
		dir := fields[1]
		if err := canonical(dir); err != nil {
			return err
		}
		src, err := column(fields[2], DefaultSource, "source")
		if err != nil {
			return err
		}
		tst, err := column(fields[3], DefaultTest, "test")
		if err != nil {
			return err
		}
		if src == 0 && tst == 0 {
			return errors.New("both columns are -: a record that raises nothing should not exist")
		}
		why, err := reason(4)
		if err != nil {
			return err
		}
		if _, dup := f.Sizes[dir]; dup {
			return fmt.Errorf("second size record for %s", dir)
		}
		f.Sizes[dir] = Size{Dir: dir, Source: src, Test: tst, Line: n, Reason: why}
	case "time":
		if len(fields) < 3 {
			return errors.New("want: time <dir> <duration> <reason>")
		}
		dir := fields[1]
		if err := canonical(dir); err != nil {
			return err
		}
		d, err := duration(fields[2])
		if err != nil {
			return err
		}
		why, err := reason(3)
		if err != nil {
			return err
		}
		if _, dup := f.Times[dir]; dup {
			return fmt.Errorf("second time record for %s", dir)
		}
		f.Times[dir] = Time{Dir: dir, Limit: d, Line: n, Reason: why}
	case "perf":
		if len(fields) < 4 || !strings.HasPrefix(fields[2], "/") {
			return errors.New("want: perf <METHOD> <pattern> <duration>|skip <reason>")
		}
		method := fields[1]
		if method != strings.ToUpper(method) {
			return fmt.Errorf("method %q must be upper case, as perf names it", method)
		}
		p := Perf{Screen: method + " " + fields[2], Line: n}
		if fields[3] == "skip" {
			p.Skip = true
		} else {
			d, err := duration(fields[3])
			if err != nil {
				return err
			}
			p.Limit = d
		}
		why, err := reason(4)
		if err != nil {
			return err
		}
		p.Reason = why
		if _, dup := f.Perfs[p.Screen]; dup {
			return fmt.Errorf("second perf record for %s", p.Screen)
		}
		f.Perfs[p.Screen] = p
	case "boot":
		if len(fields) < 2 {
			return errors.New("want: boot <duration> <reason>")
		}
		d, err := duration(fields[1])
		if err != nil {
			return err
		}
		why, err := reason(2)
		if err != nil {
			return err
		}
		if f.Boot != nil {
			return errors.New("second boot record")
		}
		f.Boot = &Boot{Limit: d, Line: n, Reason: why}
	default:
		return fmt.Errorf("unknown record %q (size, time, perf, boot)", fields[0])
	}
	return nil
}

// canonical refuses every spelling but one, so a directory can never
// have two records that both look right.
func canonical(dir string) error {
	switch {
	case dir == "..", strings.HasPrefix(dir, "../"), strings.HasPrefix(dir, "/"),
		strings.Contains(dir, `\`), path.Clean(dir) != dir:
		return fmt.Errorf("directory %q is not canonical: write it relative to the module root, slash-separated, as path.Clean gives it (%q)", dir, path.Clean(dir))
	}
	return nil
}

func column(s string, def int, name string) (int, error) {
	if s == "-" {
		return 0, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("%s column %q: want a positive line count or -", name, s)
	}
	if v <= def {
		return 0, fmt.Errorf("%s column %d is not above the default %d: write -", name, v, def)
	}
	return v, nil
}

func duration(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("duration %q: want a positive Go duration such as 25s or 1200ms", s)
	}
	return d, nil
}

// Load reads root's file. A missing file is no exemptions, not an
// error: a new app has none and should not need an empty file to say so.
func Load(root string) (*File, error) {
	fh, err := os.Open(filepath.Join(root, FileName))
	if errors.Is(err, os.ErrNotExist) {
		return empty(), nil
	}
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	return Parse(fh)
}

// ModuleRoot is the nearest directory at or above dir holding a go.mod:
// budgets.txt sits beside it, one file per module.
func ModuleRoot(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no go.mod in this directory or any parent")
		}
		dir = parent
	}
}

// ModulePath reads the module line, which is what turns an import path
// into the directory key budgets.txt uses.
func ModulePath(root string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Fields(l); len(f) >= 2 && f[0] == "module" {
			return strings.Trim(f[1], `"`), nil
		}
	}
	return "", fmt.Errorf("%s/go.mod has no module line", root)
}

// Enforce says whether timing budgets fail or only report. Timing is
// evidence on the CI runner and noise on a loaded laptop, so the switch
// is the variable every CI runner sets (amadan's AMADAN_CI, everyone
// else's CI), not a flag someone has to remember.
func Enforce(getenv func(string) string) bool {
	return getenv("AMADAN_CI") != "" || getenv("CI") != ""
}
