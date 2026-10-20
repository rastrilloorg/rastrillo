# Sourced by each task's check. The checks probe discovery, not
# correctness: did the agent use the framework piece a skilled agent
# would have, or hand-roll one. Correctness rests on make ci.
#
# They look only at code the agent added against the scaffold commit
# the harness recorded (SKILLEVAL_BASE), because the scaffold already
# uses some of these features and an agent may commit or amend.
# strip.awk takes comments out by each file's language, so naming a
# feature in a comment neither passes nor fails a check. Each changed
# file is stripped whole, old and new, and only then compared.
base=${SKILLEVAL_BASE:?the harness sets SKILLEVAL_BASE to the scaffold commit}
here=$(dirname "$0")
# A broken strip.awk would empty every pipe below and fail each need
# quietly, which reads as an agent's miss rather than the check's.
echo x | awk -f "$here/strip.awk" >/dev/null || { echo "strip.awk does not parse"; exit 2; }
git add -A -N
# stripfile <path> prints that path's code from stdin, comments out.
stripfile() { { printf '%s\n' "$1"; cat; } | awk -f "$here/strip.awk"; }
# diffcode compares each changed file's code, old and new, stripped as
# whole files, and prints removed lines as -code and added ones as +code.
diffcode() {
	git diff --no-ext-diff --no-textconv --name-only "$base" -- "$@" ':!gen' | while IFS= read -r f; do
		old=$(mktemp) new=$(mktemp)
		git show "$base:$f" 2>/dev/null | stripfile "$f" >"$old"
		if [ -f "$f" ]; then stripfile "$f" <"$f" >"$new"; fi
		diff "$old" "$new" | sed -n 's/^< /-/p; s/^> /+/p'
		rm -f "$old" "$new"
	done
}
added() { diffcode "$@" | sed -n 's/^+//p'; }
removed() { diffcode "$@" | sed -n 's/^-//p'; }
# filecode prints the current code of the named files, comments out.
filecode() { for f in "$@"; do stripfile "$f" <"$f"; done; }
need() { # need <what> <regex> <paths...>
	what=$1 re=$2; shift 2
	if added "$@" | grep -qE "$re"; then echo "ok: $what"; else echo "FAIL: $what"; status=1; fi
}
# refuse <what> <construct>... -- <paths...> fails when any one
# construct occurs more often in added code than in removed code, so
# editing a line that already held one is not taken for adding it, and
# removing one construct does not pay for adding another.
refuse() {
	what=$1; shift
	n=0
	while [ "$1" != "--" ]; do n=$((n + 1)); eval "pat$n=\$1"; shift; done
	shift
	bad=0 i=1
	while [ $i -le $n ]; do
		eval "p=\$pat$i"
		a=$(added "$@" | grep -oE "$p" | wc -l); r=$(removed "$@" | grep -oE "$p" | wc -l)
		if [ "$a" -gt "$r" ]; then echo "  $p: $r removed, $a added"; bad=1; fi
		i=$((i + 1))
	done
	if [ $bad = 1 ]; then echo "FAIL: $what"; status=1; else echo "ok: $what"; fi
}
status=0
