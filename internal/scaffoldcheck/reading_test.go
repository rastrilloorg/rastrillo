package scaffoldcheck

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// What an agent read to learn a new app's shape before the readable-code
// design's step 4 (0fef47c6): the bytes of every file outside gen/ and
// static/, and of AGENTS.md alone; and the share of the total that must
// go. Plain loses a quarter. --auth=magic loses 19%: that is what the
// prose cuts reach, and the rest of its bytes are sign-in code, not
// prose. Neither total may grow again: Task 4's manifest recipe pays for
// any bytes it adds by trimming elsewhere.
//
// migrationCheck is the first allowance on top of the cut: the migration
// check moved from the Makefile into the app's own test,
// migrations_test.go (scaffold-shapes spec §4.2 item 3), which adds 549
// bytes to either app. Each allowance is the part of those 549 that did
// not fit under the old limit, and no more.
//
// defaultSignin is the second: the default sign-in (default-signin
// branch) gives --auth=magic's app a passkey, an authenticator app and
// a passphrase through signin, which adds 3,934 bytes, magicapp being
// exactly at its limit before: signin.New, the sweep and the page that
// draws every signin View in auth.go (1,305), the chooser, offer and
// account-page tests in signin_test.go (1,945), and the schemas, gate,
// Sign-in options links and AGENTS.md line elsewhere (684). The final
// review added 1,118 more: with no instance key the app leaves the
// authenticator off and says so, and a test checks it, while the
// tests' TestMain sets a key (5,052 in all).
//
// lintGate is the third: make ci gained go fix and golangci-lint
// (bench/agent/results/lint-sweep-groomer.md), with both apps at their
// limits. .golangci.yml (491), the two lanes in the Makefile and their
// ci.d steps (462), AGENTS.md naming them (100), and the scaffold's own
// tests passing them (191: closeAll's error checked, a strings.Builder
// for go fix): 1,244. magicapp's TestMain also checks os.Setenv (40).
//
// auditCheck is the fourth: every table says whose data it holds
// (audit spec §1), and the app checks that in its own test, as it does
// the migration check. audit_test.go (286), the notes table's
// declaration in migrations.go (84) and the manifest recipe declaring
// its generated table (39): 409, with both apps at their limits.
// magicapp's notes gain an owner holding the session subject, so its
// declaration says Personal, owned by that column and erased with the
// row: 76 more, 485. Saying ownerOf holds the subject, not the address,
// cost no bytes: its comment was trimmed to pay for it.
var before = map[string]struct {
	total, agents, cutPercent, migrationCheck, defaultSignin,
	lintGate, auditCheck int
}{
	// plainapp was 41 bytes under its limit, so 508 of the 549 went over.
	"plainapp": {73438, 6536, 25, 508, 0, 1244, 409},
	// magicapp was already 495 bytes under its limit, so only 54 of the
	// 549 went over.
	"magicapp": {91400, 7360, 19, 54, 5052, 1284, 485},
}

// iconGrowth is the code, not prose, that Fiesta 1.4.0's icons added to
// every app's icons/icons.go (605e2239): sun, moon and monitor for the
// scheme control, chevron-left for the up link. Every set covers every
// slug, so no shell drops one. plainapp went from 55,077 to 56,328
// bytes and magicapp from 73,578 to 74,829.
const iconGrowth = 1251

// The shape is read in fewer bytes than before, and AGENTS.md grows by
// at most 1,000 to hold the guidance that left the comments.
func TestNewAppsReadInFewerBytes(t *testing.T) {
	t.Parallel()
	for _, variant := range variants {
		directory := newApp(t, variant.name, variant.flags...)
		was := before[variant.name]
		total := bytesToRead(t, directory)
		limit := was.total*(100-was.cutPercent)/100 + iconGrowth +
			was.migrationCheck + was.defaultSignin + was.lintGate +
			was.auditCheck
		if total > limit {
			t.Errorf("%s: %d bytes outside gen/ and static/, over %d, "+
				"%d%% less than %d, %d for the migration check, %d "+
				"for the default sign-in, %d for the lint gate and %d "+
				"for the audit check", variant.name, total, limit,
				was.cutPercent, was.total, was.migrationCheck,
				was.defaultSignin, was.lintGate, was.auditCheck)
		}
		agents := len(readFile(t, filepath.Join(directory, "AGENTS.md")))
		if limit := was.agents + 1000; agents > limit {
			t.Errorf("%s: AGENTS.md is %d bytes, over %d", variant.name,
				agents, limit)
		}
	}
}

// bytesToRead adds up every file rastrillo new wrote outside gen/ and
// static/.
func bytesToRead(t *testing.T, root string) int {
	t.Helper()
	total := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry,
		err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == "static" ||
			path == filepath.Join(root, "gen")) {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		total += int(info.Size())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return total
}

// The instructions an agent cannot work without survive the cut: where a
// subsystem's schema goes, scoping owned rows, writing a migration, the
// gates, and raising Go for a security release.
func TestAgentsMDKeepsTheEssentialInstructions(t *testing.T) {
	t.Parallel()
	owned := map[string]string{
		"plainapp": "`scope.Owned`", "magicapp": "`app.owned(request)`",
	}
	for _, variant := range variants {
		directory := newApp(t, variant.name, variant.flags...)
		agents := strings.Join(strings.Fields(readFile(t,
			filepath.Join(directory, "AGENTS.md"))), " ")
		for _, want := range []string{
			"`BootSchema`", "never in `Schema`", owned[variant.name],
			"rastrillo migration generate", "rastrillo migration new",
			"Never edit a migration that has shipped",
			"`make ci`", "`make code`", "`make routes`", "`make csp`",
			"Raise the Makefile's `GOTOOLCHAIN` with each Go security",
		} {
			if !strings.Contains(agents, want) {
				t.Errorf("%s: AGENTS.md lost %q", variant.name, want)
			}
		}
		// Its sign-in keeps no *sessions.Sessions for the token page.
		if variant.name == "magicapp" &&
			strings.Contains(agents, "app.tokens.Mount") {
			t.Errorf("magicapp: AGENTS.md says to mount the token page")
		}
	}
}

// skillCode is how AGENTS.md names the code topic: in the module this app
// builds against, never another copy.
const skillCode = "$(go list -m -f '{{.Dir}}' " +
	"amadan.net/rastrillo/rastrillo)/skill/code.md"

// AGENTS.md's Code section says what SKILL.md's "Write code that reads
// like a sentence" says: the four limits word for word, the seven rules
// each starting as SKILL.md's does, and a pointer at skill/code.md.
func TestAgentsMDStatesTheCodeRules(t *testing.T) {
	t.Parallel()
	skill := strings.Join(strings.Fields(section(readFile(t,
		filepath.Join(repositoryRoot(), "SKILL.md")),
		"## Write code that reads like a sentence")), " ")
	directory := newApp(t, "plainapp")
	code := section(readFile(t, filepath.Join(directory, "AGENTS.md")),
		"## Code")
	limits, rules := itemsOf(code)
	if len(limits) != 4 || len(rules) != 7 {
		t.Fatalf("AGENTS.md's Code section has %d limits and %d rules, "+
			"want 4 and 7:\n%s", len(limits), len(rules), code)
	}
	// A limit is its numbers, so each is compared whole; a rule by the
	// claim it opens with, so its example may differ.
	for _, limit := range limits {
		if !strings.Contains(skill, limit) {
			t.Errorf("SKILL.md does not state the limit %q", limit)
		}
	}
	for _, rule := range rules {
		if opening := openingOf(rule); !strings.Contains(skill, opening) {
			t.Errorf("SKILL.md does not say %q, as AGENTS.md does", opening)
		}
	}
	if !strings.Contains(code, skillCode) {
		t.Errorf("the Code section does not point at %s:\n%s", skillCode,
			code)
	}
}

// section is the text under heading, up to the next heading of its level.
func section(document, heading string) string {
	_, after, found := strings.Cut(document, "\n"+heading+"\n")
	if !found {
		return ""
	}
	body, _, _ := strings.Cut(after, "\n## ")
	return body
}

var numbered = regexp.MustCompile(`^\d+\. `)

// itemsOf is a section's bullets and numbered items, each joined onto one
// line however it wraps.
func itemsOf(text string) (bullets, numberedItems []string) {
	var current *[]string
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "- "):
			bullets = append(bullets, line[2:])
			current = &bullets
		case numbered.MatchString(line):
			numberedItems = append(numberedItems,
				numbered.ReplaceAllString(line, ""))
			current = &numberedItems
		case strings.HasPrefix(line, "  ") && current != nil:
			(*current)[len(*current)-1] += " " + strings.TrimSpace(line)
		default:
			current = nil
		}
	}
	return bullets, numberedItems
}

// openingOf is an item up to its first comma, colon, semicolon or bracket:
// the claim itself, before any example.
func openingOf(item string) string {
	if cut := strings.IndexAny(item, ",:;("); cut > 0 {
		return strings.TrimSpace(item[:cut])
	}
	return strings.TrimSuffix(item, ".")
}
