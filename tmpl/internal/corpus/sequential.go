package corpus

// sharedText is a partial a page also calls inside <script>. A set
// executed in one order or the other is the test of what compiled code
// matches: html/template's output for a template executed first on a
// fresh set. html/template on a set it shares across entries escapes p
// for HTML when p runs alone, and derives the <script> copy from that
// already-escaped p if page runs after, which escapes the value twice.
const sharedText = `{{define "p"}}{{.}}{{end}}` +
	`{{define "page"}}<script>var x = {{template "p" .}};</script>{{end}}`

func sharedPartial() Case {
	return fromText("shared-partial", "page", sharedText, "<b>", `a"b`, 3)
}
