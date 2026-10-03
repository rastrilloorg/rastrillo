package ui

import (
	"encoding/json"
	"testing"

	"amadan.net/rastrillo/rastrillo/nodetest"
)

// A tap on the back control writes its record before any navigation
// commits. When the reader then cancels the leave-page prompt and
// follows some other link, the next page must slide forward: the record
// was for a navigation that never happened. The real Back still slides
// back. A browser drive cannot cancel a beforeunload prompt on cue, so
// this runs shell.js itself against stub pages in Node.
func TestACancelledBackDoesNotSlideTheNextPageBack(t *testing.T) {
	var got struct{ Taken, Cancelled string }
	if err := json.Unmarshal(nodetest.Run(t, nodetest.Cmd{Args: []string{"shell_node.mjs"}}), &got); err != nil {
		t.Fatal(err)
	}
	if got.Taken != "back" {
		t.Errorf("Back tapped and taken: the up page slid %q, want back", got.Taken)
	}
	if got.Cancelled != "forward" {
		t.Errorf("Back tapped, the prompt cancelled, then a link to another page followed: it slid %q, want forward", got.Cancelled)
	}
}
