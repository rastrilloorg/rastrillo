package scaffoldcheck

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/ui"
)

// variant is one app to scaffold: its name and rastrillo new's flags.
type variant struct {
	name  string
	flags []string
}

// The two apps every check runs on: the default scaffold and the one
// that signs people in.
var variants = []variant{
	{"plainapp", nil},
	{"magicapp", []string{"--auth=magic"}},
}

// everyShell is each of the variants on every shell rastrillo new
// offers, named after the shell, so a shell added to ui is checked too.
func everyShell() []variant {
	var all []variant
	for _, shell := range ui.LayoutNames() {
		for _, each := range variants {
			flags := append([]string{"--shell=" + shell}, each.flags...)
			all = append(all, variant{shell + each.name, flags})
		}
	}
	return all
}

// shellOf is the shell a variant was scaffolded with.
func (each variant) shellOf() string {
	for _, flag := range each.flags {
		if shell, ok := strings.CutPrefix(flag, "--shell="); ok {
			return shell
		}
	}
	return "column"
}

// rastrilloBinary is the command built from this checkout, once for the
// whole package, so every test drives what a person would install.
var rastrilloBinary string

func TestMain(suite *testing.M) {
	scratch, err := os.MkdirTemp("", "scaffoldcheck")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := buildAndRun(suite, scratch)
	os.RemoveAll(scratch)
	os.Exit(code)
}

func buildAndRun(suite *testing.M, scratch string) int {
	rastrilloBinary = filepath.Join(scratch, "rastrillo")
	build := exec.Command("go", "build", "-buildvcs=false",
		"-o", rastrilloBinary, "./cmd/rastrillo")
	build.Dir = repositoryRoot()
	build.Env = goEnvironment()
	if output, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building rastrillo: %v\n%s", err, output)
		return 1
	}
	return suite.Run()
}

// repositoryRoot is this checkout, two directories up from this file.
func repositoryRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// goEnvironment resolves modules from the local module cache only, so a
// scaffold tidies offline. No VCS stamp: a temporary directory may sit
// inside someone else's repository.
func goEnvironment() []string {
	cache, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		panic(err)
	}
	proxy := "file://" + strings.TrimSpace(string(cache)) + "/cache/download"
	return append(os.Environ(), "GOFLAGS=-mod=mod -buildvcs=false",
		"GOSUMDB=off", "GOPRIVATE=*", "GOPROXY="+proxy, "GOWORK=off")
}

// newApp runs rastrillo new in a fresh directory and returns the app's
// directory. The app is not tidied, so it cannot build yet.
func newApp(t *testing.T, name string, flags ...string) string {
	t.Helper()
	parent := t.TempDir()
	arguments := append([]string{"new"}, flags...)
	run(t, parent, rastrilloBinary, append(arguments, name)...)
	return filepath.Join(parent, name)
}

// buildableApp is newApp pointed at this checkout and tidied.
func buildableApp(t *testing.T, name string, flags ...string) string {
	t.Helper()
	directory := newApp(t, name, flags...)
	replace := "\nreplace amadan.net/rastrillo/rastrillo => " +
		repositoryRoot() + "\n"
	appendTo(t, filepath.Join(directory, "go.mod"), replace)
	run(t, directory, "go", "mod", "tidy")
	return directory
}

// run runs a program in directory and returns what it printed, failing
// the test if it fails.
func run(t *testing.T, directory, program string, arguments ...string) string {
	t.Helper()
	output, err := runIn(directory, program, arguments...)
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", program, strings.Join(arguments, " "),
			err, output)
	}
	return output
}

func runIn(directory, program string, arguments ...string) (string, error) {
	command := exec.Command(program, arguments...)
	command.Dir = directory
	command.Env = goEnvironment()
	output, err := command.CombinedOutput()
	return string(output), err
}

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
