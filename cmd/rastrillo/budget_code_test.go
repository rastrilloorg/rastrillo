package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildRastrillo builds this checkout's command, because the exit codes
// are the contract a Makefile reads, and only the real process has them.
func buildRastrillo(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "rastrillo")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = filepath.Join(repoRoot(t), "cmd", "rastrillo")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, output)
	}
	return binary
}

// codeModule is a module in a git repository of its own, so the base
// comparison is skipped for having no origin/main rather than reading
// whatever repository the temporary directory sits in.
func codeModule(t *testing.T) string {
	t.Helper()
	module := t.TempDir()
	if output, err := exec.Command("git", "init", "-q", module).
		CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	writeTestFile(t, filepath.Join(module, "go.mod"), "module example.com/m\n")
	writeTestFile(t, filepath.Join(module, "a.go"), "package m\n")
	return module
}

func writeTestFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// exitStatus runs the command and returns its exit status and output.
func exitStatus(t *testing.T, binary string, arguments ...string) (
	int, string) {
	t.Helper()
	command := exec.Command(binary, arguments...)
	output, err := command.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, string(output)
	case errors.As(err, &exit):
		return exit.ExitCode(), string(output)
	}
	t.Fatalf("%s: %v", binary, err)
	return -1, ""
}

func TestBudgetCodeExitCodes(t *testing.T) {
	binary := buildRastrillo(t)
	module := codeModule(t)
	wide := "package m\n\n// " + strings.Repeat("x", 80) + "\n"
	records := filepath.Join(module, ".rastrillo", "code.txt")
	steps := []struct {
		name      string
		prepare   func()
		arguments []string
		want      int
	}{
		{"clean", func() {}, nil, 0},
		{"an offence", func() {
			writeTestFile(t, filepath.Join(module, "a.go"), wide)
		}, nil, 1},
		{"init", func() {}, []string{"--init"}, 0},
		{"recorded", func() {}, nil, 0},
		{"init over an existing file", func() {}, []string{"--init"}, 2},
		{"lower", func() {}, []string{"--lower"}, 0},
		{"malformed", func() {
			writeTestFile(t, records, "columns a.go\n")
		}, nil, 2},
		{"init and lower at once", func() {},
			[]string{"--init", "--lower"}, 2},
	}
	for _, step := range steps {
		step.prepare()
		arguments := append([]string{"budget", "code"}, step.arguments...)
		arguments = append(arguments, module)
		status, output := exitStatus(t, binary, arguments...)
		if status != step.want {
			t.Fatalf("%s: exit %d, want %d\n%s",
				step.name, status, step.want, output)
		}
	}
}
