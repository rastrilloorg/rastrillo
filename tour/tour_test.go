package tour

import (
	"slices"
	"strings"
	"testing"
)

func TestTheChatTourLoadsWithEveryStepNumbered(t *testing.T) {
	tour, err := Load("chat")
	if err != nil {
		t.Fatal(err)
	}
	if tour.App != "chat" || tour.Title == "" || len(tour.Steps) == 0 {
		t.Fatalf("Load(chat) = %+v", tour)
	}
	for index, step := range tour.Steps {
		if step.Number != index+1 || step.Lesson == "" || step.Check == "" {
			t.Errorf("step %d (%s) lacks a number, lesson or check",
				index+1, step.Slug)
		}
	}
}

// A replayed command is one the reader was shown, so the tour never runs
// something its page does not tell them to.
func TestEveryReplayedCommandIsOneTheReaderIsShown(t *testing.T) {
	tour, err := Load("chat")
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range tour.Steps {
		for _, command := range step.Run {
			if !slices.Contains(step.Commands, command) {
				t.Errorf("step %d replays %q, which it does not show",
					step.Number, command)
			}
		}
	}
}

func TestChangedLinesMarksWhatIsNew(t *testing.T) {
	old := "package chat\n\nfunc a() {}\n"
	updated := "package chat\n\nimport \"time\"\n\nfunc a() {}\n\nfunc b() {}\n"
	got := ChangedLines(old, updated)
	want := [][2]int{{3, 4}, {6, 7}}
	if !slices.Equal(got, want) {
		t.Errorf("ChangedLines = %v, want %v", got, want)
	}
	if got := ChangedLines("", "a\nb\n"); !slices.Equal(got,
		[][2]int{{1, 2}}) {
		t.Errorf("a new file's lines = %v, want all of them", got)
	}
	if got := ChangedLines("same\n", "same\n"); len(got) != 0 {
		t.Errorf("an unchanged file's lines = %v, want none", got)
	}
}

func TestAStepThatNamesAMissingFileIsRefused(t *testing.T) {
	tour := Tour{Slug: "chat", Steps: []Step{{Number: 1, Title: "x",
		Scaffold: true, Files: []string{"no/such/file.go"}}}}
	if err := tour.check(); err == nil ||
		!strings.Contains(err.Error(), "no/such/file.go") {
		t.Errorf("check = %v, want the missing file named", err)
	}
}
