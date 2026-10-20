# strip.awk prints a file's code with its comments taken out, by the
# comment syntax of its language, so a check sees code and not what a
# comment says about it. lib.sh feeds it whole files, never diff hunks:
# a hunk can start inside a block comment whose opening it does not
# show. Input is the file's path on the first line, then its lines.
#
# Strings are read as code and skipped over, so "image/*" opens no
# comment and the // after "https://x" still starts one. Block comments
# and Go raw strings carry across lines.

NR == 1 {
	path = $0
	ext = path; sub(/.*\./, "", ext)
	nb = 0; lc = ""; quotes = ""
	if (ext ~ /^(go|js|mjs|ts|css)$/) { bo[++nb] = "/*"; bc[nb] = "*/"; bt[nb] = "" }
	if (ext ~ /^(go|js|mjs|ts)$/) { lc = "//"; quotes = "\"'`" }
	if (ext == "css") quotes = "\"'"
	if (ext ~ /^(html|tmpl|gohtml)$/) {
		# Go template comments, with or without whitespace trimming.
		bo[++nb] = "{{/*"; bc[nb] = "*/"; bt[nb] = "tmpl"
		bo[++nb] = "{{- /*"; bc[nb] = "*/"; bt[nb] = "tmpl"
		bo[++nb] = "<!--"; bc[nb] = "-->"; bt[nb] = ""
	}
	if (ext ~ /^(toml|sh|yaml|yml|py|mk)$/ || path ~ /(^|\/)Makefile$/) lc = "#"
	if (ext == "sql") lc = "--"
	inblock = ""; inraw = 0
	next
}
{
	code = strip($0)
	if (code ~ /[^[:space:]]/) print code
}

# strip returns t without its comments.
function strip(t,    r, i, k, best, which, endc, q, j, c) {
	r = ""
	while (length(t) > 0) {
		if (inraw) { # inside a backtick string, which may span lines
			i = index(t, "`")
			if (i == 0) return r t
			r = r substr(t, 1, i); t = substr(t, i + 1); inraw = 0
			continue
		}
		if (inblock != "") {
			i = index(t, inblock)
			if (i == 0) return r
			t = substr(t, i + length(inblock)); inblock = ""
			if (blocktype == "tmpl") sub(/^[ \t]*-?}}/, "", t)
			continue
		}
		best = 0; which = ""
		for (k = 1; k <= nb; k++) {
			i = index(t, bo[k])
			if (i > 0 && (best == 0 || i < best)) { best = i; which = bo[k]; endc = bc[k]; btype = bt[k] }
		}
		if (lc != "") {
			i = index(t, lc)
			# A # starts a comment only at a word's start.
			if (i > 0 && lc == "#" && i > 1 && substr(t, i - 1, 1) !~ /[[:space:]]/) i = 0
			if (i > 0 && (best == 0 || i < best)) { best = i; which = "line" }
		}
		for (j = 1; j <= length(quotes); j++) {
			q = substr(quotes, j, 1)
			i = index(t, q)
			if (i > 0 && (best == 0 || i < best)) { best = i; which = "quote"; c = q }
		}
		if (best == 0) return r t
		if (which == "quote") {
			r = r substr(t, 1, best); t = substr(t, best + 1)
			if (c == "`") { inraw = 1; continue }
			# Copy through to the closing quote, honouring backslashes.
			while (length(t) > 0) {
				ch = substr(t, 1, 1)
				if (ch == "\\") { r = r substr(t, 1, 2); t = substr(t, 3); continue }
				r = r ch; t = substr(t, 2)
				if (ch == c) break
			}
			continue
		}
		r = r substr(t, 1, best - 1)
		if (which == "line") return r
		t = substr(t, best + length(which)); inblock = endc; blocktype = btype
	}
	return r
}
