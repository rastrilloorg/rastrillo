# Sourced by each task's check. The checks look only at lines the agent
# added, because the scaffold already uses some of the features they
# look for, and a check that passes on an untouched scaffold measures
# nothing.
base=$(git rev-list --max-parents=0 HEAD | tail -1)
git add -A -N
added() { git diff "$base" -- "$@" ':!gen' | grep '^+' | grep -v '^+++'; }
need() { # need <what> <regex> <paths...>
	what=$1 re=$2; shift 2
	if ! added "$@" | grep -qE "$re"; then echo "FAIL: $what"; status=1; else echo "ok: $what"; fi
}
refuse() { # refuse <what> <regex> <paths...>
	what=$1 re=$2; shift 2
	if added "$@" | grep -E "$re"; then echo "FAIL: $what"; status=1; else echo "ok: $what"; fi
}
status=0
