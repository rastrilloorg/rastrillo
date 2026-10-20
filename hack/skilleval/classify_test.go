package main

import (
	"strings"
	"testing"
)

const (
	testFW  = "/work/v/rastrillo"
	testMod = "/home/u/go/pkg/mod/amadan.net/rastrillo/rastrillo@v0.30.0"
	testApp = "/work/v/expenses/1/app"
)

func testClassifier() classifier {
	return classifier{roots: []string{testFW, testMod}, app: testApp}
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
	if !tally.Finished {
		t.Error("a success result event must mark the run finished")
	}
}

func TestTallyWithoutResultIsUnfinished(t *testing.T) {
	tally, err := tallyTranscript(strings.NewReader(`{"type":"system","subtype":"init"}`), testClassifier())
	if err != nil {
		t.Fatal(err)
	}
	if tally.Finished {
		t.Error("a transcript cut off before its result event must not read as finished")
	}
}
