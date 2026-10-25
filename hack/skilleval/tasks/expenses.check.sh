. "$(dirname "$0")/lib.sh"
# An --auth=magic scaffold's auth.go defines the owned seam and an
# example page calling it; an agent replacing that page would otherwise
# lose a marker for each one it adds.
skip=auth.go
need "owner scoping through scope.Owned(By), the app's owned seam, or a user-scoped manifest" 'scope\.Owned(By)?\(|\.owned\(r\)|scope *= *"user"' internal manifest
skip=
need "the amount uses the framework's money kind" 'form\.Money|kind *= *"money"|money\.[A-Z]' internal manifest
refuse "no float amounts" 'float32' 'float64' -- internal manifest
exit $status
