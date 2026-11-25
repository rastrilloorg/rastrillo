package authored

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/transcript"
	"amadan.net/rastrillo/rastrillo/internal/codelimits"
)

func runOf(commands ...string) transcript.Transcript {
	var run transcript.Transcript
	for _, command := range commands {
		run.Messages = append(run.Messages, transcript.Message{
			Tools: []transcript.Tool{{
				Name: "Bash", Input: map[string]any{"command": command},
			}},
		})
	}
	return run
}

func scaffoldOf(t *testing.T, module string, commands ...string) Scaffold {
	t.Helper()
	got, err := ScaffoldOf(runOf(commands...), module)
	if err != nil {
		t.Fatalf("ScaffoldOf: %v", err)
	}
	return got
}

func sameScaffold(got, want Scaffold) bool {
	return got.Name == want.Name && slices.Equal(got.Flags, want.Flags)
}

func TestScaffoldCallsAreFoundInsidePipelinesAndLists(t *testing.T) {
	got := scaffoldOf(t, "groomer", "cd $TMPDIR && rm -rf scaf && mkdir scaf"+
		" && cd scaf && rastrillo new --auth=password --shell=topbar groomer"+
		" 2>&1 | tail -20 && find groomer -type f | grep -v '^groomer/static/'"+
		" | sort && ls groomer/static")
	want := Scaffold{"groomer", []string{"--auth=password", "--shell=topbar"}}
	if !sameScaffold(got, want) {
		t.Errorf("scaffold = %+v, want %+v", got, want)
	}
}

func TestRastrilloNewInsideAHeredocOrCommitMessageIsIgnored(t *testing.T) {
	_, err := ScaffoldOf(runOf(
		`git add -A && git commit -qm "Scaffold groomer with rastrillo new --auth=password groomer"`,
		"cat > notes.md <<'EOF'\nrastrillo new --shell=topbar groomer\nEOF",
		`echo "rastrillo new groomer"`,
	), "groomer")
	if !errors.Is(err, ErrUnscorable) {
		t.Errorf("err = %v, want ErrUnscorable", err)
	}
}

func TestTheLastMatchingScaffoldCallWins(t *testing.T) {
	file, err := os.Open("testdata/build.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	run, err := transcript.Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ScaffoldOf(run, "groomer")
	if err != nil {
		t.Fatal(err)
	}
	want := Scaffold{"groomer", []string{"--auth=password", "--shell", "topbar"}}
	if !sameScaffold(got, want) {
		t.Errorf("scaffold = %+v, want %+v", got, want)
	}
}

func TestTheModulePathNamesTheScaffoldByItsLastElement(t *testing.T) {
	got := scaffoldOf(t, "example.com/x/groomer", "rastrillo new groomer")
	if got.Name != "groomer" || len(got.Flags) != 0 {
		t.Errorf("scaffold = %+v, want groomer with no flags", got)
	}
}

func TestNoMatchingScaffoldCallIsUnscorable(t *testing.T) {
	_, err := ScaffoldOf(runOf("rastrillo new groomer"), "salon")
	if !errors.Is(err, ErrUnscorable) {
		t.Errorf("err = %v, want ErrUnscorable", err)
	}
}

func TestTwoFinalCallsWithDifferentFlagsAreUnscorable(t *testing.T) {
	_, err := ScaffoldOf(runOf("rastrillo new --shell=topbar groomer; "+
		"rastrillo new --shell=sidebar groomer"), "groomer")
	if !errors.Is(err, ErrUnscorable) {
		t.Errorf("err = %v, want ErrUnscorable", err)
	}
}

func authoredFiles(t *testing.T) map[string]File {
	t.Helper()
	code, err := Authored("testdata/app", "testdata/scaffold")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]File{}
	for _, file := range code.Files {
		files[file.Path] = file
	}
	return files
}

func keys(file File) []string {
	var found []string
	for _, function := range file.Funcs {
		found = append(found, function.Key)
	}
	return found
}

func TestAPackageLevelFuncLiteralIsAnAuthoredUnit(t *testing.T) {
	file, ok := authoredFiles(t)["routes.go"]
	if !ok {
		t.Fatal("routes.go is not authored")
	}
	if got := keys(file); !slices.Equal(got, []string{"handler"}) {
		t.Errorf("funcs = %v, want [handler]", got)
	}
	if file.Funcs[0].Start != 3 || file.Funcs[0].End != 5 {
		t.Errorf("handler spans %d-%d, want 3-5",
			file.Funcs[0].Start, file.Funcs[0].End)
	}
}

func TestAuthoredCodeIsNewFilesAndChangedFunctions(t *testing.T) {
	files := authoredFiles(t)
	bookings, ok := files["bookings.go"]
	if !ok || !bookings.Whole {
		t.Fatalf("bookings.go = %+v, want a whole file", bookings)
	}
	if got := keys(bookings); !slices.Equal(got, []string{"NewBooking", "Booking.Name"}) {
		t.Errorf("bookings funcs = %v", got)
	}
	app := files["app.go"]
	if app.Whole || !slices.Equal(keys(app), []string{"New"}) {
		t.Errorf("app.go = %+v, want only New", app)
	}
	if _, ok := files["handlers.go"]; ok {
		t.Error("handlers.go is untouched but authored")
	}
}

// The fixture is gofmt-clean, since the repo's gofmt lane covers testdata.
// The test reindents render.go with spaces in a copy, so the file differs
// from the scaffold by formatting and a comment only.
func TestGofmtAndCommentOnlyChangesAreNotAuthored(t *testing.T) {
	app := t.TempDir()
	if err := os.CopyFS(app, os.DirFS("testdata/app")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(app, "render.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	spaced := strings.ReplaceAll(string(source), "\t", "    ")
	if err := os.WriteFile(path, []byte(spaced), 0o644); err != nil {
		t.Fatal(err)
	}
	code, err := Authored(app, "testdata/scaffold")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range code.Files {
		if file.Path == "render.go" {
			t.Errorf("render.go = %+v, want absent", file)
		}
	}
}

func TestGeneratedAndVendoredFilesAreNeverAuthored(t *testing.T) {
	for path := range authoredFiles(t) {
		switch path {
		case "gen/x.go", "generated.go", "static/a.go", "vendor/v.go",
			"testdata/t.go":
			t.Errorf("%s is authored", path)
		}
	}
}

func TestTestFilesAreMarked(t *testing.T) {
	file, ok := authoredFiles(t)["bookings_test.go"]
	if !ok || !file.Test {
		t.Errorf("bookings_test.go = %+v, want present with Test", file)
	}
}

func TestRunModuleIsTheGoModPath(t *testing.T) {
	got, err := RunModule("testdata/app")
	if err != nil || got != "groomer" {
		t.Errorf("module = %q, %v, want groomer", got, err)
	}
}

// keptCommand is a Bash command copied verbatim from a kept groomer
// transcript, with what ScaffoldOf must make of it. A nil Scaffold means
// it is not a scaffold call.
type keptCommand struct {
	Run      string
	Why      string
	Command  string
	Scaffold []string
}

func TestKeptTranscriptCommands(t *testing.T) {
	file, err := os.Open("testdata/kept-commands.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	for decoder.More() {
		var kept keptCommand
		if err := decoder.Decode(&kept); err != nil {
			t.Fatal(err)
		}
		got, err := ScaffoldOf(runOf(kept.Command), "groomer")
		if kept.Scaffold == nil {
			if !errors.Is(err, ErrUnscorable) {
				t.Errorf("%s (%s): %+v, %v, want ErrUnscorable",
					kept.Run, kept.Why, got, err)
			}
			continue
		}
		if err != nil || !slices.Equal(got.Flags, kept.Scaffold) {
			t.Errorf("%s (%s): %+v, %v, want flags %v",
				kept.Run, kept.Why, got, err, kept.Scaffold)
		}
	}
}

// An action taking a dot-imported *Visit is keyed as codelimits keys it,
// so codelimits's length offence on it attaches to the authored unit.
// testdata/dotaction is measured by both.
func TestADotImportedActionIsKeyedAsCodelimitsKeysIt(t *testing.T) {
	app := filepath.Join("testdata", "dotaction")
	code, err := Authored(app, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	offences, err := codelimits.Measure(app)
	if err != nil {
		t.Fatal(err)
	}

	var units []string
	for _, file := range code.Files {
		units = append(units, keys(file)...)
	}
	want := []string{"Callbacks.action1", "Callbacks.func1"}
	if !slices.Equal(units, want) {
		t.Errorf("authored units = %v, want %v", units, want)
	}
	var long []string
	for _, offence := range offences {
		if offence.Kind == codelimits.FuncLines {
			long = append(long, offence.Key)
		}
	}
	if !slices.Equal(long, []string{"Callbacks.action1"}) {
		t.Errorf("codelimits length offences = %v, want Callbacks.action1",
			long)
	}
}
