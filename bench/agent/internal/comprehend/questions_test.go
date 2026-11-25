package comprehend

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeQuestions(t *testing.T, text string) string {
	t.Helper()
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "questions.txt"), []byte(text), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestQuestionsParseEveryKind(t *testing.T) {
	dir := writeQuestions(t, `id: empty-date
kind: choice
behavioural: yes
ask: What does a visitor see after submitting the booking form with the date left empty?
choices: the form again with a message | an error page | a confirmation | nothing changes

id: signed-out
kind: status
behavioural: yes
ask: What status does a visitor get?

id: own-cancel
kind: yesno
ask: Can a customer cancel?

id: signin
kind: route
ask: Where does the owner sign in?

id: dog
kind: word
ask: Which field holds the dog's name?
`)

	questions, err := LoadQuestions(dir)
	if err != nil {
		t.Fatal(err)
	}

	var kinds []Kind
	for _, question := range questions {
		kinds = append(kinds, question.Kind)
	}
	want := []Kind{Choice, Status, YesNo, Route, Word}
	if !slices.Equal(kinds, want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	first := questions[0]
	if first.ID != "empty-date" || !first.Behavioural ||
		!strings.HasPrefix(first.Ask, "What does a visitor see") {
		t.Errorf("first question = %+v", first)
	}
	wantChoices := []string{"the form again with a message", "an error page",
		"a confirmation", "nothing changes"}
	if !slices.Equal(first.Choices, wantChoices) {
		t.Errorf("choices = %q, want %q", first.Choices, wantChoices)
	}
	if questions[2].Behavioural {
		t.Error("a question with no behavioural line is behavioural")
	}
}

func TestAChoiceQuestionNeedsChoices(t *testing.T) {
	dir := writeQuestions(t, `id: fine
kind: yesno
ask: Is it?

id: who-deletes
kind: choice
ask: Who can delete a listing?
`)

	_, err := LoadQuestions(dir)
	if err == nil || !strings.Contains(err.Error(), "who-deletes") {
		t.Errorf("err = %v, want one naming who-deletes", err)
	}
}

func TestQuestionsRefuseWhatTheyCannotRead(t *testing.T) {
	for name, text := range map[string]string{
		"unknown kind":  "id: a\nkind: colour\nask: Which?\n",
		"no ask":        "id: a\nkind: yesno\n",
		"unknown field": "id: a\nkind: yesno\nask: Is it?\nhint: no\n",
		"twice the id":  "id: a\nkind: yesno\nask: Is it?\n\nid: a\nkind: word\nask: What?\n",
		"no questions":  "\n\n",
		"choices on a yes or no": "id: a\nkind: yesno\nask: Is it?\n" +
			"choices: yes | no\n",
	} {
		if _, err := LoadQuestions(writeQuestions(t, text)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// The groomer questions are the ones the kept runs are keyed against;
// they must load, and at least four must be about what a request gets.
func TestTheGroomerQuestionsLoad(t *testing.T) {
	questions, err := LoadQuestions("../../briefs/groomer")
	if err != nil {
		t.Fatal(err)
	}
	behavioural := 0
	for _, question := range questions {
		if question.Behavioural {
			behavioural++
		}
	}
	if len(questions) != 10 || behavioural < 4 {
		t.Errorf("%d questions, %d behavioural; want 10, at least 4",
			len(questions), behavioural)
	}
}
