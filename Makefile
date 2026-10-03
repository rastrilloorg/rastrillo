# example-helloworld/blog/tickets/notes are deliberately NOT listed here:
# GNU Make treats a name in .PHONY as an explicit (empty) rule, which
# pre-empts the example-% pattern rule below and makes those targets
# silently no-op with "Nothing to be done" - exit 0, and the sweep never
# runs. None of the four names a real file, so the pattern rule already
# reruns unconditionally without needing .PHONY's safety here.
.PHONY: ci gofmt root staticcheck govulncheck gitleaks chromedp-graph gorm-free race generate-check scaffold-smoke browser \
        mirror mirror-check money

# The READMEs' documented sweeps all run with GOFLAGS=-mod=mod: the tests
# that build scratch modules (replace => this repo) rely on it to resolve
# module data the scratch go.sum does not carry. Without it, -mod=readonly
# fails every nested go build on a fresh machine.
export GOFLAGS = -mod=mod

# CGO_ENABLED=0 across the tree is an acceptance criterion, not a
# preference: static binaries, and gormlite must never quietly pull in a
# cgo driver because a C toolchain happened to be present. The race
# target is the single deliberate exception, scoped to its own command.
export CGO_ENABLED = 0

# The gate runs on one known Go, not whichever release the machine has.
# govulncheck reports the standard library's vulnerabilities for the
# toolchain doing the scan, so a runner one patch release behind would
# fail the gate for code nobody changed, and a laptop and a runner on
# different patches would disagree about what passing means. go fetches
# this release through the module proxy on first use.
#
# A plain =, not ?=: Go's own container images set GOTOOLCHAIN=local, and
# ?= would quietly keep whatever Go they ship. Override on the command
# line if you must: make ci GOTOOLCHAIN=local. Raise it with each Go
# security release; govulncheck naming a stdlib package "Found in" this
# version is the signal.
export GOTOOLCHAIN = go1.26.6

BIN := $(CURDIR)/.build

# Keep compiler/linker scratch on the checkout filesystem when shared /tmp is full.
export GOTMPDIR ?= $(BIN)/tmp
EXAMPLES := helloworld blog tickets notes

# ci is the one gate: what a runner executes and what you run before
# pushing are the same definition. .amadan/ci.d/ reports these one by
# one; it never keeps its own copy of a command.
ci: gofmt money root staticcheck govulncheck gitleaks chromedp-graph gorm-free race \
    example-helloworld example-blog example-tickets example-notes \
    generate-check scaffold-smoke browser

# The repo's own Go files, not everything under the checkout: GOTMPDIR is
# .build/tmp, and a go command killed mid-build (a Ctrl-C, or a runner
# cancelling a job a newer push superseded) leaves its generated
# _testmain.go and cgo files there. gofmt -l . walked into them and failed
# the next run on files nobody wrote. Dot-directories are pruned, the same
# rule ./... applies. It is a walk of the working tree rather than a git
# listing, so an unstaged deletion is simply absent instead of a missing
# path. NUL-delimited so a path with a space stays one argument, and a
# gofmt error (a file that does not parse) fails the target instead of
# vanishing into stderr.
gofmt:
	@out=$$(find . -name '.?*' -type d -prune -o -name '*.go' -type f -print0 | xargs -0 gofmt -l) || exit 1; \
	if [ -n "$$out" ]; then echo "gofmt needed on:"; echo "$$out"; exit 1; fi

money:
	cd money && go build ./... && go vet ./... && go test ./... -count=1

# RASTRILLO_TEST_REQUIRE_NODE turns every nodetest skip into a failure
# here: on a laptop without Node a skip is honest, but a gate that lost
# node would otherwise go green having checked none of the JavaScript
# twins. Override with RASTRILLO_TEST_REQUIRE_NODE= to run without it.
RASTRILLO_TEST_REQUIRE_NODE ?= 1
root:
	go build ./...
	go vet ./...
	RASTRILLO_TEST_REQUIRE_NODE=$(RASTRILLO_TEST_REQUIRE_NODE) go test ./... -count=1

# staticcheck catches what vet does not: deprecated APIs, values that are
# never read, and the &*x that looks like a copy and is not (SA4001 found
# exactly that in assertion's tests). staticcheck.conf at the root says
# which checks; every module below inherits it.
#
# go run with a version, not a tool directive: rastrillo is a library, and
# a tool's requirements in this go.mod would join every consuming app's
# module graph. cmd/rastrillo's staticcheckVersion is the scaffold's copy
# of this pin, and a test fails if the two disagree. Bump it alongside the
# go directive - a staticcheck older than the Go it reads does not know
# that release's deprecations, and says nothing.
#
# -tags browser because no file is excluded by it: it is a strict superset
# of the plain build, and it is the only way the browser drives get read.
# The examples' rastrillo_actions files stay out - they are generator
# input, rewritten before they compile.
STATICCHECK := honnef.co/go/tools/cmd/staticcheck@v0.7.0
staticcheck:
	go run $(STATICCHECK) -tags browser ./...
	cd money && go run $(STATICCHECK) ./...
	@for e in $(EXAMPLES); do echo "staticcheck examples/$$e"; (cd examples/$$e && go run $(STATICCHECK) ./...) || exit 1; done

# govulncheck reports only vulnerabilities this code can reach, so a
# finding is a real call path, not a dependency merely present in go.sum.
# It reads the live vulnerability database, so it can go red with no
# change here: that is the point of running it on every push. A module
# finding is fixed by raising the requirement; a standard-library one by
# raising GOTOOLCHAIN above.
GOVULNCHECK := golang.org/x/vuln/cmd/govulncheck@v1.8.0
govulncheck:
	go run $(GOVULNCHECK) ./...
	cd money && go run $(GOVULNCHECK) ./...
	@for e in $(EXAMPLES); do echo "govulncheck examples/$$e"; (cd examples/$$e && go run $(GOVULNCHECK) ./...) || exit 1; done

# The whole git history, not only the tree: a key committed and deleted
# a year ago is still in every clone, and on the public GitHub mirror.
# .gitleaks.toml allows the committed test vectors by directory and two
# single non-secrets by value. --redact keeps anything it does find out
# of the CI log.
GITLEAKS := github.com/zricethezav/gitleaks/v8@v8.30.1
gitleaks:
	go run $(GITLEAKS) git --no-banner --redact .

# The README promises chromedp stays out of the ordinary build graph.
# This is that sentence, executable.
chromedp-graph:
	@if go list -deps ./... | grep -i chromedp; then \
		echo "go list -deps ./... pulls chromedp - the README's promise is broken"; \
		exit 1; \
	fi

# The packages an app adopts to get a subsystem - its schema, its
# handlers - must not link GORM: an app that keeps its data in raw SQL
# (Tito Go is the first) would otherwise take on a second persistence
# layer to use pow or sessions. migrate is where GORM used to leak in;
# gormfn and modeldiff are the two places it is allowed to live.
GORM_FREE = ./migrate ./pow ./sessions ./blobs ./jobs ./eventlog ./auth \
            ./password ./passkey ./totp ./secondfactor ./vault ./csrf \
            ./mail ./carlos ./crypto ./flash ./form ./dbtest ./clientip ./nodetest ./background \
            ./xlsx ./table \
            ./perf ./lastsignin
# go list runs on its own line so its failure fails the target: piped
# straight into grep, a path that stopped resolving printed nothing and
# the fence passed without checking anything.
gorm-free:
	@deps=$$(go list -deps $(GORM_FREE)) || exit 1; \
	if echo "$$deps" | grep '^gorm.io/'; then \
		echo "a GORM-free package now links GORM (see GORM_FREE in the Makefile)"; \
		exit 1; \
	fi

# -race needs cgo, so this one target overrides the file-wide
# CGO_ENABLED=0: the static-binary criterion is about what ships, not
# what a detector build links. Scoped to the packages whose tests
# are actually concurrent - jobs (Start spawns a goroutine per job while
# Get reads the same map) sessions and assertion verification.
race:
	CGO_ENABLED=1 go test -race -count=1 ./jobs/ ./sessions/ ./assertion/

# The examples are separate modules with a replace back to this
# checkout, so the root sweep does not compile them - each needs its own
# ./... . blog, tickets and notes carry a `go tool sqlc` directive, so
# their first run fetches sqlc through the module proxy; that network
# access is load-bearing - do not cache it away without keeping the
# module download path working.
example-%: | $(BIN)/tmp
	cd examples/$* && go build ./... && go vet ./... && go test ./... -count=1

# Not a file target, deliberately. A stale binary from an earlier
# checkout would let generate-check and scaffold-smoke pass against a
# generator that is not the one in the tree - and .PHONY does NOT save a
# target with a directory component, which is how that shipped unnoticed
# (verified: with .build/rastrillo present, `make -n generate-check`
# omits the go build entirely).
.PHONY: build-cli
build-cli:
	@mkdir -p $(BIN)
	go build -o $(BIN)/rastrillo ./cmd/rastrillo

# generate --check is the ship gate; the second loop plus git diff
# proves the committed gen/ output still matches what today's generator
# writes.
generate-check: build-cli
	@for e in $(EXAMPLES); do $(BIN)/rastrillo generate --check examples/$$e || exit 1; done
	@for e in $(EXAMPLES); do $(BIN)/rastrillo generate examples/$$e || exit 1; done
	# Scoped to what the generator actually writes (internal/generate/generate.go's
	# single outDir, joined with "gen" by sqlcrun.go and icons.go): every committed
	# generated file lives under examples/*/gen/. An unscoped `git diff --exit-code`
	# fails on ANY uncommitted tracked change anywhere in the repo, which makes this
	# gate refuse to run on a dirty tree - contradicting AGENTS.md's "run before
	# pushing". The quoting keeps the glob for git to expand, not the shell. The
	# trailing /** is load-bearing: a pathspec containing a glob character is matched
	# with fnmatch, not treated as a directory prefix, so 'examples/*/gen' alone
	# matches nothing below gen/ - only 'examples/*/gen/**' recurses into it.
	git diff --exit-code -- 'examples/*/gen/**'

# Depends on the binary, NOT on generate-check: sharing that prerequisite
# would make step 95 re-run step 90's work and fail for step 90's reasons,
# so a red run would not say which gate actually broke.
scaffold-smoke: build-cli
	./hack/scaffold-smoke.sh

# The browser drive: the ui select journey, the harness's own checks,
# the design system's, webauthn's PRF ceremonies including the
# prfByAssertion fallback, and the sign-in screen's whole journey. -p 1
# serialises the packages - parallel Chromium cold-starts contend for
# one machine. RASTRILLO_BROWSER_OPTIONAL
# stays unset on purpose: a skip is not a pass, so a machine that loses
# its browser fails loudly instead of reporting green.
# Chromium profiles also need room when the shared /tmp tmpfs fills.
browser:
	TMPDIR="$${TMPDIR:-/var/tmp}" go test -tags browser -p 1 ./harness/ ./webauthn/ ./ui/ ./pow/ ./internal/designsystem/ ./auth/ -count=1

# origin (amadan) is where work lands; the GitHub remote is a mirror and
# nothing else. Deliberately NOT part of ci: a runner must not push, and a
# drift alarm wired into the gate would turn every branch red for
# something no branch did.
#
# The push is fast-forward only, on purpose. A commit that exists on the
# mirror and not on origin is the failure this pair exists to catch -
# #142 was squash-merged on GitHub, so its commits were never ancestors
# of anything origin had, and three pieces of SKILL.md sat there for
# days looking merged. --force would paper over exactly that, so if this
# target is rejected, do not reach for it: read what the mirror has that
# origin does not, carry it across on a branch cut from the mirror's main,
# and land that with an ordinary merge, never -squash - a squash copies the
# mirror's commits instead of adopting them, and this push stays rejected.
MIRROR_REMOTE ?= github

mirror:
	git fetch origin main
	git push $(MIRROR_REMOTE) refs/remotes/origin/main:refs/heads/main
	git push $(MIRROR_REMOTE) '+refs/amadan/ledger/*:refs/amadan/ledger/*'

# Run before branching. The two mains agreeing is the only state either
# remote is ever in; anything else means someone worked on the mirror.
mirror-check:
	@git fetch -q origin main
	@git fetch -q $(MIRROR_REMOTE) main
	@o=$$(git rev-parse refs/remotes/origin/main); \
	m=$$(git rev-parse refs/remotes/$(MIRROR_REMOTE)/main); \
	if [ "$$o" = "$$m" ]; then echo "mirror in sync: $$o"; exit 0; fi; \
	echo "MIRROR DRIFT"; \
	echo "  origin/main            $$o"; \
	echo "  $(MIRROR_REMOTE)/main  $$m"; \
	echo; \
	echo "on the mirror and not on origin:"; \
	git log --oneline refs/remotes/$(MIRROR_REMOTE)/main ^refs/remotes/origin/main | sed 's/^/  /'; \
	echo; \
	echo "carry those across on a branch cut from the mirror's main, land it"; \
	echo "with an ordinary amadan branch merge (not -squash), then: make mirror"; \
	exit 1

root staticcheck govulncheck gitleaks money chromedp-graph race build-cli browser: | $(BIN)/tmp

$(BIN)/tmp:
	mkdir -p "$@"
