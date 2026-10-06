#!/bin/sh
# One go test role of the suite.
#
#   hack/gotest.sh browser <name>   go test -tags browser, one package
#   hack/gotest.sh test <name>      go test, one package
#   hack/gotest.sh root '<split>'   go test ./... minus the split packages
#
# <name> is a package path with its slashes written as dots
# (internal.designsystem), optionally ~<k>of<n> to run only the k-th of
# n slices of its tests.
#
# A slice is dealt from the package's own test list: heaviest first,
# each onto the lightest slice so far, by the seconds in
# hack/test-weights.txt (a test not listed there weighs 1). Every shard
# computes the same deal, so the n slices of a package are a partition
# of it: each test runs in exactly one, and a test added later lands in
# a slice without anyone listing it. Two refusals keep that honest. A
# package that lists no tests is a broken build or a broken listing, not
# an empty package; and a slice dealt nothing means n is larger than the
# package, which would report green having run nothing.
#
# hack/gotest.sh --deal <mode> <pkg> <n> reads test names on stdin and
# prints "<k> <name>" for each: the deal alone, which amadan_ci_test.go
# checks is a partition without compiling anything.
set -eu

weights=$(dirname "$0")/test-weights.txt

# deal <mode> <pkg> <n>: names on stdin, "<k> <name>" out. Ties sort by
# name, so the deal does not depend on the order the names arrive in.
deal() {
	awk -v mode="$1" -v pkg="$2" -v wf="$weights" '
		BEGIN { while ((getline l < wf) > 0) { split(l, f, " "); if (f[1] == mode && f[2] == pkg) w[f[3]] = f[4] } }
		NF { print (($1 in w) ? w[$1] : 1), $1 }' |
		sort -k1,1nr -k2,2 |
		awk -v n="$3" '
		{ best = 1; for (i = 2; i <= n; i++) if (load[i] < load[best]) best = i
		  load[best] += $1; print best, $2 }'
}

case "${1:-}" in
--deal)
	deal "$2" "$3" "$4"
	exit 0
	;;
root)
	# Every package but the split ones, which run as roles of their own.
	# Each split package must be one go list names: one that was renamed
	# or deleted would otherwise drop out of both halves silently.
	all=$(go list ./...)
	keep=$all
	for p in $2; do
		ip=$(go list "./${p%@*}")
		printf '%s\n' "$all" | grep -qx "$ip" || {
			echo "gotest: $ip is split out of root but go list ./... does not name it"
			exit 1
		}
		keep=$(printf '%s\n' "$keep" | grep -vx "$ip")
	done
	# shellcheck disable=SC2086 # one argument per package, on purpose
	exec go test -count=1 $keep
	;;
browser)
	tags="-tags=browser"
	timeout=20m
	;;
test)
	tags=
	timeout=10m
	;;
*)
	echo "gotest: usage: gotest.sh browser|test <name> | root '<split>' | --deal <mode> <pkg> <n>"
	exit 2
	;;
esac
mode=$1

name=$2
rel=$name
slice=
case "$name" in
*~*)
	rel=${name%%~*}
	slice=${name#*~}
	;;
esac
rel=$(printf '%s' "$rel" | tr . /)
pkg=./$rel/

# Chromium writes a profile per browser into TMPDIR, and a /tmp shared
# by a whole machine can fill, so browser roles prefer /var/tmp. Only
# where it is writable: amadan's bwrap sandbox mounts the root
# filesystem read-only and gives each job a private tmpfs /tmp, which
# is then the right place. Not inside the checkout, ever: a scratch
# module built under it would find this repo's .git walking up.
if [ "$mode" = browser ] && [ -z "${TMPDIR:-}" ] && [ -w /var/tmp ]; then
	TMPDIR=/var/tmp
	export TMPDIR
fi

# The headless shell never restores a page from the back/forward cache,
# and ui's TestBusyButtonDrive needs one (no flag changes that; full
# Chromium of the same version does). Where `chromium` is a headless
# shell, as amadan's cloud runner links it, the packages the Makefile
# names in FULL_CHROMIUM use full Chromium for Testing from beside it
# when it is installed there. Only those: on the fleet full Chromium
# took the design system's slices from minutes to as long as twenty.
# Discovery only: a readlink without -f leaves the browser to the
# harness's own search rather than failing the role.
case " ${FULL_CHROMIUM:-} " in
*" $rel "*) full=yes ;;
*) full= ;;
esac
if [ "$mode" = browser ] && [ -n "$full" ] && [ -z "${RASTRILLO_CHROME:-}" ] &&
	shell=$(command -v chromium 2>/dev/null) && shell=$(readlink -f "$shell" 2>/dev/null); then
	case "${shell##*/}" in
	headless_shell | chrome-headless-shell)
		# The same build number as the shell: one Playwright installs both.
		build=${shell#*/chromium_headless_shell-}
		for c in "${shell%/chromium_headless_shell-*}/chromium-${build%%/*}"/chrome-linux*/chrome; do
			if [ -x "$c" ]; then
				RASTRILLO_CHROME=$c
				export RASTRILLO_CHROME
			fi
		done
		;;
	esac
fi

# Full Chromium's crash handler wants a database under its config
# directory, ~/.config by default, and will not start without one
# ("chrome_crashpad_handler: --database is required"). The cloud
# runner's HOME is read-only, so there it gets a scratch one.
# Which browser a role drove is the first question a red browser role
# raises, and nothing else in the log answers it.
if [ "$mode" = browser ]; then
	echo "gotest: on $(uname -n), driving ${RASTRILLO_CHROME:-the harness's own choice ($(command -v chromium 2>/dev/null || echo none on PATH))}"
fi

scratch=
if [ "$mode" = browser ] && [ -z "${CHROME_CONFIG_HOME:-}" ] && ! [ -w "${HOME:-/}" ]; then
	scratch=$(mktemp -d)
	CHROME_CONFIG_HOME=$scratch
	export CHROME_CONFIG_HOME
fi

# run is exec, unless there is scratch to remove after go test.
run() {
	if [ -z "$scratch" ]; then
		exec "$@"
	fi
	status=0
	"$@" || status=$?
	rm -rf "$scratch"
	exit "$status"
}

if [ -z "$slice" ]; then
	# shellcheck disable=SC2086 # an empty $tags is no argument at all
	run go test $tags -timeout "$timeout" "$pkg" -count=1
fi

k=${slice%of*}
n=${slice#*of}
# Its own command, not the head of a pipeline: a package that fails to
# build must fail here, not list nothing through a grep that succeeds.
# shellcheck disable=SC2086
listing=$(go test $tags -list '.*' "$pkg") || {
	echo "gotest: could not list the tests in $pkg:"
	printf '%s\n' "$listing"
	exit 1
}
# Examples and fuzz seeds answer to -run as well, so they are dealt too.
all=$(printf '%s\n' "$listing" | grep -E '^(Test|Example|Fuzz)' || true)
total=$(printf '%s\n' "$all" | grep -c . || true)
if [ "$total" -eq 0 ]; then
	echo "gotest: $pkg listed no tests; refusing to report a slice of nothing as a pass"
	exit 1
fi
mine=$(printf '%s\n' "$all" | deal "$mode" "$rel" "$n" | awk -v k="$k" '$1 == k { print $2 }')
if [ -z "$mine" ]; then
	echo "gotest: slice $k of $n of $pkg was dealt none of its $total tests; lower its slice count"
	exit 1
fi
echo "gotest: slice $k of $n of $pkg runs $(printf '%s\n' "$mine" | wc -l) of $total tests"
# shellcheck disable=SC2086
run go test $tags -timeout "$timeout" -run "^($(printf '%s\n' "$mine" | paste -sd'|' -))\$" "$pkg" -count=1
