package tour

import (
	"strings"
	"testing"
)

func TestAPatchAppliesWhereverItsContextMovedTo(t *testing.T) {
	patch := "--- a/x.go\n+++ b/x.go\n" +
		"@@ -1,3 +1,4 @@\n one\n two\n+inserted\n three\n"
	got, err := applyPatch("zero\none\ntwo\nthree\nfour\n", patch)
	if err != nil {
		t.Fatal(err)
	}
	if want := "zero\none\ntwo\ninserted\nthree\nfour\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAPatchWhoseLinesAreGoneIsRefused(t *testing.T) {
	_, err := applyPatch("one\nchanged\nthree\n",
		"@@ -1,3 +1,3 @@\n one\n-two\n+2\n three\n")
	if err == nil || !strings.Contains(err.Error(), "no longer matches") {
		t.Errorf("err = %v, want the hunk refused", err)
	}
}

// The same lines twice, as the end of two structs often are: the hunk
// lands on the occurrence nearest the line its header names.
func TestAPatchMatchingTwiceLandsNearestItsLine(t *testing.T) {
	got, err := applyPatch("a\nb\nc\na\nb\n", "@@ -4,2 +4,3 @@\n a\n b\n+x\n")
	if err != nil || got != "a\nb\nc\na\nb\nx\n" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestARemovedSQLCommentIsNotAHeader(t *testing.T) {
	got, err := applyPatch("keep\n-- old\nkeep too\n",
		"@@ -1,3 +1,2 @@\n keep\n--- old\n keep too\n")
	if err != nil || got != "keep\nkeep too\n" {
		t.Errorf("got %q, %v", got, err)
	}
}

// A hunk's lines must be whole lines of the file: "b" is not the end
// of "ab", so the hunk lands on the line that is "b".
func TestAPatchMatchesWholeLinesOnly(t *testing.T) {
	got, err := applyPatch("ab\nc\nb\nc\n", "@@ -1,2 +1,2 @@\n-b\n+B\n c\n")
	if err != nil || got != "ab\nc\nB\nc\n" {
		t.Errorf("got %q, %v", got, err)
	}
}

// A line without a newline is the file's last, so only the last line
// of the file matches it.
func TestALineWithoutANewlineMatchesOnlyAtTheEnd(t *testing.T) {
	patch := "@@ -1 +1 @@\n-x\n\\ No newline at end of file\n+y\n"
	if _, err := applyPatch("x\nz\n", patch); err == nil {
		t.Error("a mid-file line took a hunk meant for the last line")
	}
	got, err := applyPatch("z\nx", patch)
	if err != nil || got != "z\ny\n" {
		t.Errorf("got %q, %v", got, err)
	}
}

// The second hunk's header counts the first hunk's lines, and the file
// has two copies of its context: it lands on the one after the first
// hunk, where its header points once the growth is counted.
func TestLaterHunksCountEarlierOnes(t *testing.T) {
	patch := "@@ -1,1 +1,3 @@\n a\n+a1\n+a2\n" +
		"@@ -3,1 +5,2 @@\n a\n+second\n"
	got, err := applyPatch("a\nb\na\nb\n", patch)
	if err != nil || got != "a\na1\na2\nb\na\nsecond\nb\n" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestAPatchCutShortIsRefused(t *testing.T) {
	_, err := applyPatch("one\ntwo\nthree\n", "@@ -1,3 +1,1 @@\n-one\n")
	if err == nil || !strings.Contains(err.Error(), "header says") {
		t.Errorf("err = %v, want the short hunk refused", err)
	}
}

func TestAnInsertionWithNoContextApplies(t *testing.T) {
	got, err := applyPatch("", "@@ -0,0 +1 @@\n+first\n")
	if err != nil || got != "first\n" {
		t.Errorf("into an empty file: got %q, %v", got, err)
	}
	got, err = applyPatch("a\nb\n", "@@ -1,0 +2 @@\n+between\n")
	if err != nil || got != "a\nbetween\nb\n" {
		t.Errorf("after line 1: got %q, %v", got, err)
	}
}
