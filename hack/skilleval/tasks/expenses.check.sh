. "$(dirname "$0")/lib.sh"
need "owner scoping through scope.Owned or a user-scoped manifest" 'scope\.Owned|scope *= *"user"' internal manifest
need "the amount uses the framework's money kind" 'form\.Money|kind *= *"money"|money\.' internal manifest
refuse "no float amounts" 'float(32|64)' internal manifest
exit $status
