package main

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/vectors"
)

// readInit reads one -init-scaffolded file or fails the test — the
// readScaffold shape, absolute because runVectors absolutised dir.
func readInit(t *testing.T, dir string, parts ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{dir}, parts...)...))
	if err != nil {
		t.Fatalf("read %v: %v", parts, err)
	}
	return string(b)
}

func TestVectorsInitScaffoldsTheParityKit(t *testing.T) {
	dir := scaffold(t, map[string]string{"go.mod": "module demo\n\ngo 1.24\n"})
	if err := runVectors([]string{"-init", dir}); err != nil {
		t.Fatalf("vectors -init: %v", err)
	}

	gen := readInit(t, dir, "cmd", "genvectors", "main.go")
	for _, want := range []string{
		"TREATY",
		"same commit",
		"RFC 3339",
		"eventlog.Derive",
		"vectors.New()",
		"TOP LEVEL",
	} {
		if !strings.Contains(gen, want) {
			t.Errorf("genvectors template does not carry %q", want)
		}
	}

	parity := readInit(t, dir, "test", "parity.test.mjs")
	for _, want := range []string{
		`from "./vectors.mjs"`,
		"new Date(v.now)",
		"canonical(",
		"vectors.length >= 2",
		"did generation fail?",
		"BELT",
		"same commit",
	} {
		if !strings.Contains(parity, want) {
			t.Errorf("parity template does not carry %q", want)
		}
	}

	belt := readInit(t, dir, "internal", "demo", "parity_test.go")
	for _, want := range []string{
		`exec.LookPath("node")`,
		"t.Skip",
		`"--test", "test/parity.test.mjs"`,
		`cmd.Dir = "../.."`,
	} {
		if !strings.Contains(belt, want) {
			t.Errorf("go belt template does not carry %q", want)
		}
	}

	pin := readInit(t, dir, "internal", "demo", "vectors_vendored_test.go")
	for _, want := range []string{
		"vectors.JS()",
		`filepath.Join("..", "..", "test", "vectors.mjs")`,
		"delete this pin",
	} {
		if !strings.Contains(pin, want) {
			t.Errorf("vendored-pin template does not carry %q", want)
		}
	}

	for name, src := range map[string]string{
		"main.go":                  gen,
		"parity_test.go":           belt,
		"vectors_vendored_test.go": pin,
	} {
		if _, err := parser.ParseFile(token.NewFileSet(), name, src, 0); err != nil {
			t.Errorf("scaffolded %s does not parse: %v", name, err)
		}
	}

	vendored := readInit(t, dir, "test", "vectors.mjs")
	if !bytes.Equal([]byte(vendored), vectors.JS()) {
		t.Error("test/vectors.mjs is not byte-identical to vectors.JS()")
	}
}

// Delivered once, app-owned after: a second -init must refuse, not
// clobber files the app may have grown into its own.
func TestVectorsInitRefusesToClobber(t *testing.T) {
	dir := scaffold(t, map[string]string{"go.mod": "module demo\n\ngo 1.24\n"})
	if err := runVectors([]string{"-init", dir}); err != nil {
		t.Fatalf("first -init: %v", err)
	}
	err := runVectors([]string{"-init", dir})
	if err == nil {
		t.Fatal("second -init must refuse to overwrite app-owned files")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should say what already exists: %v", err)
	}
}

// The destination follows the scaffold's own derivation: module path
// base, cleaned to an identifier.
func TestVectorsInitDerivesPkgFromModulePath(t *testing.T) {
	dir := scaffold(t, map[string]string{"go.mod": "module github.com/acme/fancy-app\n\ngo 1.24\n"})
	if err := runVectors([]string{"-init", dir}); err != nil {
		t.Fatalf("vectors -init: %v", err)
	}
	belt := readInit(t, dir, "internal", "fancyapp", "parity_test.go")
	if !strings.Contains(belt, "package fancyapp_test") {
		t.Errorf("belt should live in package fancyapp_test:\n%s", belt)
	}
}

// vectors -init writes the belt where the app keeps its tests: beside
// the code in internal/<pkg> for a scaffold made now, in
// internal/<pkg>test for one made before tests moved there. Writing the
// old directory into a new app would make a second test package, and
// writing internal/<pkg> into an old one would split its suite in two.
// Both layouts must compile, and both sit two levels under the module
// root, which the shim's working directory depends on.
func TestVectorsInitUsesTheAppsTestLayout(t *testing.T) {
	if testing.Short() {
		t.Skip("tidies and vets two modules")
	}
	setSandboxGoEnv(t)
	for _, c := range []struct {
		name, testDir, pkgClause string
		oldLayout                bool
	}{
		{"beside the code", "demo", "package demo_test", false},
		{"own test package", "demotest", "package demotest", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			files := map[string]string{
				"go.mod": fmt.Sprintf("module demo\n\ngo 1.25.0\n\n"+
					"require amadan.net/rastrillo/rastrillo v0.0.0\n\n"+
					"replace amadan.net/rastrillo/rastrillo => %s\n", repoRoot(t)),
				"internal/demo/demo.go": "package demo\n",
			}
			if c.oldLayout {
				files["internal/demotest/harness_test.go"] = "package demotest\n"
			}
			dir := scaffold(t, files)
			if err := runVectors([]string{"-init", dir}); err != nil {
				t.Fatalf("vectors -init: %v", err)
			}
			for _, name := range []string{"parity_test.go", "vectors_vendored_test.go"} {
				src := readInit(t, dir, "internal", c.testDir, name)
				if !strings.HasPrefix(src, c.pkgClause+"\n") {
					t.Errorf("%s in internal/%s should start %q:\n%s", name, c.testDir, c.pkgClause, src)
				}
			}
			if c.oldLayout {
				if _, err := os.Stat(filepath.Join(dir, "internal", "demo", "parity_test.go")); err == nil {
					t.Error("an app with internal/demotest got its belt in internal/demo as well")
				}
			} else if _, err := os.Stat(filepath.Join(dir, "internal", "demotest")); err == nil {
				t.Error("a new-layout app got an internal/demotest directory")
			}
			tidy := exec.Command("go", "mod", "tidy")
			tidy.Dir = dir
			if out, err := tidy.CombinedOutput(); err != nil {
				t.Fatalf("go mod tidy:\n%s", out)
			}
			vet := exec.Command("go", "vet", "./...")
			vet.Dir = dir
			if out, err := vet.CombinedOutput(); err != nil {
				t.Errorf("go vet on the scaffolded belt:\n%s", out)
			}
		})
	}
}
