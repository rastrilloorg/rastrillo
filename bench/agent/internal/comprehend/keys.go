package comprehend

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/cleanenv"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/runner"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/transcript"
)

// KeyModel makes the answer keys.
const KeyModel = "claude-opus-5-5"

// NotBuilt is the answer of a Missing key.
const NotBuilt = "not built"

// Key is the settled answer to one question for one app. Missing means
// the app does not do what the question asks. SourceHash is the app's
// SourceHash when the key was made, so a key is never used on other code.
type Key struct {
	ID, Answer, Evidence, SourceHash string
	Settled, Missing                 bool
}

// KeyFile is keys.json: one app's keys and what made them. Keys made
// from other questions or by another prompt are not today's keys.
type KeyFile struct {
	Round, Run, Brief, RastrilloVersion, Model string
	QuestionsHash, PromptHash                  string
	Made                                       time.Time
	Keys                                       []Key
}

// QuestionsHash fingerprints briefDir/questions.txt.
func QuestionsHash(briefDir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(briefDir, "questions.txt"))
	return fmt.Sprintf("%x", sha256.Sum256(raw)), err
}

// PromptHash fingerprints the key-maker's instructions.
func PromptHash() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(keyInstructions)))
}

// KeySetup is one app to key. Scratch is an empty directory the caller
// removes; the key-maker's transcript is left there as keymaker.jsonl.
type KeySetup struct {
	AppDir, RastrilloDir, Token, BenchRoot, Scratch string
}

// MakeKeys has the key-maker answer every question for one app, on a
// copy of it, in the bench's clean environment. Questions it could not
// settle come back unsettled, never dropped.
func MakeKeys(
	ctx context.Context, setup KeySetup, questions []Question,
) ([]Key, error) {
	app, err := CopyApp(setup)
	if err != nil {
		return nil, err
	}
	before, err := SourceHash(app)
	if err != nil {
		return nil, err
	}
	_, env, err := cleanSession(setup)
	if err != nil {
		return nil, err
	}
	builds := buildsOffline(ctx, app, env)
	run, err := Ask(ctx, setup, app, keyPrompt(setup.RastrilloDir, questions),
		"Read,Grep,Glob,Bash", "keymaker")
	if err != nil {
		return nil, err
	}
	after, err := SourceHash(app)
	if err != nil {
		return nil, err
	}
	answered, err := keysFrom(run)
	if err != nil {
		return nil, err
	}
	return settle(questions, answered, session{before: before, after: after,
		builds: builds, calls: bashCalls(run)}), nil
}

// CopyApp copies the app into the scratch, as app/, so that a session
// never works on a kept run, and returns the copy.
func CopyApp(setup KeySetup) (string, error) {
	app := filepath.Join(setup.Scratch, "app")
	return app, copyTree(setup.AppDir, app, nil)
}

// Ask runs one claude -p session with KeyModel in dir, on the bench
// token and in the bench's clean environment, allowed only tools. Its
// transcript is kept in the scratch as <role>.jsonl, and role names it
// in errors. A session that did not run clean and finish is a harness
// failure, as is one whose answer is an error.
func Ask(
	ctx context.Context, setup KeySetup, dir, prompt, tools, role string,
) (transcript.Transcript, error) {
	settings, env, err := cleanSession(setup)
	if err != nil {
		return transcript.Transcript{}, err
	}
	command := exec.CommandContext(ctx, "claude",
		"-p", prompt,
		"--model", KeyModel,
		"--output-format", "stream-json", "--verbose",
		"--dangerously-skip-permissions", "--disable-slash-commands",
		"--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`,
		"--settings", settings,
		"--tools", tools,
	)
	command.Dir, command.Env = dir, env
	return ask(command, setup.Scratch, role)
}

// cleanSession is a session's settings and environment, cleaned as
// a bench run's are: the committed settings, no operator environment.
// Go may not touch the network or rewrite go.mod: every kept app builds
// from the module cache as it is. Its TMPDIR is in the scratch, so
// whatever it builds or leaves there goes when the scratch does.
func cleanSession(setup KeySetup) (string, []string, error) {
	config := filepath.Join(setup.Scratch, "config")
	temporary := filepath.Join(setup.Scratch, "tmp")
	for _, dir := range []string{config, temporary} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", nil, err
		}
	}
	settings, err := cleanenv.Config(setup.BenchRoot, config, nil, nil)
	if err != nil {
		return "", nil, err
	}
	extra := map[string]string{
		"GOFLAGS": "-mod=readonly -buildvcs=false",
		"GOPROXY": "off",
		"TMPDIR":  temporary,
	}
	// Empty, it would blank a token the environment already carries.
	if setup.Token != "" {
		extra["CLAUDE_CODE_OAUTH_TOKEN"] = setup.Token
	}
	env, err := cleanenv.Env(os.Environ(), config, extra)
	return settings, env, err
}

// buildsOffline is whether the app builds as the key-maker would build
// it. An app that does not cannot have answered a request.
func buildsOffline(ctx context.Context, app string, env []string) bool {
	build := exec.CommandContext(ctx, "go", "build", "./...")
	build.Dir, build.Env = app, env
	return build.Run() == nil
}

// ask runs the session, keeping its transcript, and stops everything
// it started (a server it forgot) when it ends, or when ctx does.
func ask(
	command *exec.Cmd, scratch, role string,
) (transcript.Transcript, error) {
	out, err := os.Create(filepath.Join(scratch, role+".jsonl"))
	if err != nil {
		return transcript.Transcript{}, err
	}
	defer out.Close()
	var stderr bytes.Buffer
	command.Stdout, command.Stderr = out, &stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = 10 * time.Second
	exitErr := command.Run()
	if command.Process != nil {
		syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}

	if _, err := out.Seek(0, io.SeekStart); err != nil {
		return transcript.Transcript{}, err
	}
	run, err := transcript.Parse(out)
	if err != nil {
		return run, err
	}
	return run, verdict(run, role, exitErr, stderr.String())
}

// verdict is the harness's verdict on a session: it must have run clean
// and given a final answer.
func verdict(
	run transcript.Transcript, role string, exitErr error, stderr string,
) error {
	tail := lastLines(stderr, 20)
	switch {
	case run.Init.Version == "":
		return fmt.Errorf("%w: the %s wrote no init record (%v): %s",
			runner.ErrHarness, role, exitErr, tail)
	case run.Result == nil:
		return fmt.Errorf("%w: the %s did not finish (%v): %s",
			runner.ErrHarness, role, exitErr, tail)
	case run.Result.APIError != "":
		return fmt.Errorf("%w: the %s hit API error %s",
			runner.ErrHarness, role, run.Result.APIError)
	case run.Result.IsError:
		return fmt.Errorf("%w: the %s ended in error: %s",
			runner.ErrHarness, role, run.Result.Subtype)
	}
	return runner.CheckClean(run.Init, "", nil)
}

func lastLines(text string, count int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	return strings.Join(lines[max(0, len(lines)-count):], "\n")
}

// call is one Bash command the key-maker ran, and what it printed.
type call struct{ command, result string }

func bashCalls(run transcript.Transcript) []call {
	var calls []call
	for _, message := range run.Messages {
		for _, tool := range message.Tools {
			if command, isText := tool.Input["command"].(string); isText &&
				tool.Name == "Bash" {
				calls = append(calls, call{command, tool.Result})
			}
		}
	}
	return calls
}

// keysFrom finds the keys in the key-maker's last answer or, when that
// holds none, its latest message that does: a key-maker that left work
// in the background answers again when the work ends, about that.
func keysFrom(run transcript.Transcript) ([]Key, error) {
	keys, err := parseKeys(run.Result.Text)
	for i := len(run.Messages) - 1; err != nil && i >= 0; i-- {
		if found, failed := parseKeys(run.Messages[i].Text); failed == nil {
			return found, nil
		}
	}
	return keys, err
}

// parseKeys finds the JSON array of keys in the key-maker's answer,
// whatever prose or code fence is around it.
func parseKeys(answer string) ([]Key, error) {
	for start := strings.Index(answer, "["); start >= 0; {
		var keys []Key
		decoder := json.NewDecoder(strings.NewReader(answer[start:]))
		if decoder.Decode(&keys) == nil && len(keys) > 0 && keys[0].ID != "" {
			return keys, nil
		}
		next := strings.Index(answer[start+1:], "[")
		if next < 0 {
			break
		}
		start += 1 + next
	}
	return nil, fmt.Errorf("the key-maker's answer holds no JSON array "+
		"of keys: %.300q", answer)
}

// session is what settling a key needs to know of the key-maker's run.
type session struct {
	before, after string // the copy's SourceHash, before and after
	builds        bool   // whether the copy built before it began
	calls         []call // every Bash command it ran, and its output
}

// settle gives every question a key, in the questions' order, and
// unsettles any the bench cannot trust, saying why in its Evidence.
// An unsettled key is never Missing: "not built" needs a settled key.
func settle(questions []Question, answered []Key, run session) []Key {
	byID := map[string]Key{}
	for _, key := range answered {
		if _, seen := byID[key.ID]; !seen {
			byID[key.ID] = key
		}
	}
	keys := make([]Key, 0, len(questions))
	for _, question := range questions {
		key, found := byID[question.ID]
		if !found {
			key = Key{ID: question.ID,
				Evidence: "bench: the key-maker gave no answer"}
		}
		key.SourceHash = run.before
		reason := distrust(question, &key, run)
		if key.Settled && reason != "" {
			key.Settled = false
			key.Evidence = "bench: unsettled: " + reason + "\n" + key.Evidence
		}
		if !key.Settled {
			key.Missing = false
		} else if key.Missing {
			key.Answer = NotBuilt
		}
		keys = append(keys, key)
	}
	return keys
}

// distrust says why a key cannot be trusted, or "". A choice answer is
// set to the choice's own words.
func distrust(question Question, key *Key, run session) string {
	switch {
	case run.before != run.after:
		return "the app's source changed while the key was made"
	case question.Behavioural && !run.builds:
		return "the app does not build, so no request could settle it"
	case !key.Missing && strings.TrimSpace(key.Answer) == "":
		return "there is no answer"
	case question.Behavioural && !ranAndShown(key.Evidence, run.calls):
		return unshown
	case question.Kind == Choice && !key.Missing:
		index := slices.IndexFunc(question.Choices, func(choice string) bool {
			return strings.EqualFold(choice, strings.TrimSpace(key.Answer))
		})
		if index < 0 {
			return "the answer is not one of the choices"
		}
		key.Answer = question.Choices[index]
	}
	return ""
}

const unshown = "a behavioural key needs the request it made " +
	"(a curl to 127.0.0.1 or localhost, quoted whole) and output " +
	"that request really printed"

// Recheck unsettles any settled behavioural key that run, the
// key-maker's transcript, does not bear out, as settle would today:
// keys made before the bench checked a key's output against what its
// request printed are held to that check when they are read.
func Recheck(
	questions []Question, keys []Key, run transcript.Transcript,
) []Key {
	calls := bashCalls(run)
	checked := slices.Clone(keys)
	for i, key := range checked {
		index := slices.IndexFunc(questions, func(question Question) bool {
			return question.ID == key.ID
		})
		if index < 0 || !questions[index].Behavioural || !key.Settled ||
			ranAndShown(key.Evidence, calls) {
			continue
		}
		key.Settled, key.Missing = false, false
		key.Evidence = "bench: unsettled: " + unshown + "\n" + key.Evidence
		checked[i] = key
	}
	return checked
}

// ranAndShown is true when evidence quotes, on a line starting "$ ", a
// request the key-maker really made, followed by output that request's
// call printed. A quote must be a whole line of a command it ran, or a
// part of one between ; and &&, and must name curl or wget and the
// local address.
func ranAndShown(evidence string, calls []call) bool {
	lines := strings.Split(evidence, "\n")
	for i, line := range lines {
		command, quoted := strings.CutPrefix(strings.TrimSpace(line), "$ ")
		command = squeeze(command)
		if !quoted || !isLocalRequest(command) {
			continue
		}
		output := quotedOutput(lines[i+1:])
		for _, call := range calls {
			if segments([]string{call.command})[command] &&
				printedAny(call.result, output) {
				return true
			}
		}
	}
	return false
}

// quotedOutput is the lines after a quoted command, up to the next.
func quotedOutput(lines []string) []string {
	var output []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "$ ") {
			break
		}
		if line != "" {
			output = append(output, line)
		}
	}
	return output
}

// printedAny is true when some line of output is in result. The key-
// maker may add a note after the output, so one line is enough; a line
// that leaves some out with "..." matches on the parts it kept.
func printedAny(result string, output []string) bool {
	printed := squeeze(result)
	for _, line := range output {
		var parts []string
		for _, part := range strings.Split(
			strings.ReplaceAll(line, "…", "..."), "...") {
			if part = squeeze(part); part != "" {
				parts = append(parts, part)
			}
		}
		missing := func(part string) bool {
			return !strings.Contains(printed, part)
		}
		if len(parts) > 0 && !slices.ContainsFunc(parts, missing) {
			return true
		}
	}
	return false
}

// segments are the lines of the commands run, and their parts between
// ; and &&, whitespace squeezed. A heredoc's body is text written to a
// file, not run, so it is left out.
func segments(commands []string) map[string]bool {
	ran := map[string]bool{}
	for _, command := range commands {
		for _, line := range withoutHeredocs(command) {
			ran[squeeze(line)] = true
			for _, part := range strings.FieldsFunc(
				strings.ReplaceAll(line, "&&", ";"),
				func(r rune) bool { return r == ';' },
			) {
				ran[squeeze(part)] = true
			}
		}
	}
	delete(ran, "")
	return ran
}

var heredoc = regexp.MustCompile(`<<-?\s*['"]?([A-Za-z_][A-Za-z0-9_]*)['"]?`)

func withoutHeredocs(command string) []string {
	var lines []string
	end := ""
	for _, line := range strings.Split(command, "\n") {
		if end != "" {
			if strings.TrimSpace(line) == end {
				end = ""
			}
			continue
		}
		lines = append(lines, line)
		if found := heredoc.FindStringSubmatch(line); found != nil {
			end = found[1]
		}
	}
	return lines
}

var localRequest = regexp.MustCompile(
	`\b(curl|wget)\b.*\b(127\.0\.0\.1|localhost)\b`)

func isLocalRequest(command string) bool {
	return localRequest.MatchString(command)
}

func squeeze(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// sourceExtensions are the files SourceHash covers: what a reader
// reads. A database, a built binary or a log the app writes as it runs
// are not source, so running the app does not void its keys.
var sourceExtensions = []string{".go", ".mod", ".sum", ".work", ".html",
	".tmpl", ".gohtml", ".css", ".js", ".mjs", ".sql", ".json", ".toml",
	".yaml", ".yml", ".md", ".svg"}

// SourceHash fingerprints the app's source files, names and contents,
// outside any .git directory.
func SourceHash(dir string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(dir, func(
		path string, entry fs.DirEntry, err error,
	) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if !entry.Type().IsRegular() || !(entry.Name() == "Makefile" ||
			slices.Contains(sourceExtensions, filepath.Ext(path))) {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(dir, path)
		fmt.Fprintf(hash, "%s\x00%x\n", relative, sha256.Sum256(content))
		return nil
	})
	return fmt.Sprintf("%x", hash.Sum(nil)), err
}

// copyTree copies files, directories and symlinks, keeping modes,
// leaving out any directory skip is true of. skip may be nil.
func copyTree(from, to string, skip func(fs.DirEntry) bool) error {
	return filepath.WalkDir(from, func(
		path string, entry fs.DirEntry, err error,
	) error {
		if err != nil {
			return err
		}
		if skip != nil && entry.IsDir() && path != from && skip(entry) {
			return filepath.SkipDir
		}
		relative, _ := filepath.Rel(from, path)
		target := filepath.Join(to, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case entry.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case entry.Type().IsRegular():
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(target, content, info.Mode().Perm())
		}
		return nil
	})
}

// keyPrompt asks for every question at once: one session per app, so
// the app is built and started once.
func keyPrompt(rastrilloDir string, questions []Question) string {
	var asked strings.Builder
	for _, question := range questions {
		fmt.Fprintf(&asked, "\n- id: %s\n  kind: %s\n  question: %s\n",
			question.ID, question.Kind, question.Ask)
		if question.Kind == Choice {
			fmt.Fprintf(&asked, "  choices: %s\n",
				strings.Join(question.Choices, " | "))
		}
		if question.Behavioural {
			asked.WriteString("  behavioural: answer it by making the " +
				"request to the running app\n")
		}
	}
	return fmt.Sprintf(keyInstructions, rastrilloDir, asked.String())
}

const keyInstructions = `This directory is a web app another agent built. ` +
	`You are writing the answer key for questions about what this app ` +
	`does: not what it should do, what this code does. Readers will be ` +
	`graded against your answers, so settle each one from the code and, ` +
	`where it says behavioural, by running the app and making the request.

The app uses Rastrillo, a Go web framework; its source at the version ` +
	`this app uses is at %s (read only).

How to work:
- Read the code, and run its tests if that helps.
- To run the app, build it to a file under $TMPDIR and start it on ` +
	`127.0.0.1 with a port you pick at random between 20000 and 60000, ` +
	`and its database under $TMPDIR (Rastrillo apps take -addr ` +
	`127.0.0.1:<port> and -db <path>). Never listen on any other address.
- Do not change, add or delete any file in this directory. A changed ` +
	`source here voids every key.
- Start the app, make your requests and stop it within one shell ` +
	`call: a background task cannot reach a server another call ` +
	`started. Stop every process you started before you answer.

Answer each question in the form its kind asks for:
- status: the numeric HTTP status code, as the response gives it.
- yesno: yes or no.
- route: the path as the app registers it, with {name} for a ` +
	`parameter, for example /bookings/{id}.
- word: the exact name or word.
- choice: one of the choices, word for word.

If the app does not do what a question asks at all, set "missing" to ` +
	`true and answer "not built". If you cannot settle an answer (the ` +
	`app will not build or start, or the code allows two answers), set ` +
	`"settled" to false and say why in the evidence. Never guess.

Evidence: for a behavioural question, a line starting "$ " with the ` +
	`curl that made the request, naming http://127.0.0.1:<port> or ` +
	`localhost in full (no variable or helper script for the address), ` +
	`exactly as it appears as a whole line of one of your shell calls ` +
	`(not a line of a script you wrote to a file and ran), ` +
	`then the lines of its output that settle the answer. ` +
	`Copy the command whole into every key that uses it: never "same as ` +
	`above", never "...". The bench checks it against what you ran. ` +
	`For any other question, the file and line numbers and the code that ` +
	`settle it.

The questions:
%s
End with a JSON array and nothing after it, one object per question:
[{"id": "...", "answer": "...", "evidence": "...", "settled": true, ` +
	`"missing": false}]
`
