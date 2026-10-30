package codelimits

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// wideLine is 83 columns, so a file holding it has one columns offence.
var wideLine = "// " + strings.Repeat("x", 80)

func sourceWith(lines ...string) string {
	return "package m\n\n" + strings.Join(lines, "\n") + "\n"
}

// scratchModule is a module in a repository of its own with no
// origin/main, so Run skips the base comparison for a known reason
// whatever repository the temporary directory happens to sit in.
func scratchModule(t *testing.T, source string) string {
	t.Helper()
	module := scratchRepository(t)
	writeFile(t, filepath.Join(module, "go.mod"), "module example.com/m\n")
	writeFile(t, filepath.Join(module, "a.go"), source)
	return module
}

func run(t *testing.T, mode Mode, root string) Outcome {
	t.Helper()
	outcome, err := Run(mode, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return outcome
}

func readRecords(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, FileName))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRunInitWritesThenReportsItExists(t *testing.T) {
	module := scratchModule(t, sourceWith(wideLine))
	written := run(t, InitMode, module)
	if written.Records != 1 || written.Exists || len(written.Messages) > 0 {
		t.Fatalf("first init: %+v, want one record written", written)
	}
	before := readRecords(t, module)
	if !strings.Contains(before, "columns a.go "+hashOf(wideLine)+" 1") {
		t.Fatalf("init wrote no record for the wide line:\n%s", before)
	}

	again := run(t, InitMode, module)
	if !again.Exists {
		t.Fatalf("second init: %+v, want Exists", again)
	}
	if after := readRecords(t, module); after != before {
		t.Fatalf("second init rewrote the file:\n%s", after)
	}

	checked := run(t, CheckMode, module)
	if len(checked.Messages) > 0 || checked.Records != 1 {
		t.Fatalf("check after init: %+v, want a pass on one record", checked)
	}
	if checked.Note != NoOrigin {
		t.Fatalf("note %q, want %q", checked.Note, NoOrigin)
	}
}

func TestRunFailsANewOffence(t *testing.T) {
	module := scratchModule(t, sourceWith("// short"))
	if clean := run(t, CheckMode, module); len(clean.Messages) > 0 {
		t.Fatalf("a clean module failed: %q", clean.Messages)
	}
	writeFile(t, filepath.Join(module, "a.go"), sourceWith(wideLine))
	failed := run(t, CheckMode, module)
	wantOne(t, failed.Messages, "a.go line 3", "83 columns")
}

// The base comparison is part of the check: a record raised since the
// branch left main, with no reason, fails although the code is exactly
// as the record says.
func TestRunFailsARaiseSinceTheBase(t *testing.T) {
	module := scratchModule(t, sourceWith(wideLine))
	run(t, InitMode, module)
	commitAll(t, module)
	git(t, module, "update-ref", "refs/remotes/origin/main", "HEAD")
	writeFile(t, filepath.Join(module, "a.go"),
		sourceWith(wideLine, wideLine))
	writeRecords(t, module, strings.Replace(readRecords(t, module),
		hashOf(wideLine)+" 1", hashOf(wideLine)+" 2", 1))

	outcome := run(t, CheckMode, module)
	wantOne(t, outcome.Messages, "raised from 1 to 2", "reason")
	if outcome.Note != "" {
		t.Fatalf("note %q with a base to compare with", outcome.Note)
	}
}

func TestRunLowerThenPasses(t *testing.T) {
	module := scratchModule(t, sourceWith(wideLine, wideLine))
	run(t, InitMode, module)
	writeFile(t, filepath.Join(module, "a.go"), sourceWith(wideLine))
	stale := run(t, CheckMode, module)
	wantOne(t, stale.Messages, "Lower the record to 1")

	lowered := run(t, LowerMode, module)
	if len(lowered.Messages) > 0 || lowered.Records != 1 {
		t.Fatalf("lower: %+v, want a pass on one record", lowered)
	}
	records := readRecords(t, module)
	if !strings.Contains(records, hashOf(wideLine)+" 1\n") {
		t.Fatalf("the record was not lowered to 1:\n%s", records)
	}
	if again := run(t, CheckMode, module); len(again.Messages) > 0 {
		t.Fatalf("check after lower: %q", again.Messages)
	}
}

// Lowering a module with no file has nothing to lower, and must not
// leave an empty file behind for someone to commit.
func TestRunLowerWritesNoFileWhereThereWasNone(t *testing.T) {
	module := scratchModule(t, sourceWith("// short"))
	if outcome := run(t, LowerMode, module); len(outcome.Messages) > 0 {
		t.Fatalf("lower on a clean module: %q", outcome.Messages)
	}
	_, err := os.Stat(filepath.Join(module, FileName))
	if !os.IsNotExist(err) {
		t.Fatalf("lower created %s: %v", FileName, err)
	}
}

func TestRunReportsAMalformedFile(t *testing.T) {
	module := scratchModule(t, sourceWith("// short"))
	writeRecords(t, module, "columns a.go\n")
	for _, mode := range []Mode{CheckMode, LowerMode} {
		outcome := run(t, mode, module)
		if !outcome.Malformed {
			t.Fatalf("mode %d: %+v, want Malformed", mode, outcome)
		}
		wantOne(t, outcome.Messages, FileName+" line 1")
	}
	if records := readRecords(t, module); records != "columns a.go\n" {
		t.Fatalf("a malformed file was rewritten:\n%s", records)
	}
}

// Format separates fields with spaces, so a path holding one would be
// written and then misread as a shifted key and count. Init must refuse
// it before writing anything, rather than report success on a file the
// next check rejects.
func TestRunInitRefusesAPathWithSpaces(t *testing.T) {
	module := scratchModule(t, sourceWith("// short"))
	writeFile(t, filepath.Join(module, "old handlers.go"),
		sourceWith(wideLine))
	_, err := Run(InitMode, module)
	if err == nil || !strings.Contains(err.Error(), `"old handlers.go"`) ||
		!strings.Contains(err.Error(), "spaces") {
		t.Fatalf("init on a path with spaces: %v", err)
	}
	directory := filepath.Dir(filepath.Join(module, FileName))
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("init wrote %s although it refused: %v", directory, err)
	}
}
