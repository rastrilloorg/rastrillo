package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBudgetSizeExitCodes(t *testing.T) {
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/a\n"), 0o644))
	must(os.MkdirAll(filepath.Join(root, "internal", "a"), 0o755))
	must(os.WriteFile(filepath.Join(root, "internal", "a", "a.go"), []byte(strings.Repeat("//\n", 10)), 0o644))

	var out bytes.Buffer
	if err := budgetSize([]string{root}, &out); err != nil {
		t.Fatalf("clean module: %v\n%s", err, out.String())
	}

	must(os.WriteFile(filepath.Join(root, "internal", "a", "b.go"), []byte(strings.Repeat("//\n", 5000)), 0o644))
	out.Reset()
	var ex exitError
	if err := budgetSize([]string{root}, &out); !errors.As(err, &ex) || ex.code != 1 {
		t.Fatalf("over budget: err = %v, want exit 1\n%s", err, out.String())
	}

	must(os.MkdirAll(filepath.Join(root, ".rastrillo"), 0o755))
	must(os.WriteFile(filepath.Join(root, ".rastrillo", "budgets.txt"), []byte("size ./internal/a 6000 - x\n"), 0o644))
	out.Reset()
	if err := budgetSize([]string{root}, &out); !errors.As(err, &ex) || ex.code != 2 {
		t.Fatalf("malformed file: err = %v, want exit 2", err)
	}
}
