package classify

import (
	"testing"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/transcript"
)

func bash(command string) transcript.Message {
	return withTool("Bash", map[string]any{"command": command})
}

func withTool(name string, input map[string]any) transcript.Message {
	return transcript.Message{
		Tools: []transcript.Tool{{Name: name, Input: input}},
	}
}

func TestOfFollowsRuleOrder(t *testing.T) {
	where := Where{
		AppDir:    "/runs/groomer-carlos-1/app",
		LiveHosts: []string{"groomer-r1-1.bdf.oncarlos.com"},
	}
	cases := []struct {
		name    string
		message transcript.Message
		want    Stage
	}{
		{"chromium screenshot",
			bash("chromium --headless=new --screenshot=a.png http://x/"),
			Screenshots},
		{"reading a screenshot",
			withTool("Read", map[string]any{"file_path": "/s/shot.png"}),
			Screenshots},
		{"curl of the live app",
			bash("curl -s https://groomer-r1-1.bdf.oncarlos.com/"),
			LiveCheck},
		{"deploy", bash("carlos deploy --app x ./release"), Deploy},
		{"fly deploy", bash("flyctl deploy --remote-only"), Deploy},
		{"container build", bash("podman build -t x ."), Deploy},
		{"the gate", bash("make ci 2>&1 | tail"), Gates},
		{"module source",
			bash("cat ~/go/pkg/mod/amadan.net/x@v1/sessions.go"),
			FrameworkRead},
		{"skill fetch",
			bash("curl -sL https://rastrillo.org/SKILL.md"),
			FrameworkRead},
		{"web fetch of the skill",
			withTool("WebFetch",
				map[string]any{"url": "https://rastrillo.org/SKILL.md"}),
			FrameworkRead},
		{"scaffold", bash("rastrillo new groomer"), Scaffold},
		{"install", bash("go install amadan.net/x/cmd/y@latest"), Scaffold},
		{"tests", bash("go test ./... 2>&1 | tail"), BuildTest},
		{"write tool",
			withTool("Write", map[string]any{"file_path": "/a/main.go"}),
			Write},
		{"heredoc", bash("cat > main.go <<'EOF'\npackage main\nEOF"), Write},
		{"read tool",
			withTool("Read", map[string]any{"file_path": "/a/main.go"}),
			AppRead},
		{"grep", bash("grep -n func main.go"), AppRead},
		{"text only", transcript.Message{Text: "Done."}, Other},
		{"anything else", bash("date"), Other},
	}
	for _, c := range cases {
		if got := Of(c.message, where); got != c.want {
			t.Errorf("%s: Of = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestFirstWriteContext(t *testing.T) {
	read := bash("cat ~/go/pkg/mod/x/a.go")
	read.Context = 50000
	first := withTool("Write", map[string]any{"file_path": "/a/m.go"})
	first.Context = 60000
	second := withTool("Edit", map[string]any{"file_path": "/a/m.go"})
	second.Context = 70000
	run := transcript.Transcript{
		Messages: []transcript.Message{read, first, second},
	}

	if got := FirstWriteContext(run, Where{}); got != 60000 {
		t.Errorf("FirstWriteContext = %d, want 60000", got)
	}
	if got := FirstWriteContext(transcript.Transcript{}, Where{}); got != 0 {
		t.Errorf("a run with no write gave %d, want 0", got)
	}
}

func TestSummariseCountsMessagesToolsAndCacheReads(t *testing.T) {
	read := bash("ls")
	read.Tools = append(read.Tools, read.Tools[0])
	read.Context = 1000
	write := withTool("Write", map[string]any{"file_path": "/a/m.go"})
	write.Context = 3000
	run := transcript.Transcript{
		Messages: []transcript.Message{read, write},
	}

	tallies := Summarise(run, Where{})

	if got := tallies[AppRead]; got != (Tally{1, 2, 1000}) {
		t.Errorf("app-read = %+v, want one message, two tools, 1000", got)
	}
	if got := tallies[Write]; got != (Tally{1, 1, 3000}) {
		t.Errorf("write = %+v", got)
	}
}

func TestFirstWriteContextCountsOnlyWritesIntoTheApp(t *testing.T) {
	where := Where{AppDir: "/runs/a/app"}
	elsewhere := withTool("Write", map[string]any{"file_path": "/tmp/helper.py"})
	elsewhere.Context = 40000
	later := bash("ls")
	later.Tools = append(later.Tools, transcript.Tool{Name: "Edit",
		Input: map[string]any{"file_path": "/runs/a/app/main.go"}})
	later.Context = 55000
	run := transcript.Transcript{
		Messages: []transcript.Message{elsewhere, later},
	}

	if got := FirstWriteContext(run, where); got != 55000 {
		t.Errorf("FirstWriteContext = %d, want 55000: the scratch write "+
			"is not the app, and the second tool of a message counts", got)
	}
}
