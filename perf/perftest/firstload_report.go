package perftest

import (
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

	"amadan.net/rastrillo/rastrillo/internal/vendored"
)

const (
	weightMsg = "%s: %d bytes of framework CSS and JS, gzipped; " +
		"%d-byte budget"
	overMsg = "%s: %d bytes of framework CSS and JS, gzipped, " +
		"over its %d-byte budget (%s); %s"
	shellLimitMsg     = "%d bytes for a %s page"
	grewMsg           = "%s grew from %d to %d bytes"
	noneGrewMsg       = "no file grew: it loads %s"
	linkFailedMsg     = "links %s, which returns HTTP %d"
	linkErrorMsg      = "links %s, which did not load: %v"
	linkRedirectedMsg = "links %s, which redirects to %s instead of a " +
		"framework file"
)

// reportWeight says a page's weight, or fails it when it is over its
// shell's limit plus the references of the component scripts it loads.
func reportWeight(
	t testing.TB,
	screen, shell string,
	weights []fileWeight,
	budget firstLoadBudget,
) {
	t.Helper()
	total, limit := 0, budget.shells[shell]
	var components []string
	for _, weight := range weights {
		total += weight.size
		component := slices.Contains(vendored.ComponentScripts(), weight.name)
		if component && !slices.Contains(components, weight.name) {
			limit += budget.references[weight.name]
			components = append(components, weight.name)
		}
	}
	if total <= limit {
		say(weightMsg, screen, total, limit)
		return
	}
	reason := fmt.Sprintf(shellLimitMsg, budget.shells[shell], shell)
	if len(components) > 0 {
		reason += ", plus " + strings.Join(components, ", ")
	}
	t.Errorf(overMsg, screen, total, limit, reason, whatGrew(weights, budget))
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
