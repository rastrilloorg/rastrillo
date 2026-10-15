// Package localecheck is what `rastrillo doctor` checks about an app's
// locales: the static checks, which claim only what files show, and,
// with --probe, the configuration probe, run in the app's own binary.
package localecheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"golang.org/x/mod/modfile"

	"amadan.net/rastrillo/rastrillo/internal/checkconfig"
	"amadan.net/rastrillo/rastrillo/internal/generate/catalogs"
	"amadan.net/rastrillo/rastrillo/internal/generate/packs"
	"amadan.net/rastrillo/rastrillo/internal/manifest"
	"amadan.net/rastrillo/rastrillo/locales"
)

// Appendix C.2's op.doctor.* lines for the probe.
const (
	msgProbeFailed  = "the configuration probe failed (exit %d): %s"
	msgSkipped      = "skipped the configuration probe: this app's rastrillo is %s, and the probe needs %s or later; upgrade, then run doctor again"
	msgTimeout      = "the configuration probe did not finish within %s and was stopped"
	msgBuildFailed  = "could not build %s to run the configuration probe: %v"
	msgNoCommand    = "no cmd/%s/main.go to run the configuration probe against; nothing was checked"
	checkconfigPath = "amadan.net/rastrillo/rastrillo/internal/checkconfig"
)

// Drafts for the copy review of the opt-in probe (copy-review/final):
// the --probe flag's help, and the line doctor prints under Locales:
// when it did not run the app.
const (
	ProbeFlag = "also build the app and run it with RASTRILLO_CHECK_CONFIG=1 to check its locale setup; this runs the app's own code"
	NotProbed = "did not run the app; rastrillo doctor --probe builds and runs it to check its locale setup"
)

// probeTimeout bounds one run of the built binary. The probe exits at
// once; a binary still running after this is serving, not probing. A
// variable so a test can shorten it.
var probeTimeout = 2 * time.Minute

// Static returns the problems the files under appDir show on their
// own: gen/locales/packs.go not matching manifest/app.toml, and
// catalogs.Check's findings, so doctor and generate --check apply one
// rule. An app with no manifest/ is not a generated app, so it has no
// packs.go to be stale and nothing here applies to it.
func Static(appDir string) []string {
	manifestDir := filepath.Join(appDir, "manifest")
	if fi, err := os.Stat(manifestDir); err != nil || !fi.IsDir() {
		return nil
	}
	app, err := manifest.LoadApp(manifestDir)
	if err != nil {
		return []string{err.Error()}
	}
	var problems []string
	if stale, err := packs.Stale(filepath.Join(appDir, "gen"), app); err != nil {
		problems = append(problems, err.Error())
	} else if stale != "" {
		problems = append(problems, stale)
	}
	found, err := catalogs.Check(appDir, app)
	if err != nil {
		return append(problems, err.Error())
	}
	return append(problems, found...)
}

// Probe runs the configuration probe in the app's own binary, built
// from cmd/<module name> and run with only RASTRILLO_CHECK_CONFIG=1
// added to the environment. doctor calls it only for --probe: the
// probe itself opens nothing, but the app's code runs first, and an
// init() or a main that writes files or opens a database before the
// framework's entry points does so for real.
//
// A binary must at least link the probe. One built against
// a rastrillo from before it ignores the variable and starts for real
// (database, migrations, listener), so Probe runs nothing unless the
// command depends on internal/checkconfig, which every entry point that
// probes imports; appVersion and firstVersion name the two sides of
// that skip.
//
// report is what doctor prints under its heading: the probe's JSON
// line, or the line saying why nothing ran. A non-zero exit, "ok":false
// or a build failure is a problem. err is for a probe that could not be
// attempted at all.
func Probe(ctx context.Context, appDir, appVersion, firstVersion string) (report []byte, problems []string, err error) {
	data, err := os.ReadFile(filepath.Join(appDir, "go.mod"))
	if err != nil {
		return nil, nil, err
	}
	// Only the command named after the module is the app. Anything else
	// under cmd/ may be a tool (rastrillo vectors -init adds
	// cmd/genvectors, which writes files) that never reaches the probe.
	name := path.Base(modfile.ModulePath(data))
	pkg := "./cmd/" + name
	if _, err := os.Stat(filepath.Join(appDir, "cmd", name, "main.go")); err != nil {
		return []byte(fmt.Sprintf(msgNoCommand, name) + "\n"), nil, nil
	}
	deps, err := goCmd(ctx, appDir, "list", "-buildvcs=false", "-deps", pkg)
	if err != nil {
		return nil, []string{fmt.Sprintf(msgBuildFailed, pkg, err)}, nil
	}
	if !slices.Contains(strings.Fields(deps), checkconfigPath) {
		return []byte(fmt.Sprintf(msgSkipped, appVersion, firstVersion) + "\n"), nil, nil
	}
	tmp, err := os.MkdirTemp("", "rastrillo-probe-")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(tmp)
	// Build first, then run the binary itself: under go run, a timeout
	// kills go and orphans the app, and the exit code is go run's own 1
	// rather than the probe's.
	bin := filepath.Join(tmp, name)
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if _, err := goCmd(ctx, appDir, "build", "-buildvcs=false", "-o", bin, pkg); err != nil {
		return nil, []string{fmt.Sprintf(msgBuildFailed, pkg, err)}, nil
	}
	line, problem, err := runProbe(ctx, appDir, bin)
	if err != nil {
		return nil, nil, err
	}
	if line != "" {
		report = []byte(line + "\n")
	}
	if problem != "" {
		problems = []string{problem}
	}
	return report, problems, nil
}

// goCmd runs the go command in dir and returns its stdout; a failure's
// error carries go's own stderr, which names what broke.
func goCmd(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", errors.New(msg)
		}
		return "", err
	}
	return stdout.String(), nil
}

// runProbe runs the built binary under probeTimeout. On the timeout it
// kills the binary's whole process group: a binary that is serving
// instead of probing may have started children, and killing only the
// first process would leave them holding the database.
func runProbe(ctx context.Context, appDir, bin string) (line, problem string, err error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd := exec.Command(bin)
	cmd.Dir = appDir
	cmd.Env = append(os.Environ(), checkconfig.Env+"=1")
	ownGroup(cmd)
	// A grandchild that escaped the group could hold the pipes open;
	// WaitDelay stops Wait blocking on it.
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		return "", "", err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var runErr error
	select {
	case runErr = <-done:
	case <-ctx.Done():
		killGroup(cmd.Process)
		<-done
		return "", fmt.Sprintf(msgTimeout, probeTimeout), nil
	}
	code := 0
	if runErr != nil {
		exit, ok := runErr.(*exec.ExitError)
		if !ok {
			return "", "", runErr
		}
		code = exit.ExitCode()
	}
	line = lastLine(stdout.String())
	var rep locales.ProbeReport
	parsed := json.Unmarshal([]byte(line), &rep) == nil && rep.Version != 0
	if code == 0 && parsed && rep.OK {
		return line, "", nil
	}
	// The report's own problems say why when the probe ran; when it
	// did not print one (a bad variable value exits 2 with a line on
	// stderr), stderr is the only account there is.
	detail := strings.TrimSpace(stderr.String())
	if parsed && len(rep.Problems) > 0 {
		detail = strings.Join(rep.Problems, "; ")
	}
	if !parsed {
		line = ""
	}
	return line, fmt.Sprintf(msgProbeFailed, code, detail), nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
