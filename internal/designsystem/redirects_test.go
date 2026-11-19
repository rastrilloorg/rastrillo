package designsystem

import (
	"bufio"
	"os"
	"path"
	"slices"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
)

// oldMount is where rastrillo.org published the gallery until it moved
// to DefaultMount, and so where every address in the inventory starts.
const oldMount = "/design-system"

// publishedAddresses reads testdata/published-design-system.txt, every
// file rastrillo.org publishes under the old mount, and adds the two
// directory URLs each index.html also answers at, since those are the
// addresses a reader has bookmarked.
func publishedAddresses(t *testing.T) []string {
	t.Helper()
	file, err := os.Open("testdata/published-design-system.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var out []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
		if directory, ok := strings.CutSuffix(line, "/index.html"); ok {
			out = append(out, directory, directory+"/")
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(out) < 6000 {
		t.Fatalf("the inventory has %d addresses; it should be the "+
			"whole published tree", len(out))
	}
	return out
}

// edgeRule is one line of a _redirects file as CARLOS's edge reads it
// (internal/edge/redirects.go there), in the part of its format this
// package writes: a path source of literal segments, ":name" segments
// that capture one segment each and an optional final "*" that captures
// the rest, then a destination and a status.
type edgeRule struct {
	segments    []string
	splat       bool
	destination string
	status      string
}

// parseEdgeRules reads the lines as the edge would, and fails the test
// on a line the edge would refuse or read differently from how it is
// meant.
func parseEdgeRules(t *testing.T, lines []string) []edgeRule {
	t.Helper()
	var rules []edgeRule
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 3 || strings.HasPrefix(fields[0], "#") {
			t.Fatalf("%q: want a source, a destination and a status", line)
		}
		source := fields[0]
		if !strings.HasPrefix(source, "/") ||
			strings.ContainsAny(line, "?#%\\") {
			t.Fatalf("%q: the edge would refuse or misread it", line)
		}
		rule := edgeRule{destination: fields[1], status: fields[2]}
		trimmed := strings.Trim(source, "/")
		for _, segment := range strings.Split(trimmed, "/") {
			switch {
			case trimmed == "":
			case rule.splat:
				t.Fatalf("%q: * must be the last segment", line)
			case segment == "*":
				rule.splat = true
			case segment == "", segment == ".", segment == "..",
				strings.Contains(segment, "*"),
				segment == ":", segment == ":splat":
				t.Fatalf("%q: the edge refuses %q", line, segment)
			default:
				rule.segments = append(rule.segments, segment)
			}
		}
		rules = append(rules, rule)
	}
	return rules
}

// match reports whether the rule's source matches a request whose
// cleaned path splits into requestSegments, and what it captured.
func (rule edgeRule) match(requestSegments []string) (map[string]string, bool) {
	if len(requestSegments) < len(rule.segments) ||
		!rule.splat && len(requestSegments) != len(rule.segments) {
		return nil, false
	}
	captures := map[string]string{}
	for position, segment := range rule.segments {
		if name, ok := strings.CutPrefix(segment, ":"); ok {
			captures[name] = requestSegments[position]
		} else if segment != requestSegments[position] {
			return nil, false
		}
	}
	if rule.splat {
		rest := requestSegments[len(rule.segments):]
		captures["splat"] = strings.Join(rest, "/")
	}
	return captures, true
}

// segmentsOf is a request path as the edge matches it: cleaned, so a
// trailing slash makes no difference, and split into segments.
func segmentsOf(requestPath string) []string {
	cleaned := path.Clean(requestPath)
	if cleaned == "/" {
		return nil
	}
	return strings.Split(cleaned[1:], "/")
}

// edgeRedirect is where the first rule matching requestPath sends it,
// expanded and cleaned as the edge does it: a non-empty splat keeps
// the request's trailing slash, and a cleaned destination keeps its
// own.
func edgeRedirect(rules []edgeRule, requestPath string) (string, bool) {
	for _, rule := range rules {
		captures, ok := rule.match(segmentsOf(requestPath))
		if !ok {
			continue
		}
		splat := captures["splat"]
		if splat != "" && strings.HasSuffix(requestPath, "/") {
			captures["splat"] = splat + "/"
		}
		location := expandCaptures(rule.destination, captures)
		if strings.HasPrefix(location, "https://") {
			return location, true
		}
		cleaned := path.Clean(location)
		if strings.HasSuffix(location, "/") && cleaned != "/" {
			cleaned += "/"
		}
		return cleaned, true
	}
	return "", false
}

// expandCaptures replaces each ":name" in a destination whose name the
// source captured, and leaves any other ":" as written.
func expandCaptures(destination string, captures map[string]string) string {
	var out strings.Builder
	for start := 0; start < len(destination); {
		if destination[start] != ':' {
			out.WriteByte(destination[start])
			start++
			continue
		}
		end := start + 1
		for end < len(destination) && isPlaceholderByte(destination[end]) {
			end++
		}
		if value, ok := captures[destination[start+1:end]]; ok {
			out.WriteString(value)
		} else {
			out.WriteString(destination[start:end])
		}
		start = end
	}
	return out.String()
}

func isPlaceholderByte(character byte) bool {
	return character == '_' || '0' <= character && character <= '9' ||
		'a' <= character && character <= 'z' ||
		'A' <= character && character <= 'Z'
}

// edgeSite is rastrillo.org after the website sync, as the edge serves
// it: the tree rendered at DefaultMount, and the rules.
type edgeSite struct {
	files map[string]bool
	rules []edgeRule
}

func newEdgeSite(t *testing.T, lines []string) edgeSite {
	t.Helper()
	site := edgeSite{files: map[string]bool{}, rules: parseEdgeRules(t, lines)}
	for name := range render(t) {
		site.files[DefaultMount+"/"+name] = true
	}
	return site
}

// serves reports whether a file answers requestPath: the file itself,
// or a directory's index.html.
func (site edgeSite) serves(requestPath string) bool {
	cleaned := path.Clean(requestPath)
	return site.files[cleaned] || site.files[cleaned+"/index.html"]
}

// answer is the status the edge sends for requestPath and, for a
// redirect, where to. A file wins over every rule, and a file asked for
// as .../index.html is sent on to its directory, which is another hop.
func (site edgeSite) answer(requestPath string) (int, string) {
	if !site.serves(requestPath) {
		if location, ok := edgeRedirect(site.rules, requestPath); ok {
			return 301, location
		}
		return 404, ""
	}
	if strings.HasSuffix(requestPath, "/index.html") {
		return 301, "./"
	}
	return 200, ""
}

// landing is where an old address belongs: the same file under the new
// mount, with a pt/ stub's address at the page it stood for and an
// Overview at its directory; or, for a page that left, the Fiesta page
// that says what it said, in the same theme and language.
func landing(t *testing.T, site edgeSite, old string) string {
	t.Helper()
	rest := strings.TrimPrefix(old, oldMount)
	parts := strings.SplitN(strings.TrimPrefix(rest, "/"), "/", 3)
	if len(parts) >= 2 {
		if final, ok := rastrillo.Retagged()[parts[1]]; ok {
			parts[1] = final
			rest = "/" + strings.Join(parts, "/")
		}
	}
	if directory, ok := strings.CutSuffix(rest, "index.html"); ok {
		rest = directory
	}
	index := path.Clean(DefaultMount+rest) + "/index.html"
	if site.files[index] && !strings.HasSuffix(rest, "/") {
		rest += "/"
	}
	if site.serves(DefaultMount + rest) {
		return DefaultMount + rest
	}
	if len(parts) == 3 {
		switch {
		case parts[2] == "tokens.html":
			return FiestaPage(parts[0], parts[1], FiestaTokensPage)
		case parts[2] == "primitives.html",
			strings.HasPrefix(parts[2], "primitives/"):
			return FiestaPage(parts[0], parts[1], FiestaPrimitivesPage)
		}
	}
	t.Errorf("%s: nothing in the new tree or on Fiesta stands for it", old)
	return ""
}

// Every address rastrillo.org publishes under the old mount, pt/ stubs
// and plain asset names included, answers one 301 to where it belongs,
// and that answer is final: a file of the new tree, which no rule
// matches and which is not an index.html the edge would send on again,
// or a page in the pinned manifest of Fiesta's.
func TestEveryPublishedAddressLandsInOneHop(t *testing.T) {
	lines, err := Redirects(oldMount, DefaultMount)
	if err != nil {
		t.Fatal(err)
	}
	site := newEdgeSite(t, lines)
	manifest := map[string]bool{}
	for _, page := range fiestaManifest(t) {
		manifest[fiestaDesignSystem+page] = true
	}

	for _, old := range publishedAddresses(t) {
		status, location := site.answer(old)
		if status != 301 {
			t.Errorf("%s answers %d, want a 301", old, status)
			continue
		}
		if want := landing(t, site, old); location != want {
			t.Errorf("%s goes to %s, want %s", old, location, want)
		}
		if strings.HasPrefix(location, "https://") {
			if !manifest[location] {
				t.Errorf("%s goes to %s, which is not in "+
					"testdata/fiesta-pages.txt", old, location)
			}
			continue
		}
		if status, again := site.answer(location); status != 200 {
			t.Errorf("%s goes to %s, which answers %d %s",
				old, location, status, again)
		}
		if again, ok := edgeRedirect(site.rules, location); ok {
			t.Errorf("%s goes to %s, which a rule would send on to %s",
				old, location, again)
		}
	}

	// The plan's review focus, spelled out.
	const fiesta = fiestaDesignSystem
	for old, want := range map[string]string{
		"/design-system/plain/ja/tokens.html": fiesta + "plain/ja/tokens.html",
		"/design-system/":                     "/components/",
		"/design-system/index.html":           "/components/",
		"/design-system/day/en/":              "/components/day/en/",
		"/design-system/day/en":               "/components/day/en/",
		"/design-system/day/en/index.html":    "/components/day/en/",
		"/design-system/day/pt/":              "/components/day/pt-PT/",
		"/design-system/day/pt/index.html":    "/components/day/pt-PT/",
		"/design-system/day/pt/tokens.html":   fiesta + "day/pt-PT/tokens.html",
		"/design-system/tokens.css":           "/components/tokens.css",
		"/design-system/day/pt/primitives/idiom-box-0.html": fiesta +
			"day/pt-PT/primitives.html",
		"/design-system/plain/pt/form.html": "/components/plain/pt-PT/" +
			"form.html",
	} {
		if status, location := site.answer(old); status != 301 ||
			location != want {
			t.Errorf("%s answers %d %s, want 301 %s",
				old, status, location, want)
		}
	}
}

// No rule's source matches an address of the new tree: a file at the
// path would win anyway, but a rule that matched one would be a rule
// that is wrong about where the gallery lives.
func TestNoRuleShadowsTheNewTree(t *testing.T) {
	lines, err := Redirects(oldMount, DefaultMount)
	if err != nil {
		t.Fatal(err)
	}
	site := newEdgeSite(t, lines)
	addresses := []string{DefaultMount, DefaultMount + "/"}
	for name := range site.files {
		addresses = append(addresses, name)
		if directory, ok := strings.CutSuffix(name, "/index.html"); ok {
			addresses = append(addresses, directory+"/")
		}
	}
	slices.Sort(addresses)
	for _, address := range addresses {
		if location, ok := edgeRedirect(site.rules, address); ok {
			t.Errorf("%s, in the new tree, matches a rule to %s",
				address, location)
		}
	}
}

// The two mounts must be apart, or a rule could send the new tree to
// itself, and each must be something a rule can carry.
func TestRedirectsRefuseOverlappingOrBadMounts(t *testing.T) {
	for _, mounts := range [][2]string{
		{"/components", "/components"},
		{"/components/", "components"},
		{"/a", "/a/b"},
		{"/a/b", "/a"},
		{"/design?system", "/components"},
		{"/design-system", "/comp#onents"},
		{"/design system", "/components"},
		{"/design-system", "/comp*"},
		{"/design-system", "/:components"},
		{"/design-system", "/comp%20onents"},
		{"/", "/components"},
		{"", "/components"},
	} {
		if lines, err := Redirects(mounts[0], mounts[1]); err == nil {
			t.Errorf("Redirects(%q, %q) wrote %d rules, want an error",
				mounts[0], mounts[1], len(lines))
		}
	}
	if _, err := Redirects("/ab", "/a"); err != nil {
		t.Errorf("Redirects(%q, %q): %v; a shared prefix is not a "+
			"shared directory", "/ab", "/a", err)
	}
}

// CARLOS's edge refuses a file over its limits, and carlos ship refuses
// the whole tree, so the rules stay well inside them.
func TestTheRulesStayWithinTheEdgeLimits(t *testing.T) {
	lines, err := Redirects(oldMount, DefaultMount)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) > 2000 {
		t.Errorf("%d rules; the edge takes 2,000", len(lines))
	}
	if size := len(strings.Join(lines, "\n")) + 1; size > 256<<10 {
		t.Errorf("%d bytes; the edge takes 256 KiB", size)
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(line) > 2048 {
			t.Errorf("%q is longer than the edge's 2,048 bytes", line)
		}
		if len(fields) != 3 || fields[2] != "301" {
			t.Errorf("%q: want a source, a destination and 301", line)
		}
	}
}
