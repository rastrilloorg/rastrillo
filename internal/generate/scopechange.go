package generate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"amadan.net/rastrillo/rastrillo"
)

// scopedRows and scopedRecords are printed once, on the generate run
// that first sees a resource scoped: the one moment the author is
// looking at the change. Once the app boots, a row from before the
// change matches no session, so it vanishes from every list, and
// nothing else reports it. The SQL store can be repaired with a
// migration; a mergeable record's owner lives in its event stream, and
// the generated store refuses to update a record it does not own, so
// the author appends the event themselves.
const (
	scopedRows    = "%s: rows from before scope = \"user\" have no owner, and a row with no owner is shown to nobody; assign owners in a migration of your own, written with rastrillo migration new"
	scopedRecords = "%s: records from before scope = \"user\" have no owner, and a record with no owner is shown to nobody; give each one an owner by appending an updated event that carries owner to its stream, since the generated store cannot update a record it does not own"
)

// previousGeneration is the resource set the last generate wrote, read
// from gen/manifest.json in the app's real tree. nil with no error
// when there is none: a first generate has no transition to report.
// It is read before this run writes the artifact, which is what makes
// the comparison a transition and not a description of the present.
func previousGeneration(genDir string) ([]rastrillo.Resource, error) {
	data, err := os.ReadFile(filepath.Join(genDir, "manifest.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rs []rastrillo.Resource
	if err := json.Unmarshal(data, &rs); err != nil {
		return nil, err
	}
	return rs, nil
}

// scopeNotes is one line per resource that the previous generation had
// unscoped and this one has user-scoped, whichever store it keeps. A
// resource absent from the previous generation is new and has no rows
// to hide; one scoped in both is a transition already reported. The
// comparison is against the recorded generation rather than the
// migration statements, because a resource unscoped and scoped again
// keeps its owner column and gets no new ALTER, and a mergeable store
// has no statements at all.
func scopeNotes(previous, rs []rastrillo.Resource) []string {
	was := map[string]rastrillo.ScopeKind{}
	for _, r := range previous {
		was[r.Name] = r.Scope
	}
	var out []string
	for _, r := range rs {
		prev, known := was[r.Name]
		if !known || prev == rastrillo.UserScoped || r.Scope != rastrillo.UserScoped {
			continue
		}
		msg := scopedRows
		if r.Store == rastrillo.Mergeable {
			msg = scopedRecords
		}
		out = append(out, fmt.Sprintf(msg, r.Name))
	}
	return out
}
