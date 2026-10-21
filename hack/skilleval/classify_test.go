package main

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	testFW  = "/work/v/rastrillo"
	testMod = "/home/u/go/pkg/mod/amadan.net/rastrillo/rastrillo@v0.30.0"
	testApp = "/work/v/expenses/1/app"
)

func testClassifier() classifier {
	return classifier{roots: []string{testFW}, staleBase: "/home/u/go/pkg/mod/amadan.net/rastrillo/rastrillo", app: testApp}
}

func TestClassifyFileTools(t *testing.T) {
	c := testClassifier()
	cases := []struct {
		tool string
		in   map[string]any
		want bucket
	}{
		{"Read", map[string]any{"file_path": testFW + "/SKILL.md"}, bSkill},
		{"Read", map[string]any{"file_path": testFW + "/skill/jobs.md"}, bSkill},
		{"Read", map[string]any{"file_path": testMod + "/SKILL.md"}, bSkill},
		{"Read", map[string]any{"file_path": testFW + "/docs/site/jobs.md"}, bDocs},
		{"Read", map[string]any{"file_path": testFW + "/examples/notes/internal/notes/app.go"}, bExamples},
		{"Read", map[string]any{"file_path": testFW + "/jobs/jobs.go"}, bSource},
		{"Read", map[string]any{"file_path": testMod + "/form/form.go"}, bSource},
		{"Grep", map[string]any{"pattern": "Owned", "path": testFW}, bSource},
		{"Glob", map[string]any{"pattern": "**/*.go", "path": testFW + "/scope"}, bSource},
		{"Read", map[string]any{"file_path": testApp + "/internal/expenses/app.go"}, bApp},
		{"Grep", map[string]any{"pattern": "x"}, bApp},
		{"Read", map[string]any{"file_path": "/etc/hosts"}, bOther},
		{"Edit", map[string]any{"file_path": testFW + "/jobs/jobs.go"}, bFWWrite},
		{"Write", map[string]any{"file_path": testApp + "/internal/expenses/x.go"}, bApp},
		{"Skill", map[string]any{"skill": "rastrillo"}, bSkill},
		{"WebFetch", map[string]any{"url": "https://rastrillo.org/docs/jobs.md"}, bWeb},
		{"Grep", map[string]any{"pattern": "Start", "path": "../../../rastrillo/jobs"}, bSource},
		{"Glob", map[string]any{"pattern": testFW + "/ui/partials/*.html"}, bSource},
		{"TodoWrite", map[string]any{"todos": []any{}}, bOther},
	}
	for _, tc := range cases {
		if got := c.classify(tc.tool, tc.in); got != tc.want {
			t.Errorf("%s %v = %s, want %s", tc.tool, tc.in, got, tc.want)
		}
	}
}

// An agent reaches the framework from Bash as often as from Read, and
// usually through go list rather than a literal path, so the shell
// forms are where a classifier that only matched literal paths would
// report a source-free run that was not.
func TestClassifyBash(t *testing.T) {
	c := testClassifier()
	cases := []struct {
		cmd  string
		want bucket
	}{
		{"cat " + testFW + "/SKILL.md", bSkill},
		{"sed -n 1,80p " + testMod + "/docs/site/forms.md", bDocs},
		{"grep -rn Owned " + testFW + "/scope/", bSource},
		{`cat "$(go list -m -f '{{.Dir}}' amadan.net/rastrillo/rastrillo)/SKILL.md"`, bSkill},
		{`grep -rn Start $(go list -m -f '{{.Dir}}' amadan.net/rastrillo/rastrillo)/jobs`, bSource},
		{`cd $(go list -m -f {{.Dir}} amadan.net/rastrillo/rastrillo) && grep -rn media-grid .`, bSource},
		{"go list -m -f '{{.Dir}}' amadan.net/rastrillo/rastrillo", bOther},
		// Seen in the first real run: the directory held in a variable,
		// then grepped, and with the module path mistyped besides.
		{`d=$(go list -m -f '{{.Dir}}' amadan.net/rastrillo 2>&1); echo $d; grep -rn "scheme-menu" $d --include=*.html -l`, bSource},
		{"go doc amadan.net/rastrillo/rastrillo/jobs Jobs.Start", bGoDoc},
		{"curl -s https://rastrillo.org/docs/jobs.md", bWeb},
		{"go test ./...", bApp},
		{"make ci", bApp},
	}
	for _, tc := range cases {
		got := c.classify("Bash", map[string]any{"command": tc.cmd})
		if got != tc.want {
			t.Errorf("Bash %q = %s, want %s", tc.cmd, got, tc.want)
		}
	}
}

// The baseline's agents mostly cd into the framework and then name
// files relative to it, often several in one command. A classifier
// that only matched absolute paths called every such call source and
// reported that no agent had read SKILL.md, when most had.
func TestBashTouches(t *testing.T) {
	c := testClassifier()
	cases := []struct {
		cmd  string
		want []bucket
	}{
		{"cd " + testFW + "; cat SKILL.md; ls examples examples/notes", []bucket{bExamples, bSkill}},
		{"R=" + testFW + "; cd $R; cat SKILL.md", []bucket{bSkill}},
		{"cd " + testFW + " && sed -n 1,80p docs/site/jobs.md", []bucket{bDocs}},
		{"cd " + testFW + "/examples/notes && cat internal/notes/app.go", []bucket{bExamples}},
		{"cd " + testFW + "/ui/partials; cat field-money.html confirm-form.html", []bucket{bSource}},
		{"R=" + testFW + "; cd $R; sed -n 60,200p password/handlers.go; cd ui/partials; cat field-money.html", []bucket{bSource}},
		{"cat " + testFW + "/SKILL.md; grep -rn x " + testFW + "/jobs", []bucket{bSource, bSkill}},
		{`cat "$(go list -m -f '{{.Dir}}' amadan.net/rastrillo/rastrillo)/SKILL.md"`, []bucket{bSkill}},
		{"cd " + testFW + "/x && cd " + testApp + " && cat internal/a.go", []bucket{bApp}},
		{"cd " + testApp + "; go test ./...", []bucket{bApp}},
		// From the review of f199170a: a relative path out of the app,
		// go doc or a docs URL beside a source read, and a variable
		// holding go list's answer all hid what was read.
		{"cat ../../../rastrillo/jobs/jobs.go", []bucket{bSource}},
		{"go doc fmt.Println; cat " + testFW + "/jobs/jobs.go", []bucket{bSource, bGoDoc}},
		{"curl -s https://rastrillo.org/docs/jobs.md; grep -rn x " + testFW + "/jobs", []bucket{bSource, bWeb}},
		{`d=$(go list -m -f '{{.Dir}}' amadan.net/rastrillo/rastrillo); cat "$d/SKILL.md"`, []bucket{bSkill}},
	}
	for _, tc := range cases {
		got, _ := c.touches("Bash", map[string]any{"command": tc.cmd})
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("Bash %q touches %v, want %v", tc.cmd, got, tc.want)
		}
	}
}

// The token signal is what came back into context, not how many calls
// were made: one grep -r over the framework can cost more than twenty
// reads of the app. So the tally sums result bytes per bucket, matched
// to its call by tool_use_id, and takes usage and cost from the result
// event alone (assistant events repeat a message's usage per block).
func TestTallyTranscript(t *testing.T) {
	transcript := strings.Join([]string{
		`{"type":"system","subtype":"init","model":"claude-sonnet-5-5"}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"` + testFW + `/SKILL.md"}}],"usage":{"input_tokens":5}}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"0123456789"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"ok"},{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"grep -rn x ` + testFW + `/jobs"}}],"usage":{"input_tokens":5}}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","content":[{"type":"text","text":"abcde"},{"type":"text","text":"fgh"}]}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t3","name":"Read","input":{"file_path":"` + testFW + `/jobs/handlers.go"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t3","content":"xyz"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"num_turns":4,"duration_ms":1000,"total_cost_usd":0.5,"usage":{"input_tokens":10,"cache_creation_input_tokens":100,"cache_read_input_tokens":1000,"output_tokens":50}}`,
	}, "\n")
	tally, err := tallyTranscript(strings.NewReader(transcript), testClassifier())
	if err != nil {
		t.Fatal(err)
	}
	if got := tally.Buckets[bSkill]; got.Calls != 1 || got.Bytes != 10 {
		t.Errorf("skill = %+v, want 1 call, 10 bytes", got)
	}
	if got := tally.Buckets[bSource]; got.Calls != 2 || got.Bytes != 11 {
		t.Errorf("source = %+v, want 2 calls, 11 bytes", got)
	}
	if want := []string{"Bash grep -rn x " + testFW + "/jobs", "Read " + testFW + "/jobs/handlers.go"}; strings.Join(tally.SourceReads, "|") != strings.Join(want, "|") {
		t.Errorf("source reads = %q, want %q", tally.SourceReads, want)
	}
	if tally.Turns != 4 || tally.CostUSD != 0.5 || tally.Tokens != 1160 || tally.OutputTokens != 50 {
		t.Errorf("totals = %+v", tally)
	}
	if tally.Touched[bSkill] != 1 || tally.Touched[bSource] != 2 {
		t.Errorf("touched = %v, want skill 1, source 2", tally.Touched)
	}
	if !tally.Finished {
		t.Error("a success result event must mark the run finished")
	}
}

// A run killed at its timeout has spent tokens but never wrote a
// result event. Reporting zero would make the worst runs look the
// cheapest, so the tally falls back to the assistant events' usage,
// counted once per message id, and says it is partial.
func TestTallyWithoutResultIsUnfinishedAndPartial(t *testing.T) {
	transcript := strings.Join([]string{
		`{"type":"system","subtype":"init","claude_code_version":"9.9.9"}`,
		`{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"a"}],"usage":{"input_tokens":1,"cache_read_input_tokens":100,"output_tokens":10}}}`,
		`{"type":"assistant","message":{"id":"m1","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"` + testFW + `/SKILL.md"}}],"usage":{"input_tokens":1,"cache_read_input_tokens":100,"output_tokens":10}}}`,
		`{"type":"assistant","message":{"id":"m2","content":[{"type":"text","text":"b"}],"usage":{"input_tokens":2,"cache_creation_input_tokens":5,"cache_read_input_tokens":200,"output_tokens":20}}}`,
	}, "\n")
	tally, err := tallyTranscript(strings.NewReader(transcript), testClassifier())
	if err != nil {
		t.Fatal(err)
	}
	if tally.Finished {
		t.Error("a transcript cut off before its result event must not read as finished")
	}
	if !tally.Partial || tally.Tokens != 111+227 || tally.OutputTokens != 30 {
		t.Errorf("partial=%v tokens=%d output=%d, want partial, 338, 30", tally.Partial, tally.Tokens, tally.OutputTokens)
	}
	if tally.ClaudeVersion != "9.9.9" {
		t.Errorf("claude version = %q", tally.ClaudeVersion)
	}
}

// A run that read a module-cache copy read a skill other than the
// variant under test, so the tally flags it rather than crediting the
// variant.
func TestTallyCountsStaleCopies(t *testing.T) {
	transcript := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"cat ` + testMod + `/SKILL.md"}}]}}`
	tally, err := tallyTranscript(strings.NewReader(transcript), testClassifier())
	if err != nil {
		t.Fatal(err)
	}
	if tally.StaleCalls != 1 {
		t.Errorf("stale calls = %d, want 1", tally.StaleCalls)
	}
}

// Round 2 of the review: searches with no path operand, quoted paths
// with spaces, subshell scope, and a cwd that Claude Code carries from
// one Bash call to the next while it stays inside the project.
func TestBashRound2(t *testing.T) {
	c := testClassifier()
	cases := []struct {
		cmd  string
		want []bucket
	}{
		{"cd " + testFW + "; rg Owned", []bucket{bSource}},
		{"cd " + testFW + "; ls jobs", []bucket{bSource}},
		{"cd " + testFW + "; cat SKILL.md; rg Owned", []bucket{bSource, bSkill}},
		{"cd " + testFW + "; cat SKILL.md | grep -n jobs", []bucket{bSkill}},
		{"(cd " + testFW + "; cat SKILL.md); cat internal/a.go", []bucket{bSkill}},
	}
	for _, tc := range cases {
		got, _ := c.touches("Bash", map[string]any{"command": tc.cmd})
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("Bash %q touches %v, want %v", tc.cmd, got, tc.want)
		}
	}

	spaced := classifier{roots: []string{"/work/eval results/head/rastrillo"}, app: "/work/eval results/head/x/1/x"}
	if got, _ := spaced.touches("Bash", map[string]any{"command": `cat "/work/eval results/head/rastrillo/SKILL.md"`}); fmt.Sprint(got) != "[skill]" {
		t.Errorf("a quoted path with a space touches %v, want [skill]", got)
	}

	home := classifier{
		roots:     []string{"/home/u/.cache/skilleval/r/head/rastrillo"},
		staleBase: "/home/u/go/pkg/mod/amadan.net/rastrillo/rastrillo",
		app:       "/home/u/.cache/skilleval/r/head/x/1/x",
	}
	got, stale := home.touches("Grep", map[string]any{"pattern": "Start", "path": "../../../../../../../go/pkg/mod/amadan.net/rastrillo/rastrillo@v0.30.0/jobs"})
	if fmt.Sprint(got) != "[source]" || !stale {
		t.Errorf("a relative path into a cache copy = %v stale=%v, want [source] stale=true", got, stale)
	}
}

func TestTallyCarriesCwdInsideTheProject(t *testing.T) {
	bash := func(id, cmd string) string {
		return `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"` + id + `","name":"Bash","input":{"command":` + strconv.Quote(cmd) + `}}]}}`
	}
	transcript := strings.Join([]string{
		bash("t1", "cd internal"),
		bash("t2", "cat ../../../../rastrillo/jobs/jobs.go"), // from app/internal: the framework
		bash("t3", "cd "+testFW),
		bash("t4", "cat SKILL.md"), // leaving the project resets to the app, so this is the app's
	}, "\n")
	tally, err := tallyTranscript(strings.NewReader(transcript), testClassifier())
	if err != nil {
		t.Fatal(err)
	}
	if tally.Touched[bSource] != 1 || tally.Touched[bSkill] != 0 {
		t.Errorf("touched = %v, want one source read and no skill read", tally.Touched)
	}
}

func TestTallyWithNoUsageIsStillPartial(t *testing.T) {
	tally, err := tallyTranscript(strings.NewReader(`{"type":"system","subtype":"init"}`), testClassifier())
	if err != nil {
		t.Fatal(err)
	}
	if !tally.Partial {
		t.Error("a run that died before any usage was streamed must read as partial, not as free")
	}
}

// Round 3: a search's pattern is not its operand, a bare directory
// operand is one, a substitution inside double quotes still runs, and
// a cache version that did not exist when the run started is still a
// stale copy.
func TestBashRound3(t *testing.T) {
	c := testClassifier()
	cases := []struct {
		cmd  string
		want []bucket
	}{
		{"cd " + testFW + "; rg SKILL.md", []bucket{bSource}},
		{"cd " + testFW + "; ls examples", []bucket{bExamples}},
		{"cd " + testFW + "; grep -rn -e Owned scope", []bucket{bSource}},
		{"cd " + testFW + "; rg -g '*.md' Owned docs", []bucket{bDocs}},
		{"cd " + testFW + "; fd media", []bucket{bSource}},
		{`printf '%s' "$(cat ` + testFW + `/jobs/jobs.go)"`, []bucket{bSource}},
		{`echo "in $(cd ` + testFW + ` && cat SKILL.md) out"; cat internal/a.go`, []bucket{bSkill}},
	}
	for _, tc := range cases {
		got, _ := c.touches("Bash", map[string]any{"command": tc.cmd})
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("Bash %q touches %v, want %v", tc.cmd, got, tc.want)
		}
	}
	got, stale := c.touches("Read", map[string]any{"file_path": "/home/u/go/pkg/mod/amadan.net/rastrillo/rastrillo@v9.9.9/SKILL.md"})
	if fmt.Sprint(got) != "[skill]" || !stale {
		t.Errorf("an unlisted cache version = %v stale=%v, want [skill] stale=true", got, stale)
	}
}

// Round 4: rg's patternless modes, find's leading options, and a quoted
// parenthesis that is a pattern, not a subshell.
func TestBashRound4(t *testing.T) {
	c := testClassifier()
	cases := []struct {
		cmd  string
		want []bucket
	}{
		{"rg --files " + testFW + "/skill", []bucket{bSkill}},
		{"find -L " + testFW + " -name '*.go'", []bucket{bSource}},
		{"grep -F '(' " + testFW + "/jobs/jobs.go", []bucket{bSource}},
		{`grep -n "a;b" ` + testFW + "/docs/site/jobs.md", []bucket{bDocs}},
		// Round 5: --type-list prints rg's built-in types and reads nothing.
		{"rg --type-list " + testFW + "/skill", []bucket{bApp}},
		// Round 6: --type-list anywhere in the command, and -- ending
		// the options.
		{"rg --files " + testFW + "/skill --type-list", []bucket{bApp}},
		{"rg -- --type-list " + testFW + "/jobs/jobs.go", []bucket{bSource}},
	}
	for _, tc := range cases {
		got, _ := c.touches("Bash", map[string]any{"command": tc.cmd})
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("Bash %q touches %v, want %v", tc.cmd, got, tc.want)
		}
	}
}

// Round 7: a heredoc's body is data, not commands, and an option can
// carry its value in the same word.
func TestBashRound7(t *testing.T) {
	c := testClassifier()
	cases := []struct {
		cmd  string
		want []bucket
	}{
		{"cat > NOTES.md <<'EOF'\ncat " + testFW + "/SKILL.md\ncd " + testFW + "\nEOF\ncat internal/a.go", []bucket{bApp}},
		{"cat > a.go <<EOF && go build ./...\npackage a\nEOF\ngrep -rn x " + testFW + "/jobs", []bucket{bSource}},
		{"cat <<-END\n\tcat " + testFW + "/SKILL.md\n\tEND\necho done", []bucket{bApp}},
		{"rg --regexp=Scheme " + testFW + "/scheme.go", []bucket{bSource}},
		{"grep -eScheme " + testFW + "/scheme.go", []bucket{bSource}},
	}
	for _, tc := range cases {
		got, _ := c.touches("Bash", map[string]any{"command": tc.cmd})
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("Bash %q touches %v, want %v", tc.cmd, got, tc.want)
		}
	}
}

// Round 8: regressions in round 7's heredoc and option handling.
func TestBashRound8(t *testing.T) {
	c := testClassifier()
	cases := []struct {
		cmd  string
		want []bucket
	}{
		{"cat <<< \"hello\"\ncat " + testFW + "/scheme.go", []bucket{bSource}},
		{"cat <<EOF\n$(cat " + testFW + "/scheme.go)\nEOF", []bucket{bSource}},
		{"bash <<'SH'\ncat " + testFW + "/scheme.go\nSH", []bucket{bSource}},
		{"cat <<'SH'\ncat " + testFW + "/scheme.go\nSH", []bucket{bApp}},
		{"rg -e '^ci:' -tmake " + testFW + "/Makefile", []bucket{bSource}},
		{"grep -rne Owned " + testFW + "/scope", []bucket{bSource}},
		{"cat <<'END NOTE'\nhello\nEND NOTE\ncat " + testFW + "/scheme.go", []bucket{bSource}},
	}
	for _, tc := range cases {
		got, _ := c.touches("Bash", map[string]any{"command": tc.cmd})
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("Bash %q touches %v, want %v", tc.cmd, got, tc.want)
		}
	}
}

// Round 9: no command may panic the classifier, a child shell's cd is
// its own, wrappers and assignments are not the command, and arities
// differ between grep and rg.
func TestBashRound9(t *testing.T) {
	c := testClassifier()
	cases := []struct {
		cmd  string
		want []bucket
	}{
		{"cat <<EOF\n\\$(\nEOF", []bucket{bApp}},
		{"cat <<EOF\n$(\nEOF", []bucket{bApp}},
		{"env python3 <<'PY'\nprint(open(\"" + testFW + "/scheme.go\").read())\nPY", []bucket{bSource}},
		{"LC_ALL=C bash <<'SH'\ncat " + testFW + "/scheme.go\nSH", []bucket{bSource}},
		{"bash -c cat <<'SH'\ncat " + testFW + "/scheme.go\nSH", []bucket{bApp}},
		{"sh <<SH\nprintf '%s\\n' '$(head -n 1 " + testFW + "/scheme.go)'\nSH", []bucket{bSource}},
		{"cat <<EOF\n$(grep -F ')' " + testFW + "/scheme.go)\nEOF", []bucket{bSource}},
		{"python3 <<'PY'\np=\"" + testFW + "/scheme.go\"\nprint(open(p).read())\nPY", []bucket{bSource}},
		{"cat <<< \"" + testFW + "/scheme.go\"", []bucket{bApp}},
		{"grep -nT Scheme " + testFW + "/scheme.go", []bucket{bSource}},
	}
	for _, tc := range cases {
		got, _ := c.touches("Bash", map[string]any{"command": tc.cmd})
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("Bash %q touches %v, want %v", tc.cmd, got, tc.want)
		}
	}
	// A child shell's cd does not move its parent.
	bash := func(id, cmd string) string {
		return `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"` + id + `","name":"Bash","input":{"command":` + strconv.Quote(cmd) + `}}]}}`
	}
	tally, err := tallyTranscript(strings.NewReader(bash("t1", "bash <<'SH'\ncd internal\nSH\ncat ../../../rastrillo/scheme.go")), c)
	if err != nil {
		t.Fatal(err)
	}
	if tally.Touched[bSource] != 1 {
		t.Errorf("touched = %v, want the parent's relative read counted as source", tally.Touched)
	}
}

// Round 10: who reads a heredoc is decided per reader (bash -e still
// runs its stdin; a redirection is not an argument), nested
// substitutions keep their own quotes, grep's -T takes no value, a
// wrapper's option value is not the command, and nesting costs linear
// work. The python3 cases are the shape 98 of the 116 heredocs in the
// baseline's live transcripts take.
func TestBashRound10(t *testing.T) {
	c := testClassifier()
	read := "\nprint(open(\"" + testFW + "/scheme.go\").read())\nPY"
	cases := []struct {
		cmd  string
		want []bucket
	}{
		{"bash -e <<'SH'\ncat " + testFW + "/scheme.go\nSH", []bucket{bSource}},
		{"bash -s -- arg <<'SH'\ncat " + testFW + "/scheme.go\nSH", []bucket{bSource}},
		{"bash script.sh <<'SH'\ncat " + testFW + "/scheme.go\nSH", []bucket{bApp}},
		{"python3 2>/dev/null <<'PY'" + read, []bucket{bSource}},
		{"python3 - <<'PY'" + read, []bucket{bSource}},
		{"python3 - <<'PY' 2>&1 | tail -5" + read, []bucket{bSource}},
		{"python3 -c 'print(1)' <<'PY'" + read, []bucket{bApp}},
		{"cat <<EOF\n$(printf '%s' \"$(grep -F \")\" " + testFW + "/scheme.go)\")\nEOF", []bucket{bSource}},
		{"grep -T Scheme " + testFW + "/scheme.go", []bucket{bSource}},
		{"env -u UNUSED_VAR python3 <<'PY'" + read, []bucket{bSource}},
		{"nice -n 5 bash <<'SH'\ncat " + testFW + "/scheme.go\nSH", []bucket{bSource}},
		{"cat < " + testFW + "/scheme.go", []bucket{bSource}},
	}
	for _, tc := range cases {
		got, _ := c.touches("Bash", map[string]any{"command": tc.cmd})
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("Bash %q touches %v, want %v", tc.cmd, got, tc.want)
		}
	}
	// Thirty nested shell heredocs: linear work, not 2^30.
	nested := ":"
	for i := 0; i < 30; i++ {
		nested = fmt.Sprintf("sh <<E%d\n$(%s\n)\nE%d", i, nested, i)
	}
	done := make(chan struct{})
	go func() {
		c.touches("Bash", map[string]any{"command": nested})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("thirty nested heredocs took more than five seconds to classify")
	}
}
