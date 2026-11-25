// Package comprehend asks what an app does and checks the answers: the
// questions per brief, an answer key per app, and (later) the readers.
package comprehend

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Kind is the shape of a question's answer, so it can be checked
// without a model's judgement.
type Kind string

const (
	Status Kind = "status" // an HTTP status code
	Choice Kind = "choice" // one of the question's Choices
	YesNo  Kind = "yesno"
	Route  Kind = "route" // a path, with its method if it matters
	Word   Kind = "word"  // a name: a field, a role, a username
)

var kinds = []Kind{Status, Choice, YesNo, Route, Word}

// Question is one thing to ask about an app. A behavioural question is
// about what a request gets, so its key must come from making it.
type Question struct {
	ID, Ask     string
	Kind        Kind
	Choices     []string
	Behavioural bool
}

// LoadQuestions reads briefDir/questions.txt: blocks of "field: value"
// lines, one block per question, separated by a blank line.
func LoadQuestions(briefDir string) ([]Question, error) {
	path := filepath.Join(briefDir, "questions.txt")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var questions []Question
	for _, block := range strings.Split(string(raw), "\n\n") {
		if strings.TrimSpace(block) == "" {
			continue
		}
		question, err := parseQuestion(block)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		for _, earlier := range questions {
			if earlier.ID == question.ID {
				return nil, fmt.Errorf("%s: %s is asked twice",
					path, question.ID)
			}
		}
		questions = append(questions, question)
	}
	if len(questions) == 0 {
		return nil, fmt.Errorf("%s holds no questions", path)
	}
	return questions, nil
}

func parseQuestion(block string) (Question, error) {
	var question Question
	for _, line := range strings.Split(strings.TrimSpace(block), "\n") {
		field, value, found := strings.Cut(line, ":")
		value = strings.TrimSpace(value)
		if !found {
			return question, fmt.Errorf("%q is not a field: value line", line)
		}
		switch strings.TrimSpace(field) {
		case "id":
			question.ID = value
		case "kind":
			question.Kind = Kind(value)
		case "ask":
			question.Ask = value
		case "choices":
			for _, choice := range strings.Split(value, "|") {
				question.Choices = append(question.Choices,
					strings.TrimSpace(choice))
			}
		case "behavioural":
			question.Behavioural = value == "yes"
		default:
			return question, fmt.Errorf("%q: unknown field %q", line, field)
		}
	}
	return question, check(question)
}

func check(question Question) error {
	id := question.ID
	switch {
	case id == "":
		return errors.New("a question has no id")
	case question.Ask == "":
		return fmt.Errorf("%s has no ask", id)
	case !slices.Contains(kinds, question.Kind):
		return fmt.Errorf("%s: kind %q is not one of %v",
			id, question.Kind, kinds)
	case question.Kind == Choice && len(question.Choices) < 2:
		return fmt.Errorf("%s is a choice with fewer than two choices", id)
	case question.Kind != Choice && len(question.Choices) > 0:
		return fmt.Errorf("%s has choices but is not a choice", id)
	}
	return nil
}
