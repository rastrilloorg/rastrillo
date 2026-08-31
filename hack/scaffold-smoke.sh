#!/bin/sh
# Scaffold an app with the built CLI, build it, run it, and prove it
# answers /healthz. The replace points at this checkout on purpose:
# this smoke proves the *generator*, not the published module. Task 6
# is what proves the published module, with no replace at all.
set -e

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
