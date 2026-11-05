package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// datesApp is doctorApp with the two date files as given; an empty
// body means the file is absent.
func datesApp(t *testing.T, calendar, datetime string) (*report, string) {
	t.Helper()
	dir := doctorApp(t, rastrilloVersion(), "day")
	static := filepath.Join(dir, "internal", "demoapp", "static")
	for name, body := range map[string]string{
		"calendar.js": calendar, "datetime.js": datetime,
	} {
		path := filepath.Join(static, name)
		if body == "" {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			continue
		}
		mustWrite(t, path, body)
	}
	report, err := diagnose(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	return report, static
}

// failing renames as os.Rename does, except the moves rule refuses.
func failing(refuse func(from, to string) bool) func(string, string) error {
	return func(from, to string) error {
		if refuse(from, to) {
			return errors.New("injected rename failure")
		}
		return os.Rename(from, to)
	}
}

func intoPlace(from, to, name string) bool {
	return strings.Contains(filepath.Base(from), ".new-") &&
		filepath.Base(to) == name
}

// When putting a member back fails too, doctor must not say the pair
// is as it was: it says restoring failed and where the original waits,
// and that backup is kept.
func TestDoctorFixSaysWhereTheOriginalsAreWhenRestoringFails(t *testing.T) {
	report, static := datesApp(t, "// old calendar\n", "// old datetime\n")
	report.rename = failing(func(from, to string) bool {
		restoring := strings.Contains(filepath.Base(from), ".old-")
		return intoPlace(from, to, "calendar.js") ||
			restoring && filepath.Base(to) == "datetime.js"
	})
	var output bytes.Buffer
	err := report.applyFix(&output, false)
	if err == nil {
		t.Fatal("--fix reported no failure")
	}
	message := err.Error()
	backup := filepath.Join("internal", "demoapp", "static",
		".datetime.js.old-")
	want := "write datetime.js and calendar.js: injected rename failure; " +
		"restoring failed (injected rename failure): the originals are " +
		"at " + backup
	if !strings.HasPrefix(message, want) ||
		strings.Contains(message, "as they were") {
		t.Fatalf("the error does not say where the original is:\n%s",
			message)
	}
	kept := filepath.Join(report.dir,
		strings.TrimPrefix(message, want[:len(want)-len(backup)]))
	holds(t, filepath.Dir(kept), filepath.Base(kept),
		[]byte("// old datetime\n"))
	holds(t, static, "calendar.js", []byte("// old calendar\n"))
}

// A file the app keeps at a backup-looking name is never overwritten,
// deleted or restored over a member, whether the pair is written or
// rolled back.
func TestDoctorFixLeavesAnUnrelatedOldFileAlone(t *testing.T) {
	for _, fails := range []bool{false, true} {
		report, static := datesApp(t, "// old calendar\n",
			"// old datetime\n")
		unrelated := filepath.Join(static, ".calendar.js.old")
		mustWrite(t, unrelated, "unrelated\n")
		if fails {
			report.rename = failing(func(from, to string) bool {
				return intoPlace(from, to, "calendar.js")
			})
		}
		var output bytes.Buffer
		err := report.applyFix(&output, false)
		if (err != nil) != fails {
			t.Fatalf("failing %v: applyFix: %v", fails, err)
		}
		holds(t, static, ".calendar.js.old", []byte("unrelated\n"))
		entries, _ := os.ReadDir(static)
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".") && name != ".calendar.js.old" {
				t.Errorf("failing %v: --fix left %s behind", fails, name)
			}
		}
	}
}

// A member that did not exist before is removed when the pair rolls
// back, not replaced by an older file that happens to sit at a
// backup-looking name.
func TestDoctorFixRemovesANewMemberOnRollback(t *testing.T) {
	report, static := datesApp(t, "// old calendar\n", "")
	mustWrite(t, filepath.Join(static, ".datetime.js.old"), "unrelated\n")
	report.rename = failing(func(from, to string) bool {
		return intoPlace(from, to, "calendar.js")
	})
	var output bytes.Buffer
	err := report.applyFix(&output, false)
	if err == nil || !strings.HasSuffix(err.Error(), "both are as they were") {
		t.Fatalf("the rollback is not reported as complete: %v", err)
	}
	holds(t, static, "datetime.js", nil)
	holds(t, static, ".datetime.js.old", []byte("unrelated\n"))
	holds(t, static, "calendar.js", []byte("// old calendar\n"))
}
