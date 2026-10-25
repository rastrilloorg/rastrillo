. "$(dirname "$0")/lib.sh"
need "owner scoping through scope.Owned(By), the app's owned seam, or a user-scoped manifest" 'scope\.Owned(By)?\(|\.owned\(r\)|scope *= *"user"' internal manifest
need "the amount uses the framework's money kind" 'form\.Money|kind *= *"money"|money\.[A-Z]' internal manifest
refuse "no float amounts" 'float32' 'float64' -- internal manifest
exit $status
