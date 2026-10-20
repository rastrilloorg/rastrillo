# Sourced by each task's check. The checks probe discovery, not
# correctness: did the agent use the framework piece a skilled agent
# would have, or hand-roll one. Correctness rests on make ci.
#
# They look only at what the agent added against the scaffold commit
# the harness recorded (SKILLEVAL_BASE), because the scaffold already
# uses some of these features and an agent may commit or amend. Comment
# lines are dropped, so naming a feature in a TODO does not pass a
# check, and "do not use float64" does not fail one.
base=${SKILLEVAL_BASE:?the harness sets SKILLEVAL_BASE to the scaffold commit}
git add -A -N
code() { grep -vE '^[+-][[:space:]]*(//|#|\*|/\*|\{\{/\*|<!--)'; }
added() { git diff "$base" -- "$@" ':!gen' | grep '^+' | grep -v '^+++' | code; }
removed() { git diff "$base" -- "$@" ':!gen' | grep '^-' | grep -v '^---' | code; }
need() { # need <what> <regex> <paths...>
	what=$1 re=$2; shift 2
	if added "$@" | grep -qE "$re"; then echo "ok: $what"; else echo "FAIL: $what"; status=1; fi
}
# refuse fails only on a net gain, so editing a line that already held
# the word (the layout's data-theme, say) is not taken for adding it.
refuse() { # refuse <what> <regex> <paths...>
	what=$1 re=$2; shift 2
	a=$(added "$@" | grep -cE "$re"); r=$(removed "$@" | grep -cE "$re")
	if [ "$a" -gt "$r" ]; then echo "FAIL: $what"; added "$@" | grep -E "$re"; status=1; else echo "ok: $what"; fi
}
status=0
