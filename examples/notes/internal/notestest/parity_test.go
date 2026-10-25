package notestest

import (
	"testing"

	"notes/gen/paritytest"
)

// TestBookmarksParity runs the generated scenario through the bookmark
// pages and the API: the same rows and the same refusals through both.
func TestBookmarksParity(t *testing.T) { paritytest.RunBookmarks(t, fixture{}) }
