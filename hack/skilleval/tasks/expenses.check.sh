. "$(dirname "$0")/lib.sh"
# An --auth=magic scaffold's auth.go defines the owned seam and an
# example page calling it. Counted with the rest, an agent replacing
# that page would lose a marker for each one it adds; left out, a
# handler added there would not count. So auth.go counts on its own,
# and only what it gained.
own='scope\.Owned(By)?\(|\.owned\(r\)|scope *= *"user"'
skip=auth.go; rest=$(gained "$own" internal manifest); skip=
only=auth.go; inauth=$(gained "$own" internal); only=
[ "$inauth" -lt 0 ] && inauth=0
if [ $((rest + inauth)) -gt 0 ]; then echo "ok: owner scoping through scope.Owned(By), the app's owned seam, or a user-scoped manifest"
else echo "FAIL: owner scoping through scope.Owned(By), the app's owned seam, or a user-scoped manifest"; status=1; fi
need "the amount uses the framework's money kind" 'form\.Money|kind *= *"money"|money\.[A-Z]' internal manifest
refuse "no float amounts" 'float32' 'float64' -- internal manifest
exit $status
