package comprehend

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/codex"
)

// The reader: Astra, at high effort, as the session rule has an Opus
// session's work reviewed.
const (
	ReaderModel  = "gpt-6-astra"
	ReaderEffort = "high"
)

// Stage makes a reader's directory in scratch: app/, a copy of the app
// without its git history, and rastrillo/, a copy of the framework at
// the app's version. Nothing else goes in it: no keys, no transcripts,
// no other app.
func Stage(appDir, rastrilloDir, scratch string) error {
	err := copyTree(appDir, filepath.Join(scratch, "app"),
		func(entry fs.DirEntry) bool { return entry.Name() == ".git" })
	if err != nil {
		return err
	}
	return copyTree(rastrilloDir, filepath.Join(scratch, "rastrillo"), nil)
}

// Read asks one question of the app staged in exec.Dir. A reading that
// spent more than budget tokens is marked OverBudget and grades wrong.
// The error is the harness's: codex failing, never a wrong answer.
func Read(
	ctx context.Context, exec codex.Exec, question Question, budget int,
) (Reading, error) {
	reply, err := exec.Run(ctx, readerPrompt(question))
	if err != nil {
		return Reading{}, fmt.Errorf("%s: %w", question.ID, err)
	}
	tokens := reply.InputTokens + reply.OutputTokens
	return Reading{QuestionID: question.ID, Answer: answerFrom(reply.Text),
		OverBudget: tokens > budget, Tokens: tokens,
		Commands: reply.Commands}, nil
}

// answerFrom is the "answer" field of the last JSON object in the
// reply that has one, whatever prose or code fence is around it. A
// reply with none is its own answer, trimmed, graded as given.
func answerFrom(reply string) string {
	for end := len(reply); end > 0; {
		start := strings.LastIndex(reply[:end], "{")
		if start < 0 {
			break
		}
		var object struct {
			Answer *string `json:"answer"`
		}
		decoder := json.NewDecoder(strings.NewReader(reply[start:]))
		if decoder.Decode(&object) == nil && object.Answer != nil {
			return strings.TrimSpace(*object.Answer)
		}
		end = start
	}
	return strings.TrimSpace(reply)
}

// ReaderPromptHash fingerprints the reader's instructions, so readings
// made by other instructions are not mixed with today's.
func ReaderPromptHash() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(readerInstructions)))
}

var answerForms = map[Kind]string{
	Status: "the numeric HTTP status code, for example 404.",
	YesNo:  "yes or no.",
	Route: "the path as the app registers it, with {name} for a " +
		"parameter, for example /bookings/{id}.",
	Word:   "the exact name or word.",
	Choice: "one of these choices, word for word: %s",
}

func readerPrompt(question Question) string {
	form := answerForms[question.Kind]
	if question.Kind == Choice {
		form = fmt.Sprintf(form, strings.Join(question.Choices, " | "))
	}
	return fmt.Sprintf(readerInstructions, question.Ask, form)
}

const readerInstructions = `/work/app is a web app another agent ` +
	`built. /work/rastrillo is the source of Rastrillo, the Go web ` +
	`framework it uses, at the version the app uses. Both are read only. ` +
	`You cannot run the app or reach the network: answer from the code, ` +
	`about what this code does, not what it should do.

Question: %s

Answer with %s
If the app does not do what the question asks at all, answer ` +
	`"not built".

Reply with only this JSON object and nothing else: {"answer": "..."}
`
