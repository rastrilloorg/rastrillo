# strip.awk reads `git diff` and prints its added (+) and removed (-)
# lines with comments taken out, by the comment syntax of each file's
# language, so a check sees code and not what a comment says about it.
# Block comments carry across lines, tracked for each side of the diff;
# context lines advance both. Strings are not parsed: a // inside a Go
# string cuts the line short, which can only hide code, never invent it.

/^diff --git / {
	path = $NF; sub(/^b\//, "", path)
	ext = path; sub(/.*\./, "", ext)
	nb = 0; lc = ""
	if (ext ~ /^(go|js|mjs|ts|css)$/) { bo[++nb] = "/*"; bc[nb] = "*/" }
	if (ext ~ /^(go|js|mjs|ts)$/) lc = "//"
	if (ext ~ /^(html|tmpl|gohtml)$/) { bo[++nb] = "{{/*"; bc[nb] = "*/}}"; bo[++nb] = "<!--"; bc[nb] = "-->" }
	if (ext ~ /^(toml|sh|yaml|yml|py|mk)$/ || path ~ /(^|\/)Makefile$/) lc = "#"
	if (ext == "sql") lc = "--"
	open["+"] = ""; open["-"] = ""
	next
}
/^(\+\+\+|---) / || /^@@/ || /^index / { next }
{
	side = substr($0, 1, 1); body = substr($0, 2)
	if (side == " ") { strip(body, "+"); strip(body, "-"); next }
	if (side != "+" && side != "-") next
	code = strip(body, side)
	if (code ~ /[^[:space:]]/) print side code
}

# strip returns t without its comments, carrying an unclosed block
# comment into the side's next line.
function strip(t, s,    r, i, k, best, which, endc) {
	r = ""
	while (length(t) > 0) {
		if (open[s] != "") {
			i = index(t, open[s])
			if (i == 0) return r
			t = substr(t, i + length(open[s])); open[s] = ""
			continue
		}
		best = 0; which = ""
		for (k = 1; k <= nb; k++) {
			i = index(t, bo[k])
			if (i > 0 && (best == 0 || i < best)) { best = i; which = bo[k]; endc = bc[k] }
		}
		if (lc != "") {
			i = index(t, lc)
			# A # starts a comment only at a word's start, and // after a
			# colon is a URL.
			if (i > 0 && lc == "#" && i > 1 && substr(t, i - 1, 1) !~ /[[:space:]]/) i = 0
			if (i > 0 && lc == "//" && i > 1 && substr(t, i - 1, 1) == ":") i = 0
			if (i > 0 && (best == 0 || i < best)) { best = i; which = "line" }
		}
		if (best == 0) return r t
		r = r substr(t, 1, best - 1)
		if (which == "line") return r
		t = substr(t, best + length(which)); open[s] = endc
	}
	return r
}
