// Package classify names what each message of a run was for, so a
// round can say where the turns and the context went.
package classify

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/transcript"
)

type Stage string

const (
	Screenshots   Stage = "screenshots"
	LiveCheck     Stage = "live-check"
	Deploy        Stage = "deploy"
	Gates         Stage = "gates"
	FrameworkRead Stage = "framework-read"
	Scaffold      Stage = "scaffold"
	BuildTest     Stage = "build-test"
	Write         Stage = "write"
	AppRead       Stage = "app-read"
	Other         Stage = "other"
)

type Where struct {
	AppDir    string   // the run's app directory
	LiveHosts []string // hosts the mapping step reported
}

// Tally is one stage's share of a run. Context sums each message's
// whole context, which is what that turn paid to re-read.
type Tally struct{ Messages, ToolCalls, Context int }

type rule struct {
	stage   Stage
	pattern *regexp.Regexp
}

// The order is the point: a curl of the live app is a live check even
// though it is also a fetch, and a heredoc into a file is a write even
// though it starts with cat. The 2026-10-30 classifier ran gates before
// screenshots and counted every chromium screenshot as a gate run.
var rules = []rule{
	{Screenshots, regexp.MustCompile(
		`--screenshot|headless|chrome-headless|\.png\b`)},
	{Deploy, regexp.MustCompile(
		`\b(carlos|flyctl|fly|railway|render|vercel|netlify|wrangler|` +
			`gcloud|heroku)\s|\b(podman|docker) (build|push)\b`)},
	{Gates, regexp.MustCompile(
		`\bmake ci\b|staticcheck|govulncheck|gitleaks|lighthouse|\baxe\b`)},
	{FrameworkRead, regexp.MustCompile(
		`/pkg/mod/|SKILL\.md|rastrillo\.org|carlosframework|` +
			`raw\.githubusercontent\.com`)},
	{Scaffold, regexp.MustCompile(
		`\brastrillo new\b|\bnpm (init|install)\b|\bgo mod init\b|` +
			`\bgo (get|install)\b`)},
	{BuildTest, regexp.MustCompile(
		`\bgo (test|build|vet)\b|\bgofmt\b|\bnpm test\b|\bnode --test\b`)},
	{Write, writePattern},
	{AppRead, regexp.MustCompile(
		`^\s*(cd [^;&]*(&&|;)\s*)?(cat|sed -n|grep|ls|find|head|tail)\b`)},
}

var writePattern = regexp.MustCompile(`\bcat >|\bsed -i\b|\bpython3 - <<`)

var urlPattern = regexp.MustCompile(`https?://[^\s'"<>]+`)

// Of names a message by its first tool call; a message with no tool is
// the agent talking, and counts as other.
func Of(message transcript.Message, where Where) Stage {
	if len(message.Tools) == 0 {
		return Other
	}
	tool := message.Tools[0]
	subject := subjectOf(tool)
	if rules[0].pattern.MatchString(subject) {
		return Screenshots
	}
	if touchesLiveHost(subject, where.LiveHosts) {
		return LiveCheck
	}
	// A file tool is a write whatever it writes, even a SKILL.md.
	switch tool.Name {
	case "Write", "Edit", "MultiEdit", "NotebookEdit":
		return Write
	}
	for _, r := range rules[1:] {
		if r.pattern.MatchString(subject) {
			return r.stage
		}
	}
	switch tool.Name {
	case "Read", "Grep", "Glob":
		return AppRead
	}
	return Other
}

// subjectOf is the text a rule matches: the command, path or URL the
// tool acted on, or its whole input for a tool with none of those.
func subjectOf(tool transcript.Tool) string {
	for _, key := range []string{"command", "file_path", "url", "pattern"} {
		if text, ok := tool.Input[key].(string); ok {
			return text
		}
	}
	encoded, _ := json.Marshal(tool.Input)
	return string(encoded)
}

func touchesLiveHost(subject string, hosts []string) bool {
	for _, raw := range urlPattern.FindAllString(subject, -1) {
		parsed, err := url.Parse(raw)
		if err != nil {
			continue
		}
		for _, host := range hosts {
			if strings.EqualFold(parsed.Hostname(), host) {
				return true
			}
		}
	}
	return false
}

func Summarise(
	run transcript.Transcript, where Where,
) map[Stage]Tally {
	tallies := map[Stage]Tally{}
	for _, message := range run.Messages {
		stage := Of(message, where)
		tally := tallies[stage]
		tally.Messages++
		tally.ToolCalls += len(message.Tools)
		tally.Context += message.Context
		tallies[stage] = tally
	}
	return tallies
}

// FirstWriteContext is the context the agent carried when it first
// wrote into the app: how much it read before it started building. A
// write elsewhere (a scratch script) is not building the app, and any
// tool of a message counts, not only the one that names its stage.
func FirstWriteContext(run transcript.Transcript, where Where) int {
	for _, message := range run.Messages {
		for _, tool := range message.Tools {
			if writesIntoApp(tool, where.AppDir) {
				return message.Context
			}
		}
	}
	return 0
}

func writesIntoApp(tool transcript.Tool, appDir string) bool {
	switch tool.Name {
	case "Write", "Edit", "MultiEdit", "NotebookEdit":
		path, _ := tool.Input["file_path"].(string)
		return appDir == "" || !strings.HasPrefix(path, "/") ||
			strings.HasPrefix(path, strings.TrimSuffix(appDir, "/")+"/")
	case "Bash":
		// A shell write runs in the app directory unless it says
		// otherwise, which a regex cannot follow; it counts.
		command, _ := tool.Input["command"].(string)
		return writePattern.MatchString(command)
	}
	return false
}
