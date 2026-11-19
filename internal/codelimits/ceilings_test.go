package codelimits

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func columnsOffence(path, key string, line int) Offence {
	return Offence{
		Kind: Columns, Path: path, Key: key, Size: 90,
		Limit: MaxColumns, Line: line,
	}
}

func funcOffence(path, name string, size, line int) Offence {
	return Offence{
		Kind: FuncLines, Path: path, Key: name, Size: size,
		Limit: MaxFuncLines, Line: line,
	}
}

func fileOffence(path string, size int) Offence {
	return Offence{
		Kind: FileLines, Path: path, Size: size,
		Limit: MaxFileLines, Line: 1,
	}
}

func recordOf(kind Kind, path, key string, count int) Record {
	return Record{Kind: kind, Path: path, Key: key, Count: count}
}

// wantOne fails unless there is exactly one message and it says every
// part, because a message that names the wrong file or leaves out what
// to do is as useless as none.
func wantOne(t *testing.T, messages []string, parts ...string) {
	t.Helper()
	if len(messages) != 1 {
		t.Fatalf("want one message, got %d: %q", len(messages), messages)
	}
	for _, part := range parts {
		if !strings.Contains(messages[0], part) {
			t.Errorf("message %q does not say %q", messages[0], part)
		}
	}
}

func TestAnUnrecordedOffenceFails(t *testing.T) {
	offences := []Offence{columnsOffence("app/web.go", "0123456789ab", 12)}
	wantOne(t, Check(offences, &File{}), "app/web.go", "12", "80")
}

func TestEditingARecordedLongLineMakesANewOffence(t *testing.T) {
	file := &File{Records: []Record{
		recordOf(Columns, "app/web.go", "aaaaaaaaaaaa", 1),
	}}
	edited := []Offence{columnsOffence("app/web.go", "bbbbbbbbbbbb", 12)}
	messages := Check(edited, file)
	if len(messages) != 2 {
		t.Fatalf("want a new offence and a stale record, got %q", messages)
	}
	if !strings.Contains(messages[0], "line 12") {
		t.Errorf("the new offence is not placed: %q", messages[0])
	}
	if !strings.Contains(messages[1], "Delete") {
		t.Errorf("the stale record is not told to go: %q", messages[1])
	}
}

func TestAFunctionGrowingPastItsCeilingFails(t *testing.T) {
	file := &File{Records: []Record{
		{Kind: FuncLines, Path: "app/web.go", Key: "Serve", Count: 45, Line: 7},
	}}
	same := []Offence{funcOffence("app/web.go", "Serve", 45, 3)}
	if messages := Check(same, file); len(messages) != 0 {
		t.Fatalf("a function at its ceiling failed: %q", messages)
	}
	grown := []Offence{funcOffence("app/web.go", "Serve", 46, 3)}
	wantOne(t, Check(grown, file), "Serve", "46", "45", "line 7")
}

func TestAFileGrowingPastItsCeilingFails(t *testing.T) {
	file := &File{Records: []Record{recordOf(FileLines, "a.go", "", 450)}}
	grown := []Offence{fileOffence("a.go", 451)}
	wantOne(t, Check(grown, file), "a.go", "451", "450")
}

func TestShrinkingMeansLowering(t *testing.T) {
	file := &File{Records: []Record{
		recordOf(FuncLines, "app/web.go", "Serve", 45),
		recordOf(Columns, "app/web.go", "aaaaaaaaaaaa", 3),
		recordOf(Names, "app/web.go", "Serve/i", 2),
	}}
	offences := []Offence{
		funcOffence("app/web.go", "Serve", 44, 3),
		columnsOffence("app/web.go", "aaaaaaaaaaaa", 5),
		columnsOffence("app/web.go", "aaaaaaaaaaaa", 9),
		{Kind: Names, Path: "app/web.go", Key: "Serve/i", Size: 1, Line: 4},
	}
	messages := Check(offences, file)
	if len(messages) != 3 {
		t.Fatalf("want three records to lower, got %q", messages)
	}
	// Check orders them as the offences sort: columns, func, names.
	wantOne(t, messages[:1], "app/web.go line 5", "2 lines this wide",
		"allows 3")
	wantOne(t, messages[1:2], "app/web.go line 3", "Serve is now 44 lines",
		"allows 45")
	wantOne(t, messages[2:], "app/web.go line 4", `"i" in Serve`,
		"1 time", "allows 2")
}

func TestShrinkingAFileOrAPackageLevelNameMeansLowering(t *testing.T) {
	file := &File{Records: []Record{
		recordOf(FileLines, "a.go", "", 450),
		recordOf(Names, "b.go", "-/x", 2),
	}}
	offences := []Offence{
		fileOffence("a.go", 405),
		{Kind: Names, Path: "b.go", Key: "-/x", Size: 1, Line: 6},
	}
	messages := Check(offences, file)
	if len(messages) != 2 {
		t.Fatalf("want two records to lower, got %q", messages)
	}
	wantOne(t, messages[:1], "a.go is now 405 lines", "allows 450")
	wantOne(t, messages[1:], `"x" at package level`, "allows 2")
}

func TestMoreIdenticalLongLinesThanRecordedFails(t *testing.T) {
	file := &File{Records: []Record{
		recordOf(Columns, "a.go", "aaaaaaaaaaaa", 1),
	}}
	offences := []Offence{
		columnsOffence("a.go", "aaaaaaaaaaaa", 5),
		columnsOffence("a.go", "aaaaaaaaaaaa", 9),
	}
	wantOne(t, Check(offences, file), "a.go", "2", "1")
}

func TestAStaleRecordFails(t *testing.T) {
	file := &File{Records: []Record{
		{Kind: FuncLines, Path: "gone.go", Key: "Old", Count: 50, Line: 9},
	}}
	wantOne(t, Check(nil, file), "gone.go", "Old", "line 9", "Delete")
}

func TestLowerNeverAddsOrRaises(t *testing.T) {
	file := &File{Records: []Record{
		{Kind: FuncLines, Path: "a.go", Key: "Shrunk", Count: 50, Reason: "r"},
		{Kind: FuncLines, Path: "a.go", Key: "Grown", Count: 50},
		{Kind: FuncLines, Path: "a.go", Key: "Gone", Count: 50},
		recordOf(Columns, "a.go", "aaaaaaaaaaaa", 3),
	}}
	offences := []Offence{
		funcOffence("a.go", "Shrunk", 42, 1),
		funcOffence("a.go", "Grown", 60, 2),
		funcOffence("a.go", "New", 70, 3),
		columnsOffence("a.go", "aaaaaaaaaaaa", 4),
	}
	got := Lower(offences, file)
	want := []Record{
		{Kind: FuncLines, Path: "a.go", Key: "Shrunk", Count: 42, Reason: "r"},
		{Kind: FuncLines, Path: "a.go", Key: "Grown", Count: 50},
		recordOf(Columns, "a.go", "aaaaaaaaaaaa", 1),
	}
	if !slices.Equal(got.Records, want) {
		t.Fatalf("lowered to %+v, want %+v", got.Records, want)
	}
}

// Measure numbers namesakes; the judge holds each to its own record.
func TestTwoFunctionsWithOneNameAreTwoRecords(t *testing.T) {
	offences := []Offence{
		funcOffence("a.go", "init", 44, 3),
		funcOffence("a.go", "init~2", 52, 60),
		funcOffence("a.go", "init~3", 41, 130),
	}
	got := Init(offences)
	want := []Record{
		recordOf(FuncLines, "a.go", "init", 44),
		recordOf(FuncLines, "a.go", "init~2", 52),
		recordOf(FuncLines, "a.go", "init~3", 41),
	}
	if !slices.Equal(got.Records, want) {
		t.Fatalf("init wrote %+v, want %+v", got.Records, want)
	}
	if messages := Check(offences, got); len(messages) != 0 {
		t.Fatalf("the records init wrote do not pass: %q", messages)
	}
	grown := slices.Clone(offences)
	grown[1].Size = 53
	wantOne(t, Check(grown, got), "init~2", "53", "52")
}

func TestInitRecordsEveryOffenceWithoutAReason(t *testing.T) {
	offences := []Offence{
		fileOffence("big.go", 410),
		columnsOffence("a.go", "aaaaaaaaaaaa", 4),
		columnsOffence("a.go", "aaaaaaaaaaaa", 8),
		{Kind: Names, Path: "a.go", Key: "-/x", Size: 1, Line: 2},
	}
	got := Init(offences)
	want := []Record{
		recordOf(Columns, "a.go", "aaaaaaaaaaaa", 2),
		recordOf(Names, "a.go", "-/x", 1),
		recordOf(FileLines, "big.go", "", 410),
	}
	if !slices.Equal(got.Records, want) {
		t.Fatalf("init wrote %+v, want %+v", got.Records, want)
	}
	if messages := Check(offences, got); len(messages) != 0 {
		t.Fatalf("the records init wrote do not pass: %q", messages)
	}
}

func TestFormatThenParseRoundTrips(t *testing.T) {
	file := &File{Records: []Record{
		recordOf(Columns, "a.go", "0123456789ab", 2),
		recordOf(FileLines, "b.go", "", 410),
		{Kind: FuncLines, Path: "c.go", Key: "Recv.Name", Count: 52,
			Reason: "a long table of cases, split in step 4"},
		recordOf(Names, "d.go", "-/x", 1),
	}}
	parsed, err := Parse(Format(file))
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Records) != len(file.Records) {
		t.Fatalf("parsed %+v", parsed.Records)
	}
	for index, record := range parsed.Records {
		if record.Line == 0 {
			t.Errorf("record %d carries no line number", index)
		}
		record.Line = 0
		if record != file.Records[index] {
			t.Errorf("record %d came back as %+v, want %+v",
				index, record, file.Records[index])
		}
	}
}

func TestFormatSortsAndSaysRecordsOnlyGoDown(t *testing.T) {
	text := string(Format(&File{Records: []Record{
		recordOf(Names, "b.go", "-/x", 1),
		recordOf(Names, "a.go", "-/x", 1),
	}}))
	if !strings.HasPrefix(text, "#") || !strings.Contains(text, "down") {
		t.Errorf("no header saying records only go down: %q", text)
	}
	if strings.Index(text, "a.go") > strings.Index(text, "b.go") {
		t.Errorf("records are not sorted: %q", text)
	}
}

func TestAMalformedLineNamesItsLine(t *testing.T) {
	for name, text := range map[string]string{
		"unknown kind":       "widths a.go - 1",
		"too few fields":     "func a.go Serve",
		"count not a number": "func a.go Serve many",
		"count is zero":      "func a.go Serve 0",
		"file with a key":    "file a.go Serve 410",
		"func without key":   "func a.go - 45",
		"duplicate record":   "func a.go Serve 45\nfunc a.go Serve 46",
	} {
		_, err := Parse([]byte("# header\n\n" + text + "\n"))
		if err == nil {
			t.Errorf("%s: parsed without an error", name)
			continue
		}
		if !strings.Contains(err.Error(), "line 3") &&
			!strings.Contains(err.Error(), "line 4") {
			t.Errorf("%s: error does not name the line: %v", name, err)
		}
	}
}

func TestAMissingFileIsEmpty(t *testing.T) {
	file, err := Load(t.TempDir())
	if err != nil || file == nil || len(file.Records) != 0 {
		t.Fatalf("got %+v, %v", file, err)
	}
}

// bigInit is an init function of the given body length, written as a
// real source file's would be.
func bigInit(lines int) string {
	return "func init() {\n" + strings.Repeat("\tprintln()\n", lines) + "}\n"
}

func TestTwoRealInitFunctionsAreJudgedApart(t *testing.T) {
	root := t.TempDir()
	source := "package demo\n\n" + bigInit(44) + "\n" + bigInit(52)
	path := filepath.Join(root, "demo.go")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	offences, err := Measure(root)
	if err != nil {
		t.Fatal(err)
	}
	records := Init(offences)
	want := []Record{
		recordOf(FuncLines, "demo.go", "init", 44),
		recordOf(FuncLines, "demo.go", "init~2", 52),
	}
	if !slices.Equal(records.Records, want) {
		t.Fatalf("init wrote %+v, want %+v", records.Records, want)
	}
	if messages := Check(offences, records); len(messages) != 0 {
		t.Fatalf("the records init wrote do not pass: %q", messages)
	}
}

// twoInits is a module whose two init functions have the given body
// lengths, recorded as they were, then rewritten to the new lengths.
func twoInits(t *testing.T, first, second, newFirst, newSecond int) string {
	t.Helper()
	module := scratchModule(t,
		"package m\n\n"+bigInit(first)+"\n"+bigInit(second))
	run(t, InitMode, module)
	writeFile(t, filepath.Join(module, "a.go"),
		"package m\n\n"+bigInit(newFirst)+"\n"+bigInit(newSecond))
	return module
}

// Fixing the first of two init functions must leave the second judged
// by its own record. When the numbering came after the short ones were
// dropped, the second took the first's name, and with it the first's
// ceiling.
func TestFixingOneOfTwoNamesakesLeavesTheOtherItsRecord(t *testing.T) {
	module := twoInits(t, 44, 52, 30, 52)
	checked := run(t, CheckMode, module)
	wantOne(t, checked.Messages, "func init in a.go", "Delete")

	lowered := run(t, LowerMode, module)
	if len(lowered.Messages) > 0 || lowered.Records != 1 {
		t.Fatalf("lower: %+v, want a pass on one record", lowered)
	}
	records := readRecords(t, module)
	if !strings.Contains(records, "func a.go init~2 52\n") ||
		strings.Contains(records, "func a.go init ") {
		t.Fatalf("the second init lost its own record:\n%s", records)
	}
}

// The converse: with the first fixed, the second must not grow into the
// first's larger ceiling and have --lower call that a pass.
func TestANamesakeCannotGrowIntoAFixedOnesCeiling(t *testing.T) {
	module := twoInits(t, 52, 44, 30, 50)
	lowered := run(t, LowerMode, module)
	wantOne(t, lowered.Messages, "init~2 is now 50 lines", "44")
}
