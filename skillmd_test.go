package rastrillo

import (
	"os"
	"testing"
)

// skillBudget is SKILL.md's hard size ceiling. The file is the
// authoring contract an agent loads instead of the framework source —
// its value IS its smallness, so growth is paid for by trimming, never
// by raising this number casually (that's a product decision, not a
// convenience).
const skillBudget = 15_000

// TestSkillMDStaysWithinBudget makes the budget mechanical rather than
// remembered: several release evenings have ended with a wc -c dance
// after an edit nudged the file a few bytes over.
func TestSkillMDStaysWithinBudget(t *testing.T) {
	b, err := os.ReadFile("SKILL.md")
	if err != nil {
		t.Fatalf("SKILL.md must exist at the repo root: %v", err)
	}
	if len(b) > skillBudget {
		t.Fatalf("SKILL.md is %d bytes — %d over the %d budget; trim, don't grow",
			len(b), len(b)-skillBudget, skillBudget)
	}
}
