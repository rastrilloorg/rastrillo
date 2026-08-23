# 🤖 Web docs: rastrillo.org/docs

Design, 2026-08-23. Approved by Paul the same evening.

Rastrillo has three places a person can learn it, and none of them is
documentation. `README.md` (36 KB) is a status report — what is built,
what is not, written for someone deciding whether to adopt. `SKILL.md`
(16 KB) is an authoring contract for an agent, compressed to the point
where a trap gets three dense sentences because a fourth would not fit.
Package doc comments are excellent and invisible unless you already know
which package to open. A person who wants to know how background jobs
work reads Go source.

This builds the missing thing: a documentation site at
`rastrillo.org/docs`, extensive enough that everything the framework can
do is written down somewhere findable, and gated so that it stays true.

The model is [amadan.net/docs](https://amadan.net/docs) — markdown pages,
an explicit nav, a card index, per-page section sub-nav, and anti-rot
tests that fail the build when the docs and the code disagree. What
Rastrillo takes from it is the *gates*, not the plumbing: amadan's docs
are embedded in a Go binary because amadan.net is that binary.
Rastrillo is a library and rastrillo.org is a static site, so the
rendering is different and the discipline is the same.

## 1. Where the content lives

Markdown in **`rastrilloorg/rastrillo` under `docs/site/`**, one file
per page, plus `docs/site/nav.json` holding the table of contents.

The alternative — content in the website repo — was rejected for one
reason: the docs would drift, and we know they would, because the README
already did. Twice in the last week a release landed while the README
still claimed unbuilt something that had shipped. Documentation that
lives beside the code changes in the same pull request as the code, and
can be gated by the same CI.

The cost is that the website is now downstream of the framework repo.
That is paid with a vendoring step rather than a live dependency
(§5): the website commits a copy of the markdown, so its build never
needs the Go repo present and never fails because a submodule is
unreachable.

`nav.json` is the ordering and labelling, not the filenames:

```json
{
  "sections": [
    {
      "title": "Start here",
      "pages": [
        {"slug": "getting-started", "label": "Getting started",
         "blurb": "Install the CLI, scaffold an app, and get it serving."}
      ]
    }
  ]
}
```

A nav label is usually shorter than the page's own `# ` title
("Passkeys" vs "Passkeys and second factors"). Keeping the list explicit
rather than derived from filenames is what lets a test fail the build
over a page that exists but is unreachable, or a nav entry pointing at
nothing.

## 2. The pages

Three tiers, forty-two pages.

**Start here** (3) — `index` (the card overview), `getting-started`,
`app-shape`.

**Building an app** (17) — `data`, `migrations`, `scoping`, `forms`,
`sessions`, `passwords`, `magic-links`, `passkeys`, `jobs`, `templates`,
`icons`, `localization`, `manifests`, `assets`, `agents`, `testing`,
`deploying`.

**Reference** (22) — `cli`, then one page per exported package under a
`reference/` prefix: `reference/rastrillo`, `reference/db`,
`reference/migrate`, `reference/scope`, `reference/sessions`,
`reference/password`, `reference/auth`, `reference/passkey`,
`reference/webauthn`, `reference/csrf`, `reference/flash`,
`reference/form`, `reference/jobs`, `reference/ui`, `reference/view`,
`reference/crypto`, `reference/blobs`, `reference/eventlog`,
`reference/mail`, `reference/tools`, `reference/gormlite`.

The prefix is load-bearing rather than decorative: the guide `sessions`
and the package `sessions` are different pages with different jobs, and
a flat namespace would force one of them into a suffix like
`sessions-pkg` that reads as an apology. On disk the prefix is a
directory — `docs/site/reference/sessions.md`.

The reference tier's risk is that it becomes a worse pkg.go.dev. It
earns its place by being the *semantics* — the ordering rules, the
traps, the "never call this with that" — with §4's coverage gate
guaranteeing that no exported symbol goes unmentioned. A signature dump
would rot; a semantics page fails a test when the surface moves.

### Every page stands alone

An agent will read one page cold, with no neighbours, having followed a
pointer from `SKILL.md` (§6). So: no "as we saw above" across page
boundaries, no shared running example that only makes sense in order.
Each page restates the minimum context it needs — usually two sentences
— and links rather than assumes. Within a page, order is free.

## 3. What the docs may claim

The website's accuracy rules apply and are inherited, not restated:
every technical claim traces to this repo, and maturity is never
overclaimed. Two additions specific to docs:

- **A limit is documented where the feature is, not in a footnote.**
  Jobs die on restart. Generated mergeable ids are writer-local. A
  declared resource's schema does not evolve. Each of these belongs in
  the body of the page that teaches the feature, in the same voice as
  the feature — this is the house style already, and the docs do not
  get to relax it.
- **No page documents something that does not exist**, including as a
  "coming soon". The Not-built list is the README's job.

## 4. The gates

Go tests in this repo, so they run in the framework's own CI, in a new
`internal/docsite` package with the tests beside it.

1. **Nav and files agree both ways.** Every `docs/site/*.md` appears
   exactly once in `nav.json`; every nav entry names a file that exists.
2. **Every internal link resolves.** A `](/docs/...)` href must name a
   real page, and if it carries a `#fragment`, that page must contain a
   heading generating that anchor.
3. **Every CLI command and flag appears in `cli.md`.** Parsed from the
   usage text in `cmd/rastrillo/main.go` and the subcommand usages, so
   adding a command without documenting it fails. This is the gate that
   caught a real omission in amadan on its first run.
4. **Every exported symbol appears in its package's reference page.**
   Walk each documented package with `go/parser`, collect exported
   top-level declarations, and require each identifier to appear in the
   corresponding page. This is what makes "everything you can do with
   Rastrillo is documented" a checked claim.
5. **Every ```go fence parses.** `go/parser` over each fence, as a file
   or as a statement list, so no snippet ships syntactically broken.

Gate 4 needs an escape hatch or it will block legitimate work: a
per-page `<!-- docs:ignore Symbol reason -->` marker, which the test
requires to carry a reason. Deliberately ugly, deliberately greppable.

## 5. Rendering and sync

`hack/sync-docs.mjs` in the website repo copies `docs/site/` from a
local rastrillo checkout into `src/docs/`, writing the source sha to
`src/_data/docsversion.json`. Committed output, so the site build is
self-contained. `npm run check` fails if the vendored copy differs from
the checkout it names, when that checkout is present.

Eleventy renders `src/docs/*.md` through a `docs.njk` layout: sidebar
from `nav.json`, the current page's `##` headings nested beneath it,
card index at `/docs`. Heading anchors come from `markdown-it-anchor`
(a build-time devDependency; the served output stays static HTML).

**Zero client-side JavaScript, per the site's standing rule.** No search
box. Findability comes from the nav, the card index, and the section
sub-nav. This is a real cost — forty pages is enough that search would
help — and it is the rule the site states about itself, so it wins.

**`.md` twins.** Every page is also emitted at `/docs/<slug>.md` as raw
markdown. Nearly free from Eleventy, and it makes the network path
usable for an agent: `curl -s rastrillo.org/docs/jobs.md` is exact,
cheap, and unsummarized, where fetching the HTML page is neither.

## 6. SKILL.md pointers

`SKILL.md` is 16,034 bytes against a budget test that has been raised
twice in a week — 15,000 → 16,000 → 17,000 — each raise honestly
documented as new surface arriving against one ceiling, each saying
"trim, don't grow" before growing. The framework's surface is growing
faster than one file can absorb.

The docs are the way out, but **not** by making `SKILL.md` a table of
contents. Measured, at roughly four bytes per token: `SKILL.md` alone
costs an agent ~4,000 tokens and covers the common path completely. An
index-only `SKILL.md` would turn a standard app build into five fetches
and ~18,000 tokens. The common case must stay one file.

The win is in the tail. An agent that needs the mergeable store, the
`baseline` ledger trap, or recovery codes today either guesses or reads
Go source — `jobs/jobs.go` alone is 2,200 tokens and nobody reads only
one file. A targeted page is ~1,200 tokens and was written for the
question.

So `SKILL.md` keeps its complete common-path teaching and gains **leaf
pointers** at the places where it currently pays bytes to compress a
rare trap. New surface lands in docs and `SKILL.md` gains a line, not a
paragraph; the budget should start falling on its own.

The pattern is already proven in the family:
`carlosframework/skills`'s `building-carlos-apps` is a 14.7 KB
`SKILL.md` plus 54 KB of `references/` read on demand.

Pointers name both the in-repo path and the URL, because which one is
reachable depends on how the agent got here:

```
Full treatment: docs/site/migrations.md — https://rastrillo.org/docs/migrations
```

Mirroring pages into the `carlosframework/skills` plugin, so an agent
carrying the plugin has them as local file reads, is the natural next
step and is **out of scope here** — it is a third repo on an existing
per-release PR cadence, and it should follow the docs rather than land
with them.

## 7. Verification and deploy

Go suite green; website build and checks green; a PR on each repo,
squash-merged, per the family's never-merge-to-main rule. Then the
`ship`/`promote` runbook in the website's `CLAUDE.md`, to
`canary/rehearsal`, and verify the live edge **by content** — static
routes carry no `X-Carlos-Version`, so a curl for real page text is the
only honest check. Verify `/docs`, several pages, an `.md` twin, and a
deep link with a fragment.

## 8. Deliberately not doing

- **Search**, per §5.
- **Versioned docs.** The site documents what is released; a v0.16 vs
  v0.17 switcher is a real cost for an audience that does not exist yet.
- **Generating reference pages from godoc.** Considered; rejected. The
  generated result would be a worse pkg.go.dev and would read as
  filler. The coverage gate gets the completeness without the prose
  rot.
- **Examples as their own tier.** `examples/notes`, `blog`, `tickets`
  and `helloworld` are cited from the pages they illustrate rather than
  given a section that would duplicate their READMEs.
