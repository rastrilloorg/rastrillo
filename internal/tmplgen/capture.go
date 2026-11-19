package tmplgen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"amadan.net/rastrillo/rastrillo/internal/tmplescape"
	"amadan.net/rastrillo/rastrillo/tmpl"
	"amadan.net/rastrillo/rastrillo/tmpl/tmplrt"
)

// capture builds the main package pkg and runs it in capture mode. why
// says why it produced no capture; err is for the scratch directory
// failing under it, and for a main whose templates another generator
// version must compile.
func capture(
	moduleDir, scratch string,
	index int,
	pkg string,
) (captured tmpl.Capture, why string, err error) {
	bin, why := buildMain(moduleDir, scratch, index, pkg)
	if why != "" {
		return captured, why, nil
	}

	// Its own working directory, so a relative database path gets a
	// throwaway database.
	work := filepath.Join(scratch, fmt.Sprintf("run%d", index))
	if err := os.Mkdir(work, 0o755); err != nil {
		return captured, "", err
	}
	file := filepath.Join(scratch, fmt.Sprintf("capture%d.json", index))
	out, why := runCapture(bin, work, file)
	if why != "" {
		return captured, why, nil
	}
	captured, why, err = readCapture(file, out)
	if why != "" || err != nil {
		return captured, why, err
	}
	why, err = refuseCapture(captured, pkg)
	return captured, why, err
}

// buildMain builds the main package pkg with the capture tag into
// scratch, and returns the binary, or why it did not build.
func buildMain(
	moduleDir, scratch string,
	index int,
	pkg string,
) (bin, why string) {
	bin = filepath.Join(scratch, fmt.Sprintf("main%d", index))
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	args := append([]string{"build"}, buildFlags...)
	build := exec.Command("go", append(args, "-o", bin, pkg)...)
	build.Dir = moduleDir
	if out, err := build.CombinedOutput(); err != nil {
		return bin, fmt.Sprintf(
			"does not build with -tags rastrillo_capture: %s",
			tail(out, err),
		)
	}
	return bin, ""
}

// Why a main that ran produced no capture to compile.
const (
	noCapture = "exited without writing its template sets; " +
		"does it reach rastrillo.Serve? "
	unparsedCapture = "wrote a template capture that does not parse: "
	otherCapture    = "wrote template capture version %d; " +
		"this rastrillo generate reads %d"
	otherEscaping = "escapes templates as %s does; " +
		"this rastrillo generate escapes them as %s does"
)

// readCapture reads the capture file a run wrote. out is the run's
// output, which says why when it wrote none.
func readCapture(
	file string,
	out []byte,
) (captured tmpl.Capture, why string, err error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return captured, noCapture + tail(out, nil), nil
	}
	if err != nil {
		return captured, "", err
	}
	if err := json.Unmarshal(data, &captured); err != nil {
		return captured, unparsedCapture + err.Error(), nil
	}
	return captured, "", nil
}

// refuseCapture says why this generator cannot compile what the main
// package pkg captured, or returns an error when it must not.
func refuseCapture(captured tmpl.Capture, pkg string) (string, error) {
	switch {
	case captured.Version != captureVersion:
		return fmt.Sprintf(
			otherCapture,
			captured.Version,
			captureVersion,
		), nil
	case captured.GeneratorVersion != tmplrt.GeneratorVersion:
		// The app's fingerprints take in its generator version, so code
		// this generator wrote would be registered under them and run:
		// an older generator's code after the bump that was to retire
		// it. That is no speed-up lost but wrong code shipped, so it
		// stops generate instead of leaving the main uncompiled.
		return "", fmt.Errorf(
			otherGenerator,
			pkg,
			captured.GeneratorVersion,
			tmplrt.GeneratorVersion,
		)
	case captured.GoVersion != tmplescape.GoVersion:
		return fmt.Sprintf(
			otherEscaping,
			captured.GoVersion,
			tmplescape.GoVersion,
		), nil
	}
	return "", nil
}

// otherGenerator is Run's error for a main whose rastrillo compiles
// templates with another generator version than this one.
const otherGenerator = "tmplgen: %s needs rastrillo generate from the " +
	"rastrillo version in its go.mod: its templates are for generator " +
	"%d, and this one is generator %d"

// runCapture runs bin in work with capture written to file, and returns
// its output, and why the run failed or "". It kills the binary's whole
// process group once it ends or times out: an app that is serving
// instead of capturing may have started children, and nothing generate
// starts may outlive it.
func runCapture(bin, work, file string) ([]byte, string) {
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	cmd := exec.Command(bin)
	cmd.Dir = work
	cmd.Env = captureEnv(file)
	ownGroup(cmd)
	// A grandchild that escaped the group could hold the pipe open;
	// WaitDelay stops Wait blocking on it.
	cmd.WaitDelay = 5 * time.Second
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		return nil, "could not start: " + err.Error()
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		killGroup(cmd.Process)
		if err != nil {
			return out.Bytes(),
				"failed in capture mode: " + tail(out.Bytes(), err)
		}
		return out.Bytes(), ""
	case <-ctx.Done():
		killGroup(cmd.Process)
		<-done
		return out.Bytes(), fmt.Sprintf(
			"did not finish within %s in capture mode and was stopped",
			runTimeout,
		)
	}
}

// captureKeep is what the capture child gets of this process's
// environment: what running a Go program needs. The app's startup runs
// in capture, and an app reads its database address, secrets and
// service URLs from the environment, so none of the rest reaches it. A
// systemd state directory would point its database at the real one;
// the configuration probe would exit before building any set.
var captureKeep = []string{"PATH", "HOME", "TMPDIR", "LANG"}

// captureEnv is the capture child's environment: captureKeep's
// variables that are set, and the capture file.
func captureEnv(file string) []string {
	var env []string
	for _, key := range captureKeep {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return append(env, tmpl.CaptureEnv+"="+file)
}

// tail is the end of a command's output, or err's text when there is
// none, on one line: a capture failure is one line of generate's
// report.
func tail(out []byte, err error) string {
	text := strings.Join(strings.Fields(string(out)), " ")
	if text == "" && err != nil {
		text = strings.Join(strings.Fields(err.Error()), " ")
	}
	const max = 2000
	if len(text) > max {
		start := len(text) - max
		for start < len(text) && !utf8.RuneStart(text[start]) {
			start++
		}
		text = "..." + text[start:]
	}
	return text
}
