# Sourced by each task's check. The checks probe discovery, not
# correctness: did the agent use the framework piece a skilled agent
# would have, or hand-roll one. Correctness rests on make ci.
#
# A check runs in a harness-owned copy of the agent's final tree and
# compares it with the harness's own copy of the scaffold
# (SKILLEVAL_BASE_DIR); neither has a .git, so nothing the agent did to
# its repository can shape the result. A construct counts as added when
# it occurs more often in the final code than in the scaffold's, summed
# over the whole tree, so renaming a file adds nothing and removing one
# construct cannot pay for another. strip.awk takes comments out by each
# file's language first, so naming a feature in a comment neither
# passes nor fails a check.
base=${SKILLEVAL_BASE_DIR:?the harness sets SKILLEVAL_BASE_DIR to its copy of the scaffold}
here=$(cd "$(dirname "$0")" && pwd)
# A broken strip.awk would empty every pipe below and fail each need
# quietly, which reads as an agent's miss rather than the check's.
echo x | awk -f "$here/strip.awk" >/dev/null || { echo "strip.awk does not parse"; exit 2; }
notests=0
# stripfile <path> prints that path's code from stdin, comments out.
stripfile() { { printf '%s\n' "$1"; cat; } | awk -f "$here/strip.awk"; }
# treecode <root> <paths...> prints the code of every source file under
# the paths, leaving out gen/ and, while notests=1, Go tests. Only source
# formats are read: a README documents syntax without using it, and a
# binary asset would turn grep's output into "binary file matches".
treecode() {
	root=$1; shift
	for p in "$@"; do
		[ -e "$root/$p" ] || continue
		find "$root/$p" -type f ! -path '*/gen/*' \( -name '*.go' -o -name '*.html' -o -name '*.tmpl' -o -name '*.gohtml' \
			-o -name '*.css' -o -name '*.js' -o -name '*.mjs' -o -name '*.ts' -o -name '*.toml' -o -name '*.sql' \) |
			while IFS= read -r f; do
				case $f in *_test.go) [ "$notests" = 1 ] && continue ;; esac
				stripfile "${f#"$root"/}" <"$f"
			done
	done
}
# gained <regex> <paths...> is how many more times the construct occurs
# in the final code than in the scaffold's.
gained() {
	re=$1; shift
	n=$(treecode . "$@" | grep -aoE -- "$re" | wc -l)
	o=$(treecode "$base" "$@" | grep -aoE -- "$re" | wc -l)
	echo $((n - o))
}
need() { # need <what> <regex> <paths...>
	what=$1 re=$2; shift 2
	if [ "$(gained "$re" "$@")" -gt 0 ]; then echo "ok: $what"; else echo "FAIL: $what"; status=1; fi
}
# refuse <what> <construct>... -- <paths...> fails when any one
# construct gained.
refuse() {
	what=$1; shift
	n=0
	while [ "$1" != "--" ]; do n=$((n + 1)); eval "pat$n=\$1"; shift; done
	shift
	bad=0 i=1
	while [ $i -le $n ]; do
		eval "p=\$pat$i"
		g=$(gained "$p" "$@")
		if [ "$g" -gt 0 ]; then echo "  $p: $g more than the scaffold"; bad=1; fi
		i=$((i + 1))
	done
	if [ $bad = 1 ]; then echo "FAIL: $what"; status=1; else echo "ok: $what"; fi
}
# present <what> <regex> <paths...> requires the construct in the final
# code at all.
present() {
	what=$1 re=$2; shift 2
	if treecode . "$@" | grep -aqE -- "$re"; then echo "ok: $what"; else echo "FAIL: $what"; status=1; fi
}
status=0
