package designsystem

import (
	"strings"

	"amadan.net/rastrillo/rastrillo/ui"
)

// sampleRules are the rules the samples' grid cards take their columns
// from, as an app writes them in its own stylesheet: a sample sets no
// style attribute, because the CSP drops it.
var sampleRules = []struct{ hook, rule string }{
	{`class="orders"`, ui.StyleguideCSS()},
	{`class="people"`, ".people { --rst-cols: minmax(0, 1fr) 120px; }"},
}

// sampleColumns is the rule for each grid card body carries, for the
// preview document's one <style>.
func sampleColumns(body string) string {
	var rules strings.Builder
	for _, sample := range sampleRules {
		if strings.Contains(body, sample.hook) {
			rules.WriteString(sample.rule + "\n")
		}
	}
	return rules.String()
}

// withColumns opens a sample's HTML tab with the rules its grid cards
// need, so a reader who copies the markup copies those too.
func withColumns(code string) string {
	rules := strings.TrimSpace(sampleColumns(code))
	if rules == "" {
		return code
	}
	return "<!-- In your stylesheet: " + rules + " -->\n" + code
}
