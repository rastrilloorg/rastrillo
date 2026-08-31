# Changelog

Notable changes to rastrillo, newest first. Anything that can damage an app's
files or silently change its behaviour is called out first in its release,
whether or not it is the largest change.

This file starts at v0.23.0. Earlier releases are in the git history and their
tags; nothing has been reconstructed for them, because a changelog written
backwards from commits is a guess wearing a date.

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

This is worse than a missing version string. `carlos deploy` verifies against the
router's `x-carlos-version` header — the release the platform believes it *adopted* —
while `/api/version` is what the process actually *running* on the instance says it
is. They are two different facts, and the deploy is only verified when they agree. An
app answering `dev` to the second can never disagree with the first, so a process that
was never recycled onto the new release verifies green. It did: a real deploy printed
`live` against a process that had never been recycled.

**Apps scaffolded before this release keep the old target.** Re-scaffold, or copy the
`VERSION` line and the `version-check` target into your Makefile. `make build` is
unchanged and still does not stamp: the compile check is not a build.

Found by a peer session hitting it in production.

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
