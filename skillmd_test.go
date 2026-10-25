package rastrillo

import (
	"os"
	"path/filepath"
	"regexp"
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
const (
	// skillCoreBudget holds SKILL.md near its 2026-10-22 size (6,660
	// bytes) with room for the index to grow by a few topics. Raising
	// it means moving something into a topic, which is almost always
	// the right answer, so raise it only for a rule every task needs.
	skillCoreBudget = 8_000
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
