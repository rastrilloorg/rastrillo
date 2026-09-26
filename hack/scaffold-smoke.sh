#!/bin/sh
# Scaffold an app with the built CLI, build it, run it, and prove it
# answers /healthz. The replace points at this checkout on purpose:
# this smoke proves the *generator*, not the published module. Task 6
# is what proves the published module, with no replace at all.
set -e

# The scaffolded app is a throwaway with no history, so there is no
# version control to stamp into it — but go build looks for some anyway,
# walking up from the app's directory for a .git, and a mktemp directory
# lives wherever TMPDIR points: on a CI runner that shares the host's /tmp
# that is other processes' scratch, and one stray repository above the app
# fails the build with "error obtaining VCS status" for reasons that have
# nothing to do with the generator. This smoke asserts only that the app
# builds and answers /healthz, never on build info, so opt out of stamping
# here and only here; the CLI built by build-cli is still stamped.
export GOFLAGS="${GOFLAGS:+$GOFLAGS }-buildvcs=false"

repo=$(cd "$(dirname "$0")/.." && pwd)
bin="$repo/.build/rastrillo"
appdir=$(mktemp -d)
trap 'rm -rf "$appdir"' EXIT

cd "$appdir"
"$bin" new smokeapp
cd smokeapp
go mod edit -replace amadan.net/rastrillo/rastrillo="$repo"
go mod tidy
go build ./...
go vet ./...
go build -o smokeapp ./cmd/smokeapp

./smokeapp -addr 127.0.0.1:8199 &
server=$!
trap 'kill "$server" 2>/dev/null; rm -rf "$appdir"' EXIT

for _ in $(seq 1 20); do
	if curl -sf http://127.0.0.1:8199/healthz >/dev/null; then break; fi
	sleep 0.5
done
curl -sf http://127.0.0.1:8199/healthz
