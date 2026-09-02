package rastrillo

import (
	"os"
	"path/filepath"
	"strings"
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
//
// Raised again, 17_000 to 18_000, for the scheduled-work paragraph in
// §6 (Design: carlosframework/platform 2026-08-23-scheduled-work).
// That is a whole new package — carlos.Tick, TickOccurrence,
// ScheduleAt — and one an agent writing an app cannot infer, because
// the thing it replaces (a time.Ticker in main) compiles, runs, and is
// silently wrong on a hibernating instance. A trim pass over §6 found
// no redundancy to pay for it: every line there is a fact about the
// jobs API or the poll shim, and the one duplication review did find —
// the package's full import path, already covered by §1's "and its
// subpackages" — was cut before this number moved. 584 bytes of the
// raise are spent; the rest is headroom, not licence.
// A 2026-08-28 editorial rewrite (terser prose, same facts and API
// names) brought the file from 17,998 back to ~15,100, so the budget
// holds real headroom again for the design-system programme's keys.
//
// Held at 18_000 on 2026-08-31, when the file had grown back to 17,815
// and a raise was on the table. It bought headroom by delegation
// instead: five blocks whose detail was already carried by a docs page
// (date/time kinds -> forms.md, the password plugin -> passwords.md,
// the jobs handlers -> jobs.md, doctor's exit codes -> cli.md, the
// locale list and switcher -> localization.md) were cut to one
// sentence plus their link, 17,815 -> 16,196. Prose was deliberately
// NOT re-squeezed: the 2026-08-28 pass already did that, and the
// compression round before it withdrew four of eleven proposed cuts on
// review for gutting meaning. The rule that made this safe is worth
// keeping: a block stays inline if getting it wrong is silent (the
// zero-time reads, keys-not-sentences, RenderFragment nesting the
// layout, the noscript refresh loop), and moves to its page if you
// would look it up anyway. Sixteen of the twenty-four docs pages are
// still unlinked from SKILL.md, so this lever has more left in it than
// the budget does. Trim first, still.
// Raised 18,000 -> 19,000 on 2026-09-02, and the reason is a shape this
// budget will meet again rather than a one-off.
//
// Two branches grew SKILL.md at once. Each was under 18,000 on its own
// and each passed CI on its own; the merge summed them and landed at
// 18,084, so `main` went red on a textually clean merge that neither
// author could have seen coming. A byte budget has no merge-time guard
// — there is no way to express "and not when added to whatever else
// lands first" — so this will recur whenever two branches touch the
// file in one cycle.
//
// Raised rather than trimmed, per AGENTS.md: both sides added
// load-bearing facts (the stat band's rules, and the bidi and <time>
// rules that no other page states), and cutting one to fit would delete
// a fact to satisfy a number. The prose was trimmed first — the entry
// that arrived with this raise was cut by roughly a third — and it
// closed 146 of the 437, which is the honest measure of how much slack
// is left in re-squeezing: not much. The lever with room in it is still
// the one named above, sixteen unlinked docs pages.
const skillBudget = 19_000

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

// TestSkillMDSaysExamplesAreNotInTheModule holds one sentence in place,
// and the fact under it.
//
// SKILL.md is what an agent loads instead of reading the source, and it
// names examples/notes as the worked reference. Every example is its own
// Go module — that is what keeps the root `go test ./...` from compiling
// them — and Go excludes a nested module from its parent's module zip.
// So for everyone consuming rastrillo as a dependency that pointer names
// a directory their checkout cannot contain. Measured cost, from a
// docs-only build test: four agents found the declarative path, three
// planned to use it, and the hunt for examples/ is where each of them
// stopped (discussion #9).
//
// Two assertions rather than one, because the sentence and the reason
// for it fail separately. If examples/ ever does ship in the module
// zip, the caveat becomes false and should go — and the second
// assertion is what will say so, by finding a nested module that is no
// longer there.
func TestSkillMDSaysExamplesAreNotInTheModule(t *testing.T) {
	b, err := os.ReadFile("SKILL.md")
	if err != nil {
		t.Fatalf("SKILL.md must exist at the repo root: %v", err)
	}
	src := string(b)
	if strings.Contains(src, "examples/") && !strings.Contains(src, "not** in the\npublished module") {
		t.Error("SKILL.md names examples/ without saying it is absent from the published module; a reader consuming rastrillo as a dependency will go looking for a directory that cannot be there")
	}

	// The reason, checked rather than asserted: examples/* really are
	// separate modules, which is why the zip excludes them.
	nested, err := filepath.Glob(filepath.Join("examples", "*", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if len(nested) == 0 {
		t.Error("no examples/*/go.mod — the examples are no longer nested modules, so SKILL.md's caveat about the module zip may now be false")
	}
}
