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
# A new app's templates compile: generate writes the code into the
# package that builds the pages, before anything below builds, so
# build, vet, staticcheck and the server all run with it. --check right
# after is the app's own gate passing on what generate just wrote.
"$bin" generate
gen=internal/smokeapp/rastrillo_templates_gen.go
if ! [ -f "$gen" ]; then
	echo "scaffold-smoke: rastrillo generate wrote no $gen"
	exit 1
fi
"$bin" generate --check
# The app's own line budget, with the compiled file in its package: a
# generated file must not count, or every new app is over budget after
# its first generate.
make budget
go build ./...
go vet ./...
# The app's own staticcheck target, not a copy of it: a fresh scaffold
# once failed this check on its first run (an unused test helper) while
# build and vet stayed green, because nothing ran it.
make staticcheck
make govulncheck
# Before git init the app has no history, so gitleaks has nothing to scan
# and says so. Then commit the scaffold and scan it for real: a fresh app
# must not carry anything the default rules call a secret.
make gitleaks
git init -q
git add -A
git -c user.name=smoke -c user.email=smoke@example.invalid commit -q -m scaffold
make gitleaks
# And the scaffold's .gitleaks.toml rule: an API token committed by
# mistake turns the gate red. The token is made here, at run time, so
# this file holds none for the repository's own scan to find. A gitleaks
# that failed to run at all would also exit non-zero, so the finding
# itself is what is checked.
id=$(LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 12)
secret=$(LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 43)
echo "token = \"smokeapp_${id}_${secret}\"" > leak.txt
git add leak.txt
git -c user.name=smoke -c user.email=smoke@example.invalid commit -q -m leak
if out=$(make gitleaks 2>&1); then
	echo "scaffold-smoke: gitleaks passed a committed API token"
	exit 1
fi
case "$out" in
*"leaks found: 1"*) ;;
*) echo "scaffold-smoke: gitleaks failed without finding the token:"; echo "$out"; exit 1 ;;
esac
git reset -q --hard HEAD~1
go build -o smokeapp ./cmd/smokeapp

# Shadow mode renders each compiled page through html/template too and
# fails the request on any difference, and tracing logs whether each
# render ran compiled code, so the home page served here is proved to
# be compiled code giving html/template's bytes. The log also shows
# that no template fell back as stale.
RASTRILLO_TEMPLATE_SHADOW=1 RASTRILLO_TEMPLATE_TRACE=1 ./smokeapp -addr 127.0.0.1:8199 >server.log 2>&1 &
server=$!
trap 'kill "$server" 2>/dev/null; rm -rf "$appdir"' EXIT

for _ in $(seq 1 20); do
	if curl -sf http://127.0.0.1:8199/healthz >/dev/null; then break; fi
	sleep 0.5
done
curl -sf http://127.0.0.1:8199/healthz
if ! home=$(curl -sf http://127.0.0.1:8199/); then
	echo "scaffold-smoke: the home page failed to render with compiled templates:"
	cat server.log
	exit 1
fi
case "$home" in
*"this is a rastrillo app"*) ;;
*) echo "scaffold-smoke: the home page is not the scaffold's:"; echo "$home"; exit 1 ;;
esac
if ! grep -q "tmpl: executed.*template=layout compiled=true" server.log || grep -q "compiled=false" server.log; then
	echo "scaffold-smoke: the home page did not render through compiled code:"
	cat server.log
	exit 1
fi
if grep -q "templates changed since the last rastrillo generate" server.log; then
	echo "scaffold-smoke: templates fell back to html/template as stale:"
	cat server.log
	exit 1
fi
