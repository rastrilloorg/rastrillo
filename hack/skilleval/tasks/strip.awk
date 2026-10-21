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
	# Openers are regular expressions, closers plain text.
	nb = 0; lc = ""; quotes = ""; jsregex = 0; istmpl = 0
	if (ext ~ /^(go|js|mjs|ts|css)$/) { bo[++nb] = "/\\*"; bc[nb] = "*/"; bt[nb] = "" }
	if (ext ~ /^(go|js|mjs|ts)$/) { lc = "//"; quotes = "\"'`" }
	if (ext ~ /^(js|mjs|ts)$/) jsregex = 1
	if (ext == "css") quotes = "\"'"
	if (ext ~ /^(html|htm|tmpl|gohtml)$/) {
		istmpl = 1
		# Go template comments, with or without a trim marker, which
		# any whitespace may follow.
		bo[++nb] = "\\{\\{(-[ \t\r\n]+)?/\\*"; bc[nb] = "*/"; bt[nb] = "tmpl"
		bo[++nb] = "<!--"; bc[nb] = "-->"; bt[nb] = ""
		# /* */ inside an inline <style> or <script> is left in: across
		# a whole template it also matched accept="image/*" and swallowed
		# the markup after it.
	}
	if (ext ~ /^(toml|sh|yaml|yml|py|mk)$/ || path ~ /(^|\/)Makefile$/) lc = "#"
	if (ext == "sql") lc = "--"
	inblock = ""; inraw = 0; trimpend = 0
	next
}
{
	t = $0
	# A template comment may open across lines: {{- at one line's end,
	# /* at the next one's start.
	if (trimpend && match(t, /^[ \t\r]*\/\*/)) { t = substr(t, RLENGTH + 1); inblock = "*/"; blocktype = "tmpl" }
	trimpend = 0
	code = strip(t)
	if (istmpl && inblock == "" && code ~ /\{\{-[ \t\r]*$/) trimpend = 1
	if (code ~ /[^[:space:]]/) print code
}

# strip returns t without its comments.
function strip(t,    r, i, k, best, which, wlen, endc, q, j, c, ch, pre, cls, off) {
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
		best = 0; which = ""; wlen = 0
		for (k = 1; k <= nb; k++) {
			if (match(t, bo[k]) && (best == 0 || RSTART < best)) { best = RSTART; wlen = RLENGTH; which = "block"; endc = bc[k]; btype = bt[k] }
		}
		if (lc != "") {
			i = index(t, lc)
			# A # starts a comment only at a word's start.
			if (i > 0 && lc == "#" && i > 1 && substr(t, i - 1, 1) !~ /[[:space:]]/) i = 0
			if (i > 0 && (best == 0 || i <= best)) { best = i; which = "line"; wlen = length(lc) }
		}
		# In JavaScript a / where a value may start opens a regex literal,
		# whose quotes are not strings: const q = /"/ holds no string.
		if (jsregex) {
			# Each / before the earliest other candidate is tried in turn:
			# one that follows a value (or a postfix ++ or --) divides, and
			# a later one on the line may still open a regex.
			off = 0
			while ((i = index(substr(t, off + 1), "/")) > 0) {
				i += off; off = i
				if (best != 0 && i >= best) break
				if (substr(t, i, 2) == "//" || substr(t, i, 2) == "/*") break
				pre = r substr(t, 1, i - 1); sub(/[ \t]+$/, "", pre)
				if (pre ~ /(\+\+|--)$/) continue
				# A keyword's name after a dot is a property, and divides;
				# after a spread (...) it is still the keyword.
				if (pre ~ /(^|[^.])\.[ \t]*(return|typeof|case|do|else|in|of|new|delete|void|throw|yield|await)$/) continue
				# After these keywords a value starts, so / opens a regex.
				if (pre ~ /(^|[^A-Za-z0-9_$.])(return|typeof|case|do|else|in|of|new|delete|void|throw|yield|await)$/) { best = i; which = "regex"; break }
				if (pre == "" || substr(pre, length(pre), 1) ~ /[(,=:[!&|?{};+*%<>~^-]/) { best = i; which = "regex"; break }
			}
		}
		for (j = 1; j <= length(quotes); j++) {
			q = substr(quotes, j, 1)
			i = index(t, q)
			if (i > 0 && (best == 0 || i < best)) { best = i; which = "quote"; c = q }
		}
		if (best == 0) return r t
		if (which == "regex") {
			r = r substr(t, 1, best); t = substr(t, best + 1); cls = 0
			while (length(t) > 0) {
				ch = substr(t, 1, 1)
				if (ch == "\\") { r = r substr(t, 1, 2); t = substr(t, 3); continue }
				r = r ch; t = substr(t, 2)
				if (ch == "[") cls = 1
				else if (ch == "]") cls = 0
				else if (ch == "/" && !cls) break
			}
			continue
		}
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
		t = substr(t, best + wlen); inblock = endc; blocktype = btype
	}
	return r
}
