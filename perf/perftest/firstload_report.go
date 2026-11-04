package perftest

import (
	"cmp"
	"fmt"
	"html"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/tdewolff/parse/v2"
	htmllex "github.com/tdewolff/parse/v2/html"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
	"amadan.net/rastrillo/rastrillo/internal/vendored"
)

const (
	weightMsg = "%s: %d bytes of framework CSS and JS, gzipped; " +
		"%d-byte budget"
	overMsg = "%s: %d bytes of framework CSS and JS, gzipped, " +
		"over its %d-byte budget (%s); %s"
	shellLimitMsg    = "%d bytes for a %s page"
	liveLimitMsg     = "%d bytes for a live %s page"
	unslicedLimitMsg = "%d bytes for a page linking the whole tokens.css"
	grewMsg          = "%s grew from %d to %d bytes"
	noneGrewMsg      = "no file grew: it loads %s"
	largestMsg       = "its largest files are %s"
	unslicedMsg      = "; it links the whole tokens.css: add " +
		"Assets: assets to ui.PagesConfig (PagesConfig.Assets) in " +
		"render.go, and in the layout use {{path $ pageStylesheet}} in " +
		"place of static/tokens.css and replace the busy.js, live.js, " +
		"light.js and anchor.js tags and the neededScripts line (or " +
		"the select.js, calendar.js and datetime.js tags) with " +
		"{{range pageScripts}}, keeping rastrillo.js and shell.js"
	unreadableMsg = "; it links the whole tokens.css because " +
		"static/tokens.css cannot be sliced: %v"
	unstyledMsg = "%s: shows %s, but its slice of tokens.css lacks a " +
		"rule for it that the whole file has, so it can render " +
		"unstyled. A page's slice holds the hooks its templates name, " +
		"not markup built in Go or by a script: ask for this hook in " +
		"the page's template with {{needs \"%s\"}}"
	unstyledReadMsg = "%s: its tokens.css cannot be read to check " +
		"the hooks it shows against its slice: %v"
	recordLimitMsg = "its record, %s line %d"
	recordHintMsg  = "; if it cannot be lighter, record " +
		"`weight %s %d <reason>` in %s: " + plusTenPercentMsg
	raiseRecordMsg = "; if it cannot be lighter, raise its record " +
		"to %d: " + plusTenPercentMsg
	plusTenPercentMsg = "its weight plus 10%%, rounded up to the " +
		"next 100"
	staleWeightUnderDefaultMsg = "%s line %d: %s weighs %d bytes, " +
		"within its %d-byte limit without a record: delete the record"
	staleWeightLowerMsg = "%s line %d: %s weighs %d bytes, under 60%% " +
		"of the %d its record allows: lower it to %d, " +
		plusTenPercentMsg
	staleWeightScreenMsg = "%s line %d: weight record for %s, which is " +
		"not a weighed screen: delete it"
	linkFailedMsg  = "links %s, which %w"
	wholeFailedMsg = "links %s, but the whole file to compare it " +
		"with, %s, %w"
	notLoadedMsg  = "did not load: %v"
	statusMsg     = "returns HTTP %d"
	redirectedMsg = "redirects to %s instead of a framework file"
)

// report is where a passing page's line goes.
var report = say

// reportWeight says a page's weight, or fails it when it is over its
// limit. A page linking the whole tokens.css is told how to slice it,
// or why it cannot be.
func reportWeight(
	t testing.TB,
	screen, shell string,
	weights []fileWeight,
	sheet pageStylesheet,
	budget firstLoadBudget,
) {
	t.Helper()
	total := 0
	for _, weight := range weights {
		total += weight.size
	}
	limit, reason := pageLimit(shell, weights, sheet, budget)
	hint := fmt.Sprintf(recordHintMsg, screen, roomFor(total),
		budgetfile.FileName)
	if record, ok := budget.records[screen]; ok {
		if stale := staleRecord(screen, total, limit, record); stale != "" {
			t.Errorf("%s", stale)
			return
		}
		limit = record.Limit
		reason = fmt.Sprintf(recordLimitMsg, budgetfile.FileName, record.Line)
		hint = fmt.Sprintf(raiseRecordMsg, roomFor(total))
	}
	note := unslicedNote(sheet)
	if total <= limit {
		report(weightMsg+"%s", screen, total, limit, note)
		return
	}
	// A slice has no reference size to have grown from: where its
	// page's bytes went is in its heaviest files.
	why := whatGrew(weights, budget)
	if sheet.sliced() {
		why = largestFiles(weights)
	}
	t.Errorf(overMsg+"%s%s", screen, total, limit, reason, why, note, hint)
}

// staleRecord says why a page's weight record should change, or is "":
// a page back within its limit needs none, and one well under its
// record lowers it, as a size record must.
func staleRecord(
	screen string, total, limit int, record budgetfile.Weight,
) string {
	switch {
	case total <= limit:
		return fmt.Sprintf(staleWeightUnderDefaultMsg,
			budgetfile.FileName, record.Line, screen, total, limit)
	case total*10 < record.Limit*6:
		return fmt.Sprintf(staleWeightLowerMsg, budgetfile.FileName,
			record.Line, screen, total, record.Limit, roomFor(total))
	}
	return ""
}

// roomFor is the weight record a page of total bytes should have, as
// every hint gives it: room for a little growth, in round numbers.
func roomFor(total int) int {
	return (total*11/10 + 99) / 100 * 100
}

// pageLimit is a page's limit, and the reason it is that: the unsliced
// limit for a page linking the whole tokens.css, its shell's live limit
// for a live page, its shell's limit otherwise, plus the references of
// the component scripts it loads and, on a live page, of live.js.
func pageLimit(
	shell string,
	weights []fileWeight,
	sheet pageStylesheet,
	budget firstLoadBudget,
) (int, string) {
	limit := budget.shells[shell]
	reason := fmt.Sprintf(shellLimitMsg, limit, shell)
	allowed := vendored.ComponentScripts()
	switch {
	case sheet.unsliced():
		limit = budget.unsliced
		reason = fmt.Sprintf(unslicedLimitMsg, limit)
	case isLive(weights):
		limit = budget.live[shell]
		reason = fmt.Sprintf(liveLimitMsg, limit, shell)
		allowed = append(allowed, liveScript)
	}
	var components []string
	for _, weight := range weights {
		component := slices.Contains(allowed, weight.name)
		if component && !slices.Contains(components, weight.name) {
			limit += budget.references[weight.name]
			components = append(components, weight.name)
		}
	}
	if len(components) > 0 {
		reason += ", plus " + strings.Join(components, ", ")
	}
	return limit, reason
}

// liveScript is the script only live and polling pages load.
const liveScript = "live.js"

// isLive reports whether a page is live or polling by whether it loads
// live.js: ui.Pages links it on exactly those pages (pageScripts), and
// it is the one sign every such page carries. The rastrillo-live meta
// and data-live regions are rendered only when the app wires a live
// hub, and data-poll only on a polling page.
func isLive(weights []fileWeight) bool {
	return slices.ContainsFunc(weights, func(weight fileWeight) bool {
		return weight.name == liveScript
	})
}

// unslicedNote says why a page links the whole tokens.css: the app's
// setup, or a stylesheet the slicer cannot read, which Assets links
// whole on every page.
func unslicedNote(sheet pageStylesheet) string {
	if !sheet.unsliced() {
		return ""
	}
	if _, err := vendored.StyledHooks(sheet.whole); err != nil {
		return fmt.Sprintf(unreadableMsg, err)
	}
	return unslicedMsg
}

// largestFiles names a page's three heaviest files, heaviest first.
func largestFiles(weights []fileWeight) string {
	heaviest := slices.Clone(weights)
	slices.SortStableFunc(heaviest, func(left, right fileWeight) int {
		return cmp.Compare(right.size, left.size)
	})
	var named []string
	for _, weight := range heaviest[:min(3, len(heaviest))] {
		name := weight.name
		if name == stylesheetName {
			name += "'s slice"
		}
		named = append(named, fmt.Sprintf("%s (%d bytes)", name, weight.size))
	}
	return fmt.Sprintf(largestMsg, strings.Join(named, ", "))
}

// reportUnstyled fails a sliced page for each hook it shows that is
// named by a rule the whole tokens.css would give it and its slice
// does not (spec 3a): the slice comes from the page's templates, so
// markup they do not hold renders unstyled, where the whole file would
// have styled it. Asking for every hook the failure names puts the
// rule in the slice. A hook no rule styles is the app's own, and fine.
func reportUnstyled(
	t testing.TB, screen string, page []byte, sheet pageStylesheet,
) {
	t.Helper()
	shown := shownHooks(page)
	if !sheet.sliced() || len(shown) == 0 {
		return
	}
	unkept, err := vendored.Unkept(sheet.whole, sheet.slice, shown)
	if err != nil {
		t.Errorf(unstyledReadMsg, screen, err)
		return
	}
	for _, hook := range unkept {
		t.Errorf(unstyledMsg, screen, hook, hook)
	}
}

// shownHooks is the hooks a page's markup carries: its attribute names
// and class names, read by the reader the slicer's hooks come from, and
// the value each hook attribute carries, so a variant the page does not
// show is not counted as unstyled. An option's data-rst-class counts as
// classes: select.js copies them onto the row it draws.
func shownHooks(page []byte) map[string]bool {
	var names []byte
	shown := map[string]bool{}
	for _, tag := range pageTags(page) {
		for name, value := range tag.attributes {
			names = append(names, name+" "...)
			if name == "class" || name == "data-rst-class" {
				names = append(names, value+" "...)
			}
			if vendored.IsHook(name) {
				shown[vendored.ShownValue(name, value)] = true
			}
		}
	}
	for _, hook := range vendored.HooksIn(names) {
		shown[hook] = true
	}
	return shown
}

// whatGrew names each file heavier than its reference, or, when none
// is, lists what the page loads: then the page loads framework files
// its shell does not.
func whatGrew(weights []fileWeight, budget firstLoadBudget) string {
	var grown, names []string
	for _, weight := range weights {
		names = append(names, weight.name)
		reference := budget.references[weight.name]
		if weight.size > reference {
			grown = append(grown,
				fmt.Sprintf(grewMsg, weight.name, reference, weight.size))
		}
	}
	if len(grown) == 0 {
		return fmt.Sprintf(noneGrewMsg, strings.Join(names, ", "))
	}
	return strings.Join(grown, "; ")
}

// pageTag is one start tag of a page and its attributes.
type pageTag struct {
	name       string
	attributes map[string]string
}

// pageTags reads every start tag of page with an HTML lexer, so script
// text, comments and quoted attribute values hide no tag and invent
// none. Names are lower case and values unquoted and decoded.
func pageTags(page []byte) []pageTag {
	lexer := htmllex.NewLexer(parse.NewInputBytes(page))
	var tags []pageTag
	for {
		token, _ := lexer.Next()
		switch {
		case token == htmllex.ErrorToken:
			return tags
		case token == htmllex.StartTagToken:
			name := strings.ToLower(string(lexer.Text()))
			tags = append(tags, pageTag{name, map[string]string{}})
		case token == htmllex.AttributeToken && len(tags) > 0:
			key := strings.ToLower(string(lexer.AttrKey()))
			tags[len(tags)-1].attributes[key] = unquoted(lexer.AttrVal())
		}
	}
}

// unquoted is an attribute value without its quotes, entities decoded.
func unquoted(value []byte) string {
	text := string(value)
	if len(text) >= 2 && (text[0] == '"' || text[0] == '\'') &&
		text[len(text)-1] == text[0] {
		text = text[1 : len(text)-1]
	}
	return html.UnescapeString(text)
}

// linkedFiles is each stylesheet and script page links, in document
// order, each once.
func linkedFiles(page []byte) []string {
	var hrefs []string
	for _, tag := range pageTags(page) {
		href := ""
		switch tag.name {
		case "script":
			href = tag.attributes["src"]
		case "link":
			rel := strings.Fields(strings.ToLower(tag.attributes["rel"]))
			if slices.Contains(rel, "stylesheet") {
				href = tag.attributes["href"]
			}
		}
		if href != "" && !slices.Contains(hrefs, href) {
			hrefs = append(hrefs, href)
		}
	}
	return hrefs
}

// baseHref is the href of the page's first <base> that has one, which
// the page's relative links resolve against, or "".
func baseHref(page []byte) string {
	for _, tag := range pageTags(page) {
		if href, ok := tag.attributes["href"]; ok && tag.name == "base" {
			return href
		}
	}
	return ""
}

var fingerprint = regexp.MustCompile(`^(.+)\.[0-9a-f]{16}(\.[^.]+)$`)

// frameworkName is the vendored file href serves, read off a
// static/<name> path with or without Assets' fingerprint, and false for
// any other file.
func frameworkName(href string) (string, bool) {
	link, err := url.Parse(href)
	if err != nil {
		return "", false
	}
	dir, file := path.Split(link.Path)
	if path.Base(dir) != "static" {
		return "", false
	}
	if match := fingerprint.FindStringSubmatch(file); match != nil {
		file = match[1] + match[2]
	}
	if !slices.Contains(vendored.Names(), file) {
		return "", false
	}
	return file, true
}

// shellOf reads a page's shell off its markup: the attribute each
// shell's layout puts on one element. A page with none of them is a
// column page.
func shellOf(page []byte) string {
	found := map[string]bool{}
	for _, tag := range pageTags(page) {
		for _, shell := range []string{"console", "sidebar", "topbar"} {
			_, ok := tag.attributes["rst-shell-"+shell]
			found[shell] = found[shell] || ok
		}
		_, stage := tag.attributes["rst-stage"]
		found["stage"] = found["stage"] || stage && tag.name == "body"
	}
	for _, shell := range []string{"console", "sidebar", "topbar", "stage"} {
		if found[shell] {
			return shell
		}
	}
	return "column"
}
