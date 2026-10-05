#!/bin/sh
# Runs the suite's roles (the Makefile's SUITE, passed as arguments).
#
# amadan sets AMADAN_SHARD and AMADAN_SHARD_COUNT on every step, 0 and 1
# when the step is not sharded, so those two numbers are the whole
# decision:
#
#   - unset, or a count of 1: every role, in order, stopping at the
#     first failure. That is `make ci` by hand, the single-script
#     .amadan/ci fallback, and a suite step that lost its @N.
#   - a count equal to the number of roles: this shard's role, alone.
#   - any other count: a refusal. A count above the roles would leave
#     shards with nothing to do reporting green; one below would leave
#     roles that run nowhere while every shard still says ok.
set -eu

make=${MAKE:-make}
roles=$#
count=${AMADAN_SHARD_COUNT:-1}
shard=${AMADAN_SHARD:-0}

if [ "$count" -eq 1 ]; then
	for role; do
		echo "==> make $role"
		$make --no-print-directory "$role"
	done
	exit 0
fi

if [ "$count" -ne "$roles" ]; then
	echo "suite: started as $count shards, but the Makefile's SUITE has $roles roles."
	echo "suite: rename .amadan/ci.d's suite step to end in @$roles, or every role"
	echo "suite: past shard $count runs nowhere while each shard still reports ok"
	exit 1
fi

i=0
for role; do
	if [ "$i" -eq "$shard" ]; then
		echo "==> shard $shard of $count: make $role"
		exec $make --no-print-directory "$role"
	fi
	i=$((i + 1))
done
echo "suite: shard $shard is outside 0..$((count - 1))"
exit 1
