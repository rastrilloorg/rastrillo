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
//
// Raised, 18_000 to 30_000, on 2026-09-02 — a change of regime rather
// than another notch, so read this one before citing the paragraphs
// above. Each earlier raise bought room for one new subsystem and was
// defended by a trim. This one is a deliberate decision to stop trimming
// for a while, and two things forced it. The file reached main 84 bytes
// OVER 18_000, red before a word was added: the delegation lever
// (2026-08-31) had already been pulled and the file grew back past the
// ceiling within two days, which is what a lever with nothing left in it
// looks like. And the trimming now costs more than it saves. An agent
// building an app from this file stacked a create form, an import panel
// and a list onto one screen, because nothing here said not to — §7 did
// not exist, because a byte ceiling had it competing with the SQL. Every
// remaining gap has that shape: a rule the framework holds and never
// wrote down.
//
// The ceiling was sized for a context window that is no longer the
// binding constraint — Opus and GLM 5.3 both load a 30 KB file without
// noticing. Concision is still the rule: an inaccurate or padded line is
// worse here than a missing one, and small models still read this file.
// The pruning is deferred, not cancelled — one human pass, once the
// framework is reliably producing good apps across a variety of them,
// which today it is not. Until that pass: write the rule down, keep it
// short, and do not read this number as room to fill.
const skillBudget = 30_000

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
