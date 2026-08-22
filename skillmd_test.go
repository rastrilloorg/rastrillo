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
//
// Raised from 15_000 to 16_000 when the migrations subsystem (Design:
// 2026-08-22-migrations) landed its own §2 content — a genuinely new
// surface (Schema/BootSchema, the immutability rule, baseline
// recovery) that a trim-only budget could only have paid for by
// cutting existing, load-bearing facts (see the task-13 fix-up: a
// join-table tie-breaker, SafeReturn's path spec, RenderSignup's
// enforcement, the keymail viewer alternative, and the jobs
// Render/RenderFragment warning were all dropped to fit 15_000, then
// restored here). Trim before raising again.
//
// Raised again, 16_000 to 17_000, at the merge with main for v0.17.0.
// The previous raise sized its headroom for the migrations content
// alone; main independently grew the file by ~1_057 bytes over the
// same window (SSE push, icon sets and delivery modes, mergeable
// stores, the tenancy ruling, the stripped release target), so two
// releases' worth of new surface arrived against one ceiling. The
// merge was trimmed first — the rate-limiting paragraph absorbed
// main's two new facts in 13 fewer bytes than either side spent — and
// a duplication sweep found none, so the remainder is real growth,
// not slack. Trim first, still.
const skillBudget = 17_000

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
