# Changelog

Notable changes to rastrillo, newest first. Anything that can damage an app's
files or silently change its behaviour is called out first in its release,
whether or not it is the largest change.

This file starts at v0.23.0. Earlier releases are in the git history and their
tags; nothing has been reconstructed for them, because a changelog written
backwards from commits is a guess wearing a date.

## v0.25.0

Written after the fact. This release was tagged without a prep commit, so it
had no entry here and did not bump the scaffold's fallback version — see the
Fixed note below.

### Changed — the module path moved

rastrillo's repository is now `amadan.net/rastrillo/rastrillo`, and so is its
module path. An app pinning the old `github.com/carlosframework/rastrillo`
keeps building against the version it already has; to move, rewrite the path
in `go.mod` and in every import, then `go mod tidy`.

Note that v0.24.0 was prepared but never tagged, under either path. The
release before this one is v0.23.0.

### Fixed — a dev-built CLI scaffolded an unbuildable app

`rastrillo new` writes the framework version into the app's `go.mod`. A CLI
installed with `go install ...@vX.Y.Z` reads that from its own build info, but
one built from a checkout has no tag to read and falls back to a constant —
and the constant still said `v0.24.0`, a version that was never tagged. So a
scaffold from any local build failed on the first command it prints:

```
go: amadan.net/rastrillo/rastrillo@v0.24.0: reading .../go.mod at revision
  v0.24.0: unknown revision v0.24.0
```

The constant now tracks the newest tag, and two tests hold it there: one
compares it against this file's newest heading, the other against the newest
`v*` tag in the checkout. Between them a release cut without a prep commit —
which is exactly how this happened — turns the gate red.

## v0.24.0

### Fixed — read this one if you have an app scaffolded before v0.24.0

**A freshly scaffolded app failed its own `make ci`**, on the first run, before
a line of app code existed:

```
missing go.sum entry for module providing package github.com/BurntSushi/toml
  (imported by amadan.net/rastrillo/rastrillo/internal/manifest)
```

The `migration-check` target runs the CLI as `go run` from inside the app
module, so the CLI's whole package graph resolves against the app's `go.sum`.
Nothing an app imports reaches `internal/manifest`'s TOML parser, so
`go mod tidy` prunes it: a dependency that is real at `go run` time and
invisible at tidy time. The scaffolded `go.mod` now declares the CLI as a
`tool`, which is what makes tidy record it. One indirect requirement, two
`go.sum` lines — `cmd/rastrillo` does not drag in sqlc.

**Apps scaffolded before this release keep the broken `go.mod`.** Their
`make ci` fails the same way. Add the line to yours:

```
tool amadan.net/rastrillo/rastrillo/cmd/rastrillo
```

then `go mod tidy`. (`go get github.com/BurntSushi/toml` also clears it, but
names a dependency you do not use and will drift again the next time the CLI
grows one.)

Worth recording why this shipped at all. The suite *does* scaffold an app and
run `make migration-check` — but under `GOFLAGS=-mod=mod`, which the sandbox
environment sets so tests can work against a local module cache. Under
`-mod=mod` a missing `go.sum` entry is not an error: `go run` writes the
requirement into `go.mod` and carries on. So the step passed here for exactly
as long as it failed for everyone else, who runs the default `-mod=readonly`.
That step now runs `-mod=readonly` explicitly.

Reported by the Docs team.

### Added

**`auth.Config.SubjectFor`**, for an app that must not store a readable email
address at rest. `rastrillo/auth` minted every session with the verified
address as its subject, with no way to configure it away — so the address
landed in the `sessions` table and in everything keyed off the subject:
passkey credentials, challenges, pending enrollments, recovery codes. An app
whose guarantee is that publishing its database teaches an attacker nothing
could not use the family default for identity, which is the one surface where
it most wanted to.

`SubjectFor` maps the verified address to whatever the app wants the session
keyed by — an HMAC under a pepper it holds, a row id from its own directory.
Nil is exactly the previous behaviour.

Three things to know before setting it. `Authorize` still receives the real
address, because admission answers a question about an address. `Identity`
(`auth.From`, `sessions.Current`) then carries the remapped value in its
`Address` field, which is the trade being made. And an error refuses the
sign-in rather than falling back to the address — writing one is the failure
the hook exists to prevent.

It does not make the plugin server-blind on its own: `auth_links` still holds
the address at rest between sending a magic link and its click, because the
link has to survive a restart. Sealing that store is separate work.

Reported by the Oficina home team.

### Changed

**`SKILL.md` is 1,619 bytes smaller**, and says the same things. Five blocks
whose detail a documentation page already carried — the date and time field
kinds, the password plugin's configuration, the jobs handlers' mount paths,
`doctor`'s exit codes, and the shipped locale list — are now one sentence plus
their page, which is the delegation the file already promised ("rare traps get
one sentence plus a page") and had stopped doing. Every trap stayed inline, by
the rule that a block stays if getting it wrong is silent and moves to its page
if you would look it up anyway. No API changed, and the size budget did not
move.

## v0.23.0

### Fixed — read this one if you ran `rastrillo markup --fix` on v0.22.0

<!-- markup-spelling: old-spelling begin — this entry's subject IS the old
     spelling, so it names it. The gate that would otherwise flag it has the
     same blind spot as the bug the entry describes: prose about markup read
     as markup. -->

**`rastrillo markup --fix` rewrote Markdown as well as markup.** A sentence in
your documentation reading `` `class="rst-box"` `` became `` `rst-box` `` — which
destroys the explanation and leaves a diff that looks correct. If you ran it,
check `git log -p -- '*.md'` from your v0.22.0 bump before trusting those pages.

<!-- markup-spelling: old-spelling end -->

The files most likely to be hit are the ones that *teach* the class-versus-attribute
distinction, so the damage lands where the explanation lives. In this repository the
tool would have rewritten the documentation of the codemod itself, the table
recording the `rst-form-foot` rename, a correct use of `class` for a utility, and
eleven of the fifty-one design documents under `docs/superpowers/`.

`.md` is no longer scanned. A Markdown file has no markup to migrate; it has
discussion of markup, and the costs are asymmetric — an example left unmigrated is
stale, visible and harmless, while a rewritten explanation is destroyed, invisible
and plausible.

Reported by the Sheets team after upgrading.

### Fixed

**Every scaffolded app reported `dev` from `GET /api/version`, forever.** The
scaffolded `make release` built with `-ldflags="-s -w"` and no `-X`, so
`rastrillo.BuildVersion` kept its default in every binary the framework has ever
produced — while `serve.go` said the version was stamped at build time, "see
`cmd/rastrillo`". It now stamps from `git describe --tags --always --dirty`, and
refuses to build rather than shipping an empty or `-dirty` version.

This is worse than a missing version string, because of what `/api/version` is for.
`x-carlos-version` reports the release the platform believes it *adopted*; `/api/version`
reports what the process actually *running* on the instance says it is. Those are two
different facts, and only the second can tell you a process was never recycled onto a new
release. An app answering `dev` cannot supply that fact at all — so the one endpoint that
could detect a stale process is silent on every app this framework has scaffolded.

That failure has been seen: a deploy reported `live` over a process that had never been
recycled, caught only because that app's binary *was* stamped and its two answers
disagreed. A scaffolded app could not have produced that disagreement.

**Apps scaffolded before this release keep the old target.** Re-scaffold, or copy the
`VERSION` line and the `version-check` target into your Makefile. `make build` is
unchanged and still does not stamp: the compile check is not a build.

Reported by the Sheets team.

**`rastrillo markup` exited 3 on a clean tree.** The escaped-markup note ignored
fenced `old-spelling` regions, so a repository that had correctly fenced its
discussion of the old spelling could never get a green run. A report mode that
always reports is one people stop reading — which is how the `--fix` above gets
run without a second look.

**`rastrillo markup` read `<style>` inside Go comments and string literals** as an
app's stylesheet, so roughly a hundred lines of unrelated code could be reported
as selectors to change. It now scans only real string literals, via `go/scanner`.

**The design-system gallery's preview frames were unreadable on a phone.** A
1200px-wide desktop rendering was scaled to fit a phone column, which made most
previews an 18px sliver — present, and invisible. The opening view now follows the
width of the space the preview actually has rather than the width of the window,
and a desktop rendering that would fall below legibility pans instead of shrinking.

### Added

**A colour engine in `ui`**, for apps that need to generate colours rather than
pick them: `Pair` for a fill with matching ink, `Wash` for a fill that keeps the
ink a caller already has, and `Allocate` for assigning distinguishable hues to a
set of keys. Every result clears WCAG 2.2 AA by construction, and the offered set
is proven against every background it can be rendered on. Built with Sheets and
Docs, whose requirements changed the design more than once.
