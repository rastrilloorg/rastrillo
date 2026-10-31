package rastrillo

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The skill is SKILL.md, which every session loads, and the topic files
// in skill/, which a session reads only for the work in front of it.
//
// Until 2026-10-22 it was SKILL.md alone, held to one byte ceiling that
// had been raised from 15,000 to 32,127, each raise by exactly the
// paragraph being added, because a single file has no other way to
// take a new feature: every session paid for every feature. The
// skilleval baseline (hack/skilleval, discussion #97) showed the cost
// was also in the wrong place: all eight runs read the 32 KB file and
// still read 70–117 KB of framework source for what it did not cover.
// So the core now carries only what every task needs, plus the index
// that sends an agent to a topic, and each topic has a ceiling of its
// own. A new feature costs a line in the index and a paragraph in one
// topic; only the sessions that do that work read it.
//
// The single file's raises are kept below as the record of what each
// ceiling bought and what paid for it, since the topics inherit those
// paragraphs and the same rule: trim first, and never cut a fact to
// satisfy a number. The 2026-09-05 entry is a change of regime; read
// it before citing the ones above it.
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
//
// Raised 19,000 -> 19,600 on 2026-09-02 for the CSP paragraph in §5.
// It is a fact an agent cannot infer and gets no warning about: the
// baseline `form-action 'self'` is checked against a form submission's
// whole redirect chain, so auth's own 303 out to a keymail server is
// refused by the framework's own default policy, in the browser, only
// at sign-in. Nothing else in the file mentions the baseline's
// contents, and the fix is unguessable twice over — Options.CSP
// replaces the policy wholesale rather than adding to it, so the
// paragraph has to carry the literal baseline string as well as the
// rule. A trim pass over §5 found nothing to pay for it; the sections
// read this cycle are facts, not prose. The lever is still the one
// named above.
//
// Raised, 19_600 to 30_000, on 2026-09-05 — a change of regime rather
// than another notch, so read this one before citing the paragraphs
// above. Every raise above it bought room for one new subsystem and was
// defended by a trim; the two most recent were the narrow answer to an
// incident this one answers properly. The file reached main 84 bytes
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
//
// Raised 30,000 -> 30,750 on 2026-10-04, the 2026-09-02 shape again: a
// textually clean merge summing two branches that each passed alone.
// main's budgets section (§7a), phone shells and row/prerender rules
// met pow-admission's front door (auth and password refuse to start
// without a proof choice; Admit before BeginTx; Check for
// redirect-after-POST; Recovery after a refusal), landing at 30,732.
// Each is a rule an agent gets silently wrong otherwise: a form that
// 500s on ErrSpent, a cached challenge shared by every visitor, a
// vendored pow.js drifting from the verifier, a test package nobody
// times. The duplicated Proof/ProofOff rule is stated once, which
// leaves the file at 30,608. A deeper squeeze of the pow paragraph was
// tried and reverted on review: it dropped why Check exists and that
// the app owns and sweeps the Guard, which is the failure the ceiling
// must never cause. 30,750 is the smallest round number above that,
// so the 142 bytes over the file's size are slack for a one-line
// correction, not room to fill; the deferred human pruning pass above
// still stands.
//
// Raised 30,750 -> 30,900 on 2026-10-09 for one sentence: seal with
// carlos.InstanceKey, mint only on ErrNotOnCarlos. An agent that keeps
// minting its key on CARLOS ships an app whose rows can outlive their
// key when region HA relocates it, which no test of the app's own will
// catch. The file had 3 bytes of slack; no existing line is redundant
// enough to pay 147 without losing a fact, so this lands at 30,894.
//
// Raised 30,900 -> 31,365 on 2026-10-14 for the multi-currency release,
// which changed how every template calls T and how money is read. The
// old lines (a per-request Clone with ui.WithLocale, money as int64
// cents) now produce pages that fail in strict mode and forms that
// store an amount without its currency, so the file must say: ui.Pages
// with $L first, the money field's Currency/Minor/SeedMoney/Stored
// contract, manifest/app.toml's locales and the probe, and gen.Boot.
// Net of the Clone recipe and cents line they replace, the new lines
// add 718 bytes. Trimming restated prose (the design-system "reuse"
// line, the four-step flow's preamble, the empty-state echo of step 1,
// the stacking example's aftermath, the CSS floor's date) paid 249;
// the file landed at 31,365, 465 over, and the ceiling is raised by
// exactly that, so there is no slack.
//
// Raised 31,365 -> 31,630 on 2026-10-15 for one paragraph: the
// colour-scheme switch. Four apps hand-rolled a toggle before the
// framework shipped one, because nothing told an agent the shells
// offer it; the next agent reading only the source would see the
// themes' data-theme rules and build a fifth. The paragraph names the
// partial, the route, the cookie, the field and the attribute, in 265
// bytes. The file had no slack after the previous raise and no line
// in the locales section restates another, so the ceiling is raised
// by exactly the addition.
//
// Raised 31,630 -> 31,761 on 2026-10-18 for one sentence: the media
// grid. Patrix hand-rolled an image grid that stretched its cards
// because nothing told an agent the framework ships one; the sentence
// names the partial and its three switches (Fit, Select, Menu) in 131
// bytes. The file had no slack after the previous raise and the rows
// paragraph it joins restates nothing, so the ceiling is raised by
// exactly the addition.
//
// Raised 31,761 -> 31,897 on 2026-10-18 for one paragraph: a home or
// hub creates tenants with `provision` and tests with `provisiontest`.
// Without it an agent building a hub reads only the console's API and
// writes its own client, which misses the record contract (record
// before POST, Resume on every later call) and orphans an instance the
// first time a create times out. The file had no slack after the media
// grid; the paragraphs that might pay were read again (the objrec line
// it sits under, the §0 and §1 pointers, the §8 page links) and each
// holds a fact or a page an agent needs, so the ceiling is raised by
// exactly the 136 bytes the paragraph adds.
//
// Raised 31,897 -> 32,096 on 2026-10-20 for one sentence: light.js.
// The vendored list gained a file an app can delete, and a skill that
// omits a deletable file tells an agent to keep what it may remove,
// or to treat its own pin test's failure on the missing file as a bug
// rather than a line to add to vendoredIsMine. The sentence names the
// file, the three opt-out steps and what doctor says afterwards, in
// 199 bytes. The file had no slack after the previous raise and the
// scripts paragraph it joins restates nothing, so the ceiling is
// raised by exactly the addition. The copy is a draft until the review
// lands; the number moves again with the approved text.
//
// 32,148 (#104 item 3): doctor now calls a deleted, recorded light.js
// "removed on purpose", and the sentence quotes it, 21 bytes more. The
// ceiling moves by exactly that; nothing around it was slack to trim.
//
// 2026-10-26 (#104 item 3, day-followups): doctor now calls a deleted,
// recorded light.js "removed on purpose", and the sentence quotes it,
// 21 bytes more. In the single file that moved the ceiling to 32,148,
// by exactly the addition. The sentence now lives in
// skill/background.md, 2,921 bytes against the topic ceiling, so no
// ceiling moves: that is the split doing what it was for.
//
// Core raised 8,000 -> 9,100 on 2026-10-30 for "Write code that reads
// like a sentence" (Design: 2026-10-29-readable-code), the rule the
// skillCoreBudget comment reserves a raise for: every task writes Go.
// An agent that never loads skill/code.md still writes `w` and `r` and
// a 60-line handler, and an app's gate does not run the limits yet, so
// nothing would tell it. The section is the four limits and rules 1 to
// 7, a line each; the other seven rules, the action shape and the gate
// messages went to skill/code.md. Trimming paid 120 bytes (the intro's
// restated topic pointer, a sess.Require line the App shape and the
// checklist both carry, the index pointer's preamble); the rest of the
// core is facts, and the 1,461 bytes added land the file at 9,084.
// The ceiling is the next hundred above that: slack for a one-line
// correction from review, not room to fill.
//
// Held at 9,100 on 2026-10-31, when step 4 rewrote the App shape and
// Owner scoping for the scaffold's App, New and Routes. The longer
// names were paid for without cutting a fact: the file list moved to
// the app's AGENTS.md, which lists it, the session cookie facts to
// skill/signin.md, and the body-binding rule is said once here, its
// detail in skill/forms.md. Review's fixes (map gorm.ErrRecordNotFound
// to op.NotFound; mount gen.Router in Routes) took it to 9,100, with
// "the scaffold's *gorm.DB" cut to "a *gorm.DB": no slack is left.
const (
	// skillCoreBudget holds SKILL.md near its 2026-10-22 size (6,660
	// bytes) with room for the index to grow by a few topics. Raising
	// it means moving something into a topic, which is almost always
	// the right answer, so raise it only for a rule every task needs.
	skillCoreBudget = 9_100
	// skillTopicBudget is per topic file. A topic that outgrows it
	// splits in two, each with its own index line.
	skillTopicBudget = 9_000
)

// skillFiles are SKILL.md and every topic, by path.
func skillFiles(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	paths, err := filepath.Glob(filepath.Join("skill", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range append([]string{"SKILL.md"}, paths...) {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		files[p] = string(b)
	}
	return files
}

// TestSkillStaysWithinBudget makes the budgets mechanical rather than
// remembered: several release evenings ended with a wc -c dance after
// an edit nudged the single file a few bytes over.
func TestSkillStaysWithinBudget(t *testing.T) {
	for p, s := range skillFiles(t) {
		budget := skillTopicBudget
		if p == "SKILL.md" {
			budget = skillCoreBudget
		}
		if len(s) > budget {
			t.Errorf("%s is %d bytes, %d over its %d budget: move detail into a topic, or split the topic",
				p, len(s), len(s)-budget, budget)
		}
	}
}

// TestSkillIndexNamesEveryTopic: a topic the index does not name is
// never read, because the index is how an agent finds it; and a topic
// named anywhere must exist, or the pointer sends the agent back to
// the source.
func TestSkillIndexNamesEveryTopic(t *testing.T) {
	files := skillFiles(t)
	// The index is the table's rows, not any mention elsewhere in the
	// core: a topic named only in passing is not one an agent is sent to.
	var index []string
	for _, line := range strings.Split(files["SKILL.md"], "\n") {
		if strings.HasPrefix(line, "| ") && strings.Contains(line, "`skill/") {
			index = append(index, line)
		}
	}
	rows := strings.Join(index, "\n")
	for p := range files {
		if p != "SKILL.md" && !strings.Contains(rows, "`"+filepath.ToSlash(p)+"`") {
			t.Errorf("SKILL.md's index table has no row for %s", p)
		}
	}
	ref := regexp.MustCompile(`skill/[a-z0-9-]+\.md`)
	for p, s := range files {
		for _, m := range ref.FindAllString(s, -1) {
			if _, ok := files[m]; !ok {
				t.Errorf("%s names %s, which does not exist", p, m)
			}
		}
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
	for p, src := range skillFiles(t) {
		if strings.Contains(src, "examples/") && !strings.Contains(src, "not** in the\npublished module") {
			t.Errorf("%s names examples/ without saying it is absent from the published module; a reader consuming rastrillo as a dependency will go looking for a directory that cannot be there", p)
		}
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

// The skill is what an agent loads instead of the source. After this
// release, the per-request Clone recipe and ui.WithLocale binding it
// taught fail in strict mode, and money is no longer cents only; an
// agent following the old lines writes code that does not render.
func TestSkillTeachesTheLocalizerAndCurrencyContract(t *testing.T) {
	var all []string
	for _, s := range skillFiles(t) {
		all = append(all, s)
	}
	s := strings.Join(all, "\n")
	for _, gone := range []string{"Bind `ui.WithLocale(rastrillo.LocaleFrom(r))`", "each request's cloned template", "Money is `int64` cents"} {
		if strings.Contains(s, gone) {
			t.Errorf("the skill still says %q", gone)
		}
	}
	for _, want := range []string{"ui.Pages", "{{$L := localizer $}}", "{{money $L", "form.SeedMoney", "Strict: true", "manifest/app.toml", "RASTRILLO_CHECK_CONFIG", "gen.Boot"} {
		if !strings.Contains(s, want) {
			t.Errorf("the skill does not mention %q", want)
		}
	}
}

// The skill teaches the shape rastrillo new writes (readable-code step
// 4): an agent copies the nearest example, so a line still showing the
// old chi wiring or a one-letter handler is the code it writes next.
func TestSkillTeachesTheScaffoldsShape(t *testing.T) {
	files := skillFiles(t)
	files["docs/site/app-shape.md"] = readSkillPage(t,
		"docs/site/app-shape.md")
	for path, text := range files {
		joined := strings.Join(strings.Fields(text), " ")
		gones := obsoleteShape
		if path != "skill/live.md" {
			gones = slices.Concat(obsoleteShape, netHTTPShape)
		}
		for _, gone := range gones {
			if strings.Contains(joined, gone) {
				t.Errorf("%s still says %q", path, gone)
			}
		}
	}
	core := strings.Join(strings.Fields(files["SKILL.md"]), " ")
	for _, want := range []string{
		"`func Routes(routes *action.Routes, app *App)`",
		"`routes.Mount(\"/api/v1\", app.api, \"api\")`",
		"`visit.Render(", "`app.owned(request).",
		"`app.auth.RequireSession`", "`make code`", "`make routes`",
	} {
		if !strings.Contains(core, want) {
			t.Errorf("SKILL.md does not say %q", want)
		}
	}
	screens := strings.Join(strings.Fields(files["skill/screens.md"]), " ")
	if want := "`func Mount(routes *action.Routes, deps *Deps)`"; !strings.
		Contains(screens, want) {
		t.Errorf("skill/screens.md does not say %q", want)
	}
	unreleased, _, _ := strings.Cut(readSkillPage(t, "CHANGELOG.md"),
		"\n## v")
	unreleased = strings.Join(strings.Fields(unreleased), " ")
	if strings.Contains(unreleased, "Nothing runs it in an app") ||
		!strings.Contains(unreleased, "rastrillo budget code --init") ||
		!strings.Contains(unreleased,
			"only once the app has a `Routes` function") {
		t.Errorf("CHANGELOG.md's Unreleased section does not say how " +
			"an existing app adopts the gates, the routes lane last")
	}
}

// obsoleteShape is what the skill said before the scaffold moved to App,
// New, Routes and actions, and what said an app does not run the gates.
var obsoleteShape = []string{
	"does not run it yet", "The scaffold still writes", "being renamed",
	"Mount(r chi.Router, deps)", "owned(r)", "(a *app)", "notes.App(",
	"func App(", "r.Use(csrf.Protect(origin))", "r.Use(sess.Require)",
	"`sess.Require` group", "render(w, r", "`Router` (`app.go`)",
	"called from `Router`", "`render.go`'s `render`",
	"the render helper writes none", "everything `App()` applies",
	"r.Use(sess.Middleware)", "mux.Handle(\"/api/v1\"", "`d.G",
}

// netHTTPShape is a handler written on net/http and chi. skill/live.md is
// exempt until step 6 or 7 moves it: its render path cannot go through
// visit.Render yet (the design's order of work says why).
var netHTTPShape = []string{"(w, r", "r.Use(", "pages.Render(w, r"}

func readSkillPage(t *testing.T, path string) string {
	t.Helper()
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}
