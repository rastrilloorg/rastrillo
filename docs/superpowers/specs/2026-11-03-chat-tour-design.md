# The chat tour: build a live chat app from nothing

Date: 2026-11-03. Status: design, awaiting Paul's approval.

## Intent

Rastrillo has reference docs and a one-page Getting started, but nothing
that takes a person from an empty machine to a finished app they typed
themselves. This is that: a step-by-step tour at `rastrillo.org/tour/`,
in the spirit of the Go tour and the classic "build a blog in five
minutes" demo, where the reader builds a small chat app with rooms.

Chat rather than a blog because a chat feed shows what is different
about Rastrillo: live pages, where a new message appears in everyone's
open room without a reload. A blog has nothing that needs that.

Paul will work through the tour himself to give feedback, so the first
reader is someone who knows Rastrillo's ideas but types every line by
hand. The intended reader after that is a Go-literate developer who has
never used Rastrillo.

## Goals

- A reader with Go installed goes from nothing to a running chat app by
  following the tour, copying code from it, and nothing else.
- Every step ends in a state that builds, passes its tests, and can be
  checked by eye (each step says what "it worked" looks like).
- The tour teaches the everyday loop: `rastrillo dev`, `go test`,
  `make ci`.
- The finished app has: magic-link sign-in, a list of rooms, a room page
  with a live message feed and a post form, and an admin area where
  admins create, rename and delete rooms.
- The code the tour shows can never drift from what Rastrillo actually
  does: it is real, tested code in the framework repo.

## Non-goals

- Not a reference. Each step links to the guide that covers its topic
  in depth and does not repeat it.
- No in-browser code execution or playground. The right pane is for
  reading and copying.
- No Fiesta step yet (see Constraints). The tour gains one later.
- Not a production chat product: no typing indicators, read receipts,
  editing, moderation, attachments or pagination beyond what is needed
  to keep the room page sane.
- Only one tour for now. The layout should allow a second tour later
  (a blog, say) without redesign, but nothing is built for it.

## Anti-goals

These would make it a failure even if it technically works:

- A step whose code does not compile, or whose "check it worked" does
  not happen, against the pinned release.
- Code in the tour that differs from the code in the repo's example.
- The tour working around a gap in Rastrillo. If a step needs something
  awkward, that is a framework issue to file and fix, not a trick to
  teach.
- Long walls of explanation. Each step is short: what to do, why in a
  sentence or two, how to check.

## Constraints

- **Pin one released version.** The tour names an exact Rastrillo
  release (`go install …/cmd/rastrillo@vX.Y.Z`). A scaffold made by a
  CLI built from main picked `@latest` (v0.30.2) for its `go.mod` on
  2026-11-03, which did not contain the `mcp` package the scaffold
  imports, and `go mod tidy` failed. Readers must never meet that.
- **Fiesta is not in Rastrillo yet.** Rastrillo vendoring Fiesta (part 2
  of the Fiesta work) has not landed, so a scaffolded app has
  Rastrillo's own components and no mention of Fiesta. Paul chose to
  ship now with the built-in components and add a Fiesta step once
  part 2 lands. The tour must not claim the app uses Fiesta.
- **Live pages and hibernation.** An open live stream keeps a CARLOS
  instance awake, so the optional "ship it" step must deploy to an
  always-on route, and say why in one line (see `docs/site/live.md`,
  "On CARLOS").
- **Site rules.** rastrillo.org is Eleventy, built from
  `carlosframework/rastrillo-website`. It self-hosts its fonts and
  fetches nothing from other origins. LLM-written prose on the site is
  marked with 🤖, as the docs pages already are.
- **Platforms.** Linux and macOS. Windows readers use WSL, and the
  install step says so: the scaffold's `make ci` needs Make, and
  `rastrillo routes --check` refuses other systems. (Claude's call; Paul may override.)
- **Copy.** All reader-facing prose goes through copy review before it
  ships.

## Decided

### What the reader builds

A chat app called `chat`:

- Sign-in by emailed link, from `rastrillo new chat -auth magic`. In
  dev no mailer is configured, so the link is written to the server
  log; the tour tells the reader to copy it from there.
- `/` lists rooms. A room page shows its messages, newest at the
  bottom, with a form to post one. Each message shows who sent it and
  when.
- The room page is live: a message posted in one tab appears in every
  other tab open on that room. The post form swaps in place
  (`data-live-form`). With scripts off, the rooms, the room page and
  posting still work as plain pages. Sign-in is the scaffold's as-is,
  and its proof-of-work check needs scripts; the tour does not change
  that. (Claude's call; Paul may override.)
- An admin area at `/admin/rooms` to create, rename and delete rooms.
- Admins are the signed-in users whose email is listed in an
  environment variable (`CHAT_ADMINS`, comma-separated). Everyone else
  gets 404 under `/admin/`.

### The steps

Fourteen steps (revised from twelve while building: rename and delete
got a step of their own, and the admin became three small steps).

1. Install the CLI at the pinned release.
2. Make the app: `rastrillo new chat -auth magic`.
3. Run it: `rastrillo dev`, sign in with the link from the log.
4. Run the tests the app came with.
5. Give rooms a table: the model and `rastrillo migration generate`.
6. Add rooms from an admin page (rules in `rooms.go`, actions in
   `admin.go`), open to anyone signed in for one step.
7. Only admins: the `CHAT_ADMINS` guard, 404 for everyone else.
8. Rename and delete rooms, delete behind a confirm page.
9. List the rooms on the home page, with a page per room; the
   scaffold's own home-page test is updated as part of it.
10. Post messages: the model, its migration and the form.
11. Make the room live: a topic in `app.go`, the message list as a
    region, `Notify` after posting, and the form in a region of its
    own so a sent message clears the box.
12. Test two people chatting, an empty message refused, and the admin
    pages hidden from a non-admin.
13. Run the whole gate: `make ci`, after giving the perf test a room
    and an admin.
14. Ship it (optional), behind a callout for readers without CARLOS.

### The page

The layout Paul approved on 2026-11-03 is in
`2026-11-03-chat-tour-layout.html` beside this file (open it in a
browser; fonts load from rastrillo.org). In words:

- The homepage's look: the rake mark and "CARLOS / rastrillo / tour"
  brand line, the flag row, Ibarra Real Nova headings, Chivo body,
  Chivo Mono code, the plaster palette, and the homepage's
  markup-window style for code boxes.
- Three columns. Left: the step list, collapsible to a narrow column of
  step numbers. Middle: the lesson, with Back and Next pinned to the
  bottom of that pane and the lesson scrolling above them. Right: the
  editor.
- The editor has one tab per file the step touches, each tagged NEW or
  EDIT, a path bar naming the file's location, the step's changed
  lines highlighted, and a "Copy file" button. Under it, a terminal box
  with the commands for the step and a "Copy" button.
- Each step's lesson ends with a "Check it worked" box.
- A "Project at step N (.zip)" button in the top bar downloads the
  whole app as it stands at the end of the current step.
- A progress indicator ("5 / 12") in the top bar.

### Where the code lives

The tour's code and prose live in the framework repo, so its tests
prove every step. (Revised while building, 2026-11-03: the first draft
put the finished app in a nested `examples/chat` module, but Go's
module zip leaves nested modules out, so the site, which fetches the
framework through the module proxy like `dsgen`, could never see it.)

- `tour/chat/` in the root module holds the tour: `tour.toml` (title
  and step order), one `NN-slug.md` per step (TOML front matter naming
  the step's files and commands, then the lesson), and `_files/NN/`
  holding the full text of each file the step has the reader type.
  Go ignores `_` directories, so those files never build as part of
  the framework.
- Package `tour` embeds it and replays it: scaffold with
  `rastrillo new chat -auth magic`, then per step write its files and
  run its commands, then `rastrillo generate` as `rastrillo dev`
  would.
- A test replays every step in CI and builds, vets and tests the app
  after each one, and runs the app's route, migration and generate
  checks on the last.
- `cmd/tourgen` replays it with the pinned release and writes what the
  site needs: `tour.json` and one zip per step. The site runs it at
  build time from the synced framework sha, as it runs `dsgen`.

### tour.json

```json
{
  "slug": "chat",
  "title": "Build a live chat app",
  "version": "v0.31.0",
  "steps": [{
    "number": 5,
    "slug": "rooms",
    "title": "Give chat rooms a table",
    "lesson": "markdown",
    "check": "markdown",
    "commands": ["rastrillo migration generate"],
    "files": [{
      "path": "internal/chat/models.go",
      "status": "edit",
      "language": "go",
      "content": "the whole file",
      "changed": [[12, 20]]
    }],
    "zip": "chat-05-rooms.zip"
  }]
}
```

`status` is `new` or `edit`; `changed` is 1-based inclusive line
ranges that differ from the previous step (all of a new file).

## Left to the builder

- How per-step states are stored (full copies, patches, or markers in
  one tree) and how CI checks each one. The requirement is only that
  every step builds and tests green, and that the editor and the zip
  are derived from the same source, never hand-copied.
- The prose format for lessons, and how a step names its files and
  highlighted lines.
- How the site renders the tour: Eleventy templates in the site repo,
  and whatever small script the step list collapse, tabs and copy
  buttons need. The docs pages are script-free; the homepage already
  carries a script, so a small one here is acceptable.
- How the zips are built (at site build time is the obvious place) and
  how the pinned version flows from one place into the install step,
  the example's `go.mod`, and the zips.
- Message limits: a sensible maximum length, and how many messages the
  room page shows.
- Mobile: below a sensible width the three columns stack (steps
  collapsed, lesson, then editor).
- Whether the docs nav and the homepage link to the tour, and from
  where.

### The admin is hand-written (decided 2026-11-03)

The first plan was to declare rooms in a manifest and generate the
admin. Adding the first manifest to a scaffolded app is an eight-step
manual recipe (`manifest/README.md` in the scaffold: sqlc, genassets.go,
edits to render.go, migrations.go, app.go, routes.go and the perf
test). That is plumbing a beginner should not have to type, so Paul
chose to hand-write the admin and teach one path throughout. A
framework issue asks for `rastrillo generate` to wire the first
manifest itself; once it lands, the tour can gain a "declare it
instead" step.

## Later

- A Fiesta step, once Rastrillo vendors Fiesta.
- A "declare it instead" step, once the first manifest wires itself.
- Paul's feedback from working through it.
- A second tour, if this one earns it.
