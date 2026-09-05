# example-helloworld/blog/tickets/notes are deliberately NOT listed here:
# GNU Make treats a name in .PHONY as an explicit (empty) rule, which
# pre-empts the example-% pattern rule below and makes those targets
# silently no-op with "Nothing to be done" - exit 0, and the sweep never
# runs. None of the four names a real file, so the pattern rule already
# reruns unconditionally without needing .PHONY's safety here.
.PHONY: ci gofmt root chromedp-graph race generate-check scaffold-smoke browser \
        mirror mirror-check

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

BIN := $(CURDIR)/.build
EXAMPLES := helloworld blog tickets notes

# ci is the one gate: what a runner executes and what you run before
# pushing are the same definition. .amadan/ci.d/ reports these one by
# one; it never keeps its own copy of a command.
ci: gofmt root chromedp-graph race \
    example-helloworld example-blog example-tickets example-notes \
    generate-check scaffold-smoke browser

gofmt:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then \
		echo "gofmt needed on:"; echo "$$out"; exit 1; fi

root:
	go build ./...
	go vet ./...
	go test ./... -count=1

# The README promises chromedp stays out of the ordinary build graph.
# This is that sentence, executable.
chromedp-graph:
	@if go list -deps ./... | grep -i chromedp; then \
		echo "go list -deps ./... pulls chromedp - the README's promise is broken"; \
		exit 1; \
	fi

# -race needs cgo, so this one target overrides the file-wide
# CGO_ENABLED=0: the static-binary criterion is about what ships, not
# what a detector build links. Scoped to the two packages whose tests
# are actually concurrent - jobs (Start spawns a goroutine per job while
# Get reads the same map) and sessions.
race:
	CGO_ENABLED=1 go test -race -count=1 ./jobs/ ./sessions/

# The examples are separate modules with a replace back to this
# checkout, so the root sweep does not compile them - each needs its own
# ./... . blog, tickets and notes carry a `go tool sqlc` directive, so
# their first run fetches sqlc through the module proxy; that network
# access is load-bearing - do not cache it away without keeping the
# module download path working.
example-%:
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
# the design system's, and webauthn's PRF ceremonies including the
# prfByAssertion fallback. -p 1 serialises the packages - parallel
# Chromium cold-starts contend for one machine. RASTRILLO_BROWSER_OPTIONAL
# stays unset on purpose: a skip is not a pass, so a machine that loses
# its browser fails loudly instead of reporting green.
browser:
	go test -tags browser -p 1 ./harness/ ./webauthn/ ./ui/ ./internal/designsystem/ -count=1

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
# origin does not, carry it across as a branch, and land it here.
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
	echo "carry those across as a branch and land them, then: make mirror"; \
	exit 1
