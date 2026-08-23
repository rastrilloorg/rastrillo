package main

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo/vectors"
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

	belt := readInit(t, dir, "internal", "demotest", "parity_test.go")
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

	pin := readInit(t, dir, "internal", "demotest", "vectors_vendored_test.go")
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

// The internal/<pkg>test destination follows the scaffold's own
// derivation: module path base, cleaned to an identifier.
func TestVectorsInitDerivesPkgFromModulePath(t *testing.T) {
	dir := scaffold(t, map[string]string{"go.mod": "module github.com/acme/fancy-app\n\ngo 1.24\n"})
	if err := runVectors([]string{"-init", dir}); err != nil {
		t.Fatalf("vectors -init: %v", err)
	}
	belt := readInit(t, dir, "internal", "fancyapptest", "parity_test.go")
	if !strings.Contains(belt, "package fancyapptest") {
		t.Errorf("belt should live in package fancyapptest:\n%s", belt)
	}
}
