package tour

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// The chat tour, replayed on this checkout as a reader follows it: after
// every step that changes the app it builds, vets and passes its tests,
// and every path, command and name its lesson mentions is there; the
// finished app passes its own make ci, as step 13 promises.
func TestEveryStepOfTheChatTourBuildsAndPasses(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds and builds an app for every step")
	}
	tour, err := Load("chat")
	if err != nil {
		t.Fatal(err)
	}
	toolchain := thisCheckout(t)
	app := ""
	played := func(step Step, directory string, changes []Change) error {
		app = directory
		if directory == "" {
			return nil
		}
		if err := lessonMatches(step, directory, toolchain); err != nil {
			return err
		}
		if len(changes) == 0 && !step.Scaffold {
			return nil
		}
		for _, arguments := range [][]string{
			{"build", "./..."}, {"vet", "./..."}, {"test", "./..."},
		} {
			if err := toolchain.run(directory, "go", arguments...); err != nil {
				return err
			}
		}
		return nil
	}
	if err := tour.Replay(t.TempDir(), toolchain, played); err != nil {
		t.Fatal(err)
	}
	wholeGate(t, app)
}

// wholeGate runs every lane on the app's own ci: line but govulncheck,
// which reads the live vulnerability database, offline from this
// machine's module cache, as the scaffold's own gate test does.
func wholeGate(t *testing.T, app string) {
	t.Helper()
	makefile, err := os.ReadFile(filepath.Join(app, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	var lanes []string
	for _, line := range strings.Split(string(makefile), "\n") {
		if lint, found := strings.CutPrefix(line, "GOLANGCI_LINT := "); found {
			// As scaffoldGate does: the one tool rastrillo's gate never runs,
			// so nothing else caches it. -n fetches it and builds nothing; the
			// gate below then runs offline.
			install := exec.Command("go", "install", "-n", lint)
			if output, err := install.CombinedOutput(); err != nil {
				t.Fatalf("go install -n %s: %v\n%s", lint, err, output)
			}
		}
		if targets, found := strings.CutPrefix(line, "ci: "); found {
			for _, lane := range strings.Fields(targets) {
				if lane != "govulncheck" {
					lanes = append(lanes, lane)
				}
			}
		}
	}
	if len(lanes) < 5 {
		t.Fatalf("read only %v from the app's ci: line", lanes)
	}
	gate := exec.Command("make", lanes...)
	gate.Dir = app
	gate.Env = append(os.Environ(), offline(t)...)
	gate.Env = append(gate.Env, "GOFLAGS=-mod=readonly -buildvcs=false",
		"GOPRIVATE=", "AMADAN_CI=", "CI=")
	output, err := gate.CombinedOutput()
	if err != nil {
		t.Fatalf("make ci on the finished app: %v\n%s", err, output)
	}
	for _, ran := range []string{"budget code: within the limits.",
		"csp: nothing the CSP drops", "0 issues.\n", "perf: GET /: median"} {
		if !strings.Contains(string(output), ran) {
			t.Errorf("make ci ran no %q:\n%s", ran, output)
		}
	}
}

// thisCheckout runs the rastrillo this checkout builds, and points each
// new app at it: rastrillo new pins a released version, and this tour
// must pass on the code beside it.
func thisCheckout(t *testing.T) Toolchain {
	t.Helper()
	root := repositoryRoot()
	binary := filepath.Join(t.TempDir(), "rastrillo")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", binary,
		"./cmd/rastrillo")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build rastrillo: %v\n%s", err, output)
	}
	toolchain := Toolchain{Rastrillo: []string{binary}, Env: offline(t),
		NewEnv: []string{"GOPROXY=off", "GOMODCACHE=" + t.TempDir(),
			"GOFLAGS=-mod=mod -buildvcs=false -modcacherw"}}
	toolchain.Made = func(directory string) error {
		file, err := os.OpenFile(filepath.Join(directory, "go.mod"),
			os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, err = file.WriteString("\nreplace " +
			"amadan.net/rastrillo/rastrillo => " + root + "\n")
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		if err := toolchain.run(directory, "go", "mod", "tidy"); err != nil {
			return err
		}
		return toolchain.rastrillo(directory, "generate")
	}
	return toolchain
}

// output is what rastrillo prints for arguments, failing or not: its
// usage and -help are read for the commands and flags it has.
func (toolchain Toolchain) output(arguments ...string) string {
	argv := slices.Clone(toolchain.Rastrillo)
	for _, argument := range arguments {
		if argument != "" {
			argv = append(argv, argument)
		}
	}
	command := exec.Command(argv[0], argv[1:]...)
	command.Env = append(os.Environ(), toolchain.Env...)
	output, _ := command.CombinedOutput()
	return string(output)
}

// offline resolves modules from this machine's module cache only, as
// internal/scaffoldcheck does, so a replay never waits on the network.
func offline(t *testing.T) []string {
	t.Helper()
	cache, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatal(err)
	}
	proxy := "file://" + strings.TrimSpace(string(cache)) + "/cache/download"
	return []string{"GOFLAGS=-mod=mod -buildvcs=false", "GOSUMDB=off",
		"GOPRIVATE=*", "GOPROXY=" + proxy, "GOWORK=off"}
}

func repositoryRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..")
}
