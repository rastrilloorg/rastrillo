package rastrillo

import (
	"os"
	"strings"
	"testing"
	"time"
)

// cssFloor is the engine floor rastrillo's own stylesheets impose on
// every app that ships them. It is derived, not chosen: take the
// features tokens.css and the three themes actually use, and keep the
// highest engine version among them. light-dark() sets it everywhere
// except Firefox, where :has() does. docs/site/templates.md shows that
// derivation; SKILL.md §7 states the result, because an agent that
// cannot find the floor invents one — an agent building on rastrillo
// refused oklch() as "a 2023-era browser feature" and reached for hex
// twins, which protect nobody, since an engine too old for oklch()
// dropped the whole light-dark() palette several declarations earlier.
//
// Two files stating one fact can drift into two facts.
// TestCSSFloorIsStatedTheSameInBothFiles holds them equal. Both hard-wrap
// prose, so the comparison normalises whitespace — the string may break
// across a line, it may not disagree.
const cssFloor = "Chrome 123, Safari 17.5, Firefox 121"

// The adoption policy is deliberately a rolling one: a feature is fair
// game once it has been in all three engines about a year. A rolling
// policy with no clock, though, is just a lapsed one that nobody has
// noticed yet — these two constants are the clock, in the same spirit as
// skillBudget above it. Mechanical rather than remembered.
//
// Note what does NOT move when the policy rolls: the floor. It rises
// only when someone actually writes a newer feature into the CSS, so
// "we may use anything a year old" and "today we bottom out at Chrome
// 123" stay true together, indefinitely, without either drifting behind
// the other. That is also why the review is by hand rather than
// computed. The computable half — what is now a year old — was never
// the hard half. The judgement is which of those we want, and every
// yes is a floor raise for every app that vendored this CSS and will
// not learn about it until someone opens the app on an older machine.
//
// Nine months rather than twelve, so a review that slips a release
// still lands inside the year the policy names.
//
// Answering the failure means re-deriving, not bumping the date: list
// the features tokens.css and ui/themes/*.css use, take the highest
// engine version among them, and if it differs from cssFloor change it
// here, in SKILL.md §7 and in docs/site/templates.md together. Then set
// floorReviewed to the day you did it.
const (
	floorReviewed     = "2026-09-01"
	floorReviewMonths = 9
)

// TestCSSFloorIsStatedTheSameInBothFiles fails when SKILL.md and the
// templates page disagree about the floor. They are written for
// different readers — the skill is what an agent loads instead of the
// source, the page is what a person reads when the answer surprised
// them — and a reader who checks one against the other and finds two
// numbers trusts neither.
func TestCSSFloorIsStatedTheSameInBothFiles(t *testing.T) {
	for _, path := range []string{"SKILL.md", "docs/site/templates.md"} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s must exist: %v", path, err)
		}
		if !strings.Contains(strings.Join(strings.Fields(string(b)), " "), cssFloor) {
			t.Errorf("%s does not state the floor %q.\n"+
				"Either it drifted, or the floor moved and this file was not brought along — "+
				"cssFloor, SKILL.md §7 and docs/site/templates.md change together or not at all.",
				path, cssFloor)
		}
	}
}

// TestCSSFloorHasBeenReviewedRecently is the tripwire. It fails on a
// date, which is unusual and intended: the alternative is a warning,
// and a warning on a release evening is a thing you scroll past. The
// cost of being wrong in the two directions is not symmetric — a build
// that stops for ten minutes while somebody re-reads a list of CSS
// features is cheap; a floor that quietly describes 2024 while the
// policy promises "about a year" is the exact silence this whole
// programme started from.
func TestCSSFloorHasBeenReviewedRecently(t *testing.T) {
	reviewed, err := time.Parse(time.DateOnly, floorReviewed)
	if err != nil {
		t.Fatalf("floorReviewed must be a %s date: %v", time.DateOnly, err)
	}
	due := reviewed.AddDate(0, floorReviewMonths, 0)
	if time.Now().After(due) {
		t.Fatalf("the CSS floor was last reviewed %s and was due %s.\n"+
			"Re-derive it — the features tokens.css and ui/themes/*.css use, highest engine "+
			"version among them — then update cssFloor, SKILL.md §7 and docs/site/templates.md "+
			"if it moved, and set floorReviewed to today. Do not bump the date alone; the date "+
			"is the record that somebody looked.",
			floorReviewed, due.Format(time.DateOnly))
	}
}
