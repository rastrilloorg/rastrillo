# Code that reads like a sentence: design

**Date:** 2026-10-29
**Status:** Approved (Paul, in conversation: "I'm good"). Routing
decided by Paul after a focus group (see "Routing: one route table").
**Branch:** `readable-code-design`
**Written by:** Claude Opus 5.5 (claude-opus-5-5), default reasoning,
from a brainstorming conversation with Paul.

## Why

Paul opened Patrix to follow what one form does and had to dig for it.
`tagSave` (`internal/patrix/access.go:184` in Patrix) is 67 lines that
create and update, run five validation rules, query for a duplicate,
re-render on error, pick between two writes, flash and redirect. The
intent ("save who may read this tag, then go back to the list") is in
there, but nothing makes it stand out.

Agents write most Rastrillo code now, and agents copy what is nearest.
So the fix is not one tidy file. It is a shape every app is generated
in, rules agents are handed before they write, and gates that catch the
rest.

Paul's words for the target: "delightfully easy to follow and parse and
read like a sentence". It should be "zen, like a gardener raking their
garden. It should promote calm, but also excitement." Excitement is
"the joy of understanding what's going on and seeing the intent 'pop'
out of the clarity of the code." What would make Paul wince: "Big files,
unclear code, overly long lines, terse sentences, overly verbose logic.
Ugliness in general."

In practice, for this design:

- **Calm** is predictability. Every action has the same shape, so after
  reading one you can read them all. Nothing important hides behind an
  option or a convention you have to look up.
- **Excitement** comes from the same clarity: once the plumbing is out
  of the way, the intent is what is left on the screen.

## The proof it works

A throwaway rewrite of Patrix's `tagSave` (kept at
`~/.cache/action-spike` during the conversation; not committed) built,
passed `go vet` and passed Patrix's 12 access and protected-section
tests. One of the two actions that replaced it:

```go
func (app *app) tagUpdate(
	response http.ResponseWriter, request *http.Request,
) {
	submitted := tagForm(request)
	tagName := chi.URLParam(request, "tag")
	tag, err := access.UpdateTag(app.db, app.now(), tagName, submitted)
	if app.refusal(response, request, err, showAgain) {
		return
	}
	flash.Set(response, "notice", "Saved who may read "+tag.Tag+".")
	http.Redirect(response, request, "/admin/access", http.StatusSeeOther)
}
```

(Simplified for the page: the spike built `showAgain` inline, which is
what pushed one line to 99 columns. It compiled against the older
Rastrillo Patrix pins, whose `flash.Set` takes no request; today's
does.)

**What the spike does not prove.** It leaves out mounting, the form
parsing and the re-show helpers, and it has no money field. The whole
flow, not just the action, has to meet the limits. Step 1 of the work
proves it on a generated money form and on Patrix's tag form before any
gate enforces the shape.

What the spike taught, and this design keeps:

- **A refusal helper, with success written out.** `if app.refusal(...)
  { return }` is plain Go. An options style (`door.Redirect(...)`)
  would have hidden the line a reader most wants to see.
- **One route per intent.** The old handler served create and update,
  so it branched on `isNew` in six places. Splitting the route removed
  them all; shortening alone would not have.
- **The code moves, it does not vanish.** 67 lines became about 16 of
  action, 25 of form reading and re-showing, and 85 of operation. The
  rules now live in the `access` package, where its doc already said
  they belong.
- **Re-showing the form is the awkward part.** It needed a function
  that returns a function. That is what the framework should absorb.
- **`response, request` travels in pairs** and is what breaks the
  80-column limit. See "One value for the exchange" below.

## Goals

1. Any route, in the framework, in generated code or in an app, reads
   as a short sequence of named steps a person or agent can follow at a
   glance.
2. The rules are written where agents load them before writing code:
   Rastrillo's `SKILL.md`, a `skill/` topic, and the `AGENTS.md` every
   `rastrillo new` app is scaffolded with.
3. Everything a machine can check is checked by a gate, in Rastrillo's
   own gate and in every scaffolded app's `make ci`.
4. Anyone using the CARLOS skill (`carlosframework/skills`) inherits the
   rules without that repo restating them.
5. Patrix's largest handlers are rewritten in the shape, as the proof.

## Non-goals

- **A new syntax or transpiler.** Paul's first sketch
  (`import("action", ...)`, `params dict`) is not Go. Supporting it
  would cost gopls, `go vet`, the debugger and readable stack traces.
  Everything here is plain Go.
- **Untyped input.** A `params dict` would undo `op.Opt`, which keeps
  "absent" and "sent as zero" apart. Inputs stay typed.
- **Behaviour changes,** with two exceptions listed under "The refusal
  helper". Otherwise renames and reshapes keep every response, status
  code and stored row the same, and existing tests are the check.
- **Deciding the future of manifests.** Paul's next question was
  whether manifests should go too, with generators that write code an
  app owns. That is the next design, and it takes this one as its
  target. Here the generator only changes what it emits.

## Anti-goals

It fails, even if every gate passes, if:

- the code is shorter but harder to follow, because control flow moved
  into helpers and options a reader has to look up;
- spelled-out names make lines so long that they wrap or need a second
  look;
- the rules become a mini-language of options that agents misuse;
- comments grow to explain what code that reads well would show.

## The rules

These go to agents, word for word where possible. Rules 1 to 7 came out
of the conversation; 8 to 14 are recommendations Paul accepted.

1. **Spell names out, everywhere, except `err`, `ctx` and Go's
   testing values (`t`, `b`, `f`).** `response`, not `w`;
   `request`, not `r`; `app`, not `a`; `submitted`, not `in`; `tag`,
   not `t`. `err` stays because every Go reader knows it, and `error`
   would hide Go's built-in error type. `ctx` stays too, because
   `context` is the package. `t *testing.T` and its siblings
   stay for the same reason as `err`. Type parameters may be one
   capital (`Opt[T]`). Names cost nothing at run time.
2. **Name things for what the person is doing, not how the code does
   it.** Patrix's `saveEntry` becomes `entries.Publish`,
   `entries.Update`, `entries.Unpublish` and `entries.SaveDraft`, one
   per button it has today. Scheduling and backdating stay part of
   Publish, decided by the time submitted, exactly as now.
3. **One route, one intent.** No `isNew` or `intent` switches inside a
   handler. Two buttons that mean two things post to two routes; in one
   form, each button's `formaction` sends it to its own route, with no
   JavaScript.
4. **Every form action has four beats:** read the input, run one
   operation, answer a refusal, say what success looks like. Nothing
   else goes in an action. Input that does not parse is a refusal from
   the first beat and never reaches the operation (see "Showing the
   form again").
5. **Operations hold the rules and the database work.** They return the
   `op` refusals (`Invalid`, `NotFound`, `Forbidden`, `Conflict`) and
   never touch HTTP, so they can be tested without a request.
6. **A helper answers refusals; success stays written out** in the
   action, because it is the line a reader most wants to see.
7. **Comments say why, in one or two full sentences.** Why something
   deliberately looks wrong, or a rule that spans files. Never what the
   code does. Long reasoning goes in the spec or the commit, and the
   comment can point there. This tightens `AGENTS.md`'s existing
   "Comments" rule rather than replacing it.
8. **One way to do each thing.** Agents copy the nearest example, so
   three patterns means a random pick. For routing, the one way is the
   route table (see "Routing: one route table").
9. **One canonical example app follows every rule,** and the guidance
   says "write it like `examples/blog`". Agents imitate examples more
   reliably than they follow rules.
10. **Names you can grep and land in one place.** `tagUpdate` and
    `access.UpdateTag` each find one definition; a function called
    `Handle` in every action file finds dozens. No `util` packages.
11. **Explicit over magic.** No `init()` registration, reflection-based
    dispatch or struct-tag mini-languages where plain code would do.
    Go-to-definition must work in an editor and with grep.
12. **Types that rule out mistakes.** `op.Opt` is the model. Typed IDs
    (`TagName`, not `string`) are the next step where a mix-up is
    plausible.
13. **Error messages say the fix:** what is wrong, where, and what to
    change, the way `generate --check` already does.
14. **Tests are named as sentences and run fast.** Patrix already does
    this (`TestTheAccessScreenMapsATagToRoles`, 0.6 s for the access
    tests).

## The limits

| Wince | Limit | Checked by | Today |
|---|---|---|---|
| Long lines | 80 columns, a tab counted as 4 | gate | 5 to 6% of lines over, nearly all code |
| Big files | 400 lines, one topic per file | gate | Rastrillo 52 of 455 files over; Patrix 16 of 89 |
| Long functions | an action 15 lines; anything else 40 | gate | Patrix's `saveEntry` is 192 |
| Unclear names | no single-letter names except `err`, `t`, `b`, `f` and type parameters | gate | everywhere |
| Terse sentences | comments and messages are full sentences | review | |
| Verbose logic | no wrapper that only forwards, no unused option | review | |

The "today" figures count non-test Go on `origin/main` of both repos on
2026-10-29, excluding generated code.

Data files are exempt from the file limit (Rastrillo's 5,474-line
`internal/designsystem/prose.go` is sample text), and from the columns
limit, because a table's entries are long strings that wrapping makes
worse (added while planning step 2). A data file says so with
`//rastrillo:data <reason>` before its package clause. Tests are held to the
naming and line rules but not the function limit, since a table-driven
test is long by nature.

**The gates ratchet.** Each of today's offenders is recorded with its
current size as its ceiling. A new offender fails; an old one that grows
past its ceiling fails; one that shrinks has its ceiling lowered, and one
that fits the limit leaves the list. A move or a split is a new entry,
so it must fit the limit. Rastrillo already ratchets this way for
directory sizes (`internal/budgetsize`), and `blueprint.md` applies the
same rule to the JavaScript module cap ("Caps only ratchet down").
Nobody has to fix every file before the gate can land.

## The action shape in Rastrillo

### The refusal helper

`view.Refuse` already answers `NotFound` with the 404 page, `Forbidden`
with the 403 page and anything else with a logged 500. It treats
`Invalid` and `Conflict` as 500s, because it has no form to show them
on. The new helper takes a way to show the form again and handles all
four:

- `Invalid`: show the form again, with the field messages, at 422.
  That is what `skill/forms.md` teaches and what Patrix answers.
  Generated actions answer 400 today, so they change, with the blog's
  tests (**behaviour change 1**).
- `Conflict`: show the form again with the message, at 409. Today it
  reaches `view.Refuse` and becomes a 500 (**behaviour change 2**).
  The API's 409 is untouched.

The generated money edit action's handling of a currency `Conflict`
(`internal/generate/actions.go`, around line 1036) stays where it is.
It re-reads the record and re-parses the original submission in the
new currency, which is form work, not an operation's; an operation
handed already-parsed amounts could not do it without changing their
meaning. It runs before the refusal helper, which sees only a conflict
it could not resolve.
- `NotFound`: the 404 page.
- `Forbidden`: the 403 page, reason logged.
- anything else: the logged 500.

It returns whether it answered, so the action reads
`if refusal(...) { return }`. A nil error is not a refusal.

It is **two calls, never one call with a nil**: one for an action with a
form to show again, one for an action without (a delete, say). Five of
the seven focus-group panelists stopped at `Refused(err, nil)` to ask
what the nil meant. The names are the builder's, judged by how the call
site reads in an action.

### Showing the form again

The spike's `tagFormAgain` (a function that returns a function) is the
part that read worst. The framework should make "show this form again
with what was submitted and these errors" one value the action can hand
to the helper. The contract:

- **Input that does not parse never reaches the write.** Reading the
  form returns its field errors as a refusal the same helper answers,
  so the action stops before the operation.
- **A broken request is not a form error.** A body that cannot be read
  or is too large keeps today's answer (400 in generated actions, 413
  in Patrix's editor), straight from reading the form, never the 422
  re-show. The complete examples in step 1 show this early return.
  A hand-written form that today ignores a broken body, as Patrix's tag
  form does, starts answering 400 (**behaviour change 3**, found while
  planning step 1).
- **Parse errors and the operation's validation show together,** as
  generated actions do today by merging `p.Errors()` with the
  operation's `Validate` result. A person fixes everything in one pass.
- **The re-show carries everything a field needs,** not just the
  echoed text: for money that is the error arguments, the verified
  seed, the locale and whether to reseed (`form/moneyfield.go`). The
  parsed form, not `p.Echo()` alone, is its source.

### One value for the exchange

`response, request` (and in `actions/`, `ctx` as well) travel together
into every call, and they are what push a short action past 80 columns.
Rastrillo should offer one value carrying both, so the action above
reads:

```go
func (app *app) tagUpdate(visit *rastrillo.Visit) {
	submitted := tagForm(visit)
	tagName := visit.PathValue("tag")
	tag, err := access.UpdateTag(app.db, app.now(), tagName, submitted)
	if visit.RefusedShowing(err, showAgain) {
		return
	}
	visit.Flash("Saved who may read " + tag.Tag + ".")
	visit.Redirect(paths.AccessList())
}
```

`Visit`, `RefusedShowing` and `paths` are placeholders, not decisions,
and so is the `rastrillo.` prefix. Constraints on `Visit`:

- **`Ctx` survives, inside it.** A `Visit` wraps the per-request
  `*rastrillo.Ctx` the router already builds, and hands it to
  operations and render callbacks, which keep taking `*rastrillo.Ctx`
  unchanged. App code never reads the database off it.
- **It lives in a new package above `view` and `op`** (call it `action`).
  `action` imports `rastrillo`, `view`, `op` and `form`; none of them
  imports `action`, so there is no cycle. Named `action`, not `page`,
  by Paul after the step 1 proofs: `page` collided with existing
  identifiers in both apps that adopted it. Its
  methods (`Refused`, `Flash`, `Redirect`) call the existing `view`
  and `flash` functions, which keep their `(ctx, w, r)` and `(w, r)`
  signatures.

- It wraps `http.ResponseWriter` and `*http.Request` and exposes both,
  so nothing in `net/http` is out of reach.
- A chi route can mount a handler written against it with one adapter,
  so Patrix and every scaffolded app can adopt it a route at a time.
- It carries no state of its own beyond the request's lifetime, and no
  app dependencies. The database and the clock stay on the app
  (`app.db`, `app.now()`); a `visit.DB` turns the request into a bag
  of everything, which the panel winced at. Generated code gets its
  dependencies the same way, passed to its mount function.
- It does carry what `Ctx` carries per request today: the logger, the
  render and error-page callbacks, the actor and the authorization
  policy. The router keeps doing what it does now when it builds that
  per-request copy: deriving the actor and checking the database passed
  `gen.Boot`.
- Its methods are a short list. If it grows past what an action needs,
  it has become the mini-language this design is against.

**Where it replaces the pair (my call).** `Visit` is the one shape
taught for code an app writes: actions, the scaffold, generated code,
`examples/blog` and every `skill/` topic. What an action needs from
`view.Render`, `view.Refuse` and `flash.Set` it reaches through
`Visit`'s methods; those functions stay as they are, documented as the
`net/http` layer underneath.
Framework handlers mounted as plain `http.Handler`s (`auth`,
`password`, `apitoken`, middleware) stay on `net/http`: an app mounts
them, it does not copy them. Nothing is removed in this work, so no
app breaks on upgrade.

## Where the rules live

| Place | What changes | Who it reaches |
|---|---|---|
| `SKILL.md` | A short "Code that reads like a sentence" section: rules 1 to 7 and the limits, in a few lines, pointing at the topic. The "App shape" and "Owner scoping" examples are rewritten in the new names (`app.owned(request)`, not `a.owned(r)`). | Every agent writing a Rastrillo app |
| `skill/code.md` (new topic) | All fourteen rules with one example each, the action shape, the operation shape, and what each gate failure means. Added to the topic table. | Agents, before writing a handler |
| Scaffolded `AGENTS.md` | The rules, so an app carries them with its own code. | Every `rastrillo new` app, for life |
| Scaffold templates | `handlers.go`, `app.go` and `render.go` written in the new names and shape, so the first thing an agent copies is right. | Every `rastrillo new` app |
| Scaffolded `make ci` | The four gates, via a `rastrillo` subcommand the app calls. | New apps from the start; existing apps when they opt in (below) |
| Generated code | The generator emits the new shape, on the route table. | Every manifest resource |
| `examples/blog` | Rewritten as the canonical garden. | What agents imitate |

**Existing apps opt in (my call).** Upgrading Rastrillo does not add
the gates to an app's `make ci`, so nobody's CI turns red on upgrade.
A command of its own (say `rastrillo gates init`, not `doctor --fix`,
which means "repair vendored assets" and is run during ordinary
upgrades) adds the target and records the app's starting ceilings. It
refuses to overwrite ceilings that already exist, so it cannot be used
to wave new violations through. The CHANGELOG says how. Patrix's step
starts with it.

`SKILL.md` is byte-budgeted (`skillmd_test.go`). Per `AGENTS.md`, if
the new section does not fit, trim redundant prose or raise the ceiling
in the test with the reason; never cut a load-bearing fact to fit.

### The CARLOS skill

`carlosframework/skills` already tells agents that Rastrillo's
`SKILL.md` is "the authoritative authoring doc" and to read it "before
writing any app code" (`skills/building-carlos-apps/references/rastrillo.md`).
So users of that skill inherit the rules through `SKILL.md` without a
copy. The change there is small:

- one line in `references/rastrillo.md` naming the new section, so an
  agent reading only the CARLOS skill knows the rules exist;
- a version bump in `.claude-plugin/plugin.json` (0.14.0 today), and the
  fresh-agent retrieval test its `AGENTS.md` requires before publishing.

That repo is on GitHub and public. Its change is a separate pull
request, opened only once Paul has approved the wording.

`blueprint.md` says its JavaScript cap is "carried into `carlos vet`".
If the Go gates should run there too, that is a request to the platform
(carloku's `carlos` CLI), not something this work builds around it.

## Order of work

Each step lands on its own and leaves everything working. The shape is
proved before anything enforces it.

1. **The action shape, proved.** The exchange value, the two refusal
   calls, the form re-show contract, and mount functions on the route
   table, with tests. The proof is two complete, compiling examples,
   mounting to redirect, measured against the limits: a generated money
   edit form (tickets' `ticket_types`), and Patrix's tag form.
2. **Rules and Rastrillo's own gates.** `skill/code.md`, the `SKILL.md`
   section, and the four gates with their ceilings in Rastrillo's gate
   (a new suite role, per its sharded gate). Not yet in the scaffold.
3. **Named paths and `rastrillo routes`.**
4. **The scaffold, all at once.** Templates rewritten in the new names
   and shape, the rules in its `AGENTS.md`, and the gates in its
   `make ci`, landing together: the templates today break the naming
   rule (`a`, `w`, `r`), and Rastrillo's tests run every scaffolded
   app's gate.
5. **The generator.** The `actions/` bridge, generated code in the new
   shape on the route table, the parity lint on handler methods, 422
   for invalid forms, and the deprecation warning.
6. **The canonical example.** `examples/blog` and `examples/helloworld`
   moved to the route table and rewritten; the guidance points at them.
7. **The framework pass.** Rename and split Rastrillo's own code,
   lowering the ceilings. This can be many small branches.
8. **The CARLOS skill line**, once step 2 has landed.
9. **Patrix, as the proof**, on a Patrix branch once it pins a
   Rastrillo with steps 1 to 3. It starts with `rastrillo gates init`.
   Then `tagSave` as in the spike, and `saveEntry` split by the table
   below. Then the other handlers over 40 lines (`settingsSave`,
   `redirectSave`, `invite`, `storeUpload`, `renderDashboard`,
   `personSave`). Patrix already uses a route table, so it moves its
   routes into per-area mount functions as it goes.
10. **Next design: manifests and generators.** Whether manifests are
    deprecated in favour of generators that write code an app owns.

`saveEntry` today, split by button (each keeps today's rules exactly):

| Button | Operation | What it does with the time |
|---|---|---|
| Publish | `entries.Publish` | empty means now; future schedules; past backdates |
| Update (any published entry, live or scheduled) | `entries.Update` | on a live entry, empty keeps the date it went live; on a scheduled one, empty publishes now; a time given reschedules or backdates |
| Unpublish | `entries.Unpublish` | back to draft, keeping the time for later |
| Save draft | `entries.SaveDraft` | a draft keeps any time it was given |

The buttons stay in one form, each with its own `formaction`, so the
editor works without JavaScript as it does now.

## Routing: one route table

**Decided by Paul:** the route table is the one routing style, and
`actions/` file routes are deprecated.

### Why

Rastrillo had two: `rastrillo new` scaffolds chi routes and handler
methods (Patrix's 144 routes work this way), while the manifest
generator and `examples/blog` use `actions/` files
(`posts/[id]/publish.POST.go`), inspired by Next.js.

A focus group settled it. Seven panelists (a Next.js developer new to
Go, a senior Go engineer, a Rails developer, a junior on a first Go
project, an AI coding agent, an agency tech lead, and a polyglot indie
developer run on GPT-6 Astra) were given the same feature built both
ways, half of them in the opposite order. All seven picked the route
table; four strongly. Average scores out of 5:

| | Route table | File routes |
|---|---|---|
| Finding the code for a URL | 4.0 | 5.0 |
| Seeing every route and who may reach it | 5.0 | 2.1 |
| Reading one action | 4.3 | 4.1 |
| Adding a route | 4.1 | 4.0 |
| Trusting an AI agent to follow the pattern | 4.6 | 2.6 |

File routes won on finding code by URL. What sank them was how
Rastrillo implements them, named by every panelist: compile errors and
stack traces point at copies under `gen/` (several predicted someone
would edit the copy); a build tag hides the real files from `go build`,
`go test` and the editor; every function is called `Handle`, so search
is useless; who may reach a route is decided outside the tree; and
helpers cannot sit beside the actions that share them. The panelists
were AI personas, not people, and the material was written by the
author of this design, who had leaned towards the route table without
saying so in the brief.

### The route table's shape

The panel also found the route table's weak spots. The shape answers
them:

- **One mount function per area, in that area's file.** Access routes
  live in `mountAccess` at the top of `access_tags.go`, not in one
  long `routes.go` that every branch edits. The top-level table only
  calls the mount functions, so it stays a page long.
- **The guard is on the same screen as the routes.** A mount function
  that needs sign-in or an admin opens with it, so nobody has to look
  one function up to know who may reach a route.
- **`rastrillo routes`** prints every route with its handler and its
  page guards (sign-in, admin). It is what file routes' directory tree
  gave for free. The contract that makes it possible, which step 1's
  mount API must already meet:
  - mount functions only register: no migration, query or network
    call at registration, so they can run with no database;
  - every route, including the outer `ServeMux`'s and the API's
    mount, registers through the same `Routes` type, which records
    method, path, handler name and guard names;
  - the app exposes one function that registers everything, given its
    dependencies; the CLI compiles a small program that calls it with
    a recording `Routes` and empty dependencies, and prints the record.
  Its output says plainly that guards are not the whole answer to "who
  may reach this?": an operation's `Authorize` can still refuse.
- **A handler no route mounts fails the gate.** The route table records
  each handler it mounts, and a check compares that against the app's
  handlers. A handler is any function or method with the `Visit`
  handler signature (`func(*action.Visit)`), which a helper never has.
  Legacy `(w, r)` handlers are not checked; they are on their way out.
  Staticcheck's unused check (U1000) is not enough on its own: a
  handler a test calls directly counts as used.
- **Redirects name a route, not a string.** Five panelists flagged
  `visit.Redirect("/admin/access")`: rename the route and every
  redirect to it breaks silently. Rastrillo should let a route table
  give its paths names an action can use, so a rename is one edit. How
  is the builder's; it is step 3 of the work.

### Deprecating `actions/`

No app outside Rastrillo's repository uses `actions/` files (checked
across the apps on the shared machine on 2026-10-29). Inside it, they
are used by `examples/blog`, `examples/helloworld`, and everything the
manifest generator emits. Nothing breaks on upgrade:

1. The generator emits mount functions and handler methods instead of
   `gen/actions/` packages, behind a bridge that keeps every app
   working:
   - hand `actions/` files keep being discovered and routed as now;
   - a hand file at a generated route's path still replaces exactly
     that one generated route, the precedence `internal/generate`
     applies today;
   - `gen.Router` keeps its signature, so `main.go` does not change;
   - the parity lint, which today finds overrides only under `actions/`
     and starts at a function named `Handle`, learns handler methods in
     the same step, so no override escapes it;
   - its `//rastrillo:page-only <reason>` exemption becomes a comment on
     the one handler it excuses. Today it sits above the package clause
     and exempts the whole file, which is one action under `actions/`;
     in a file of several handlers it would excuse the neighbours too.
     File-wide exemption stays for legacy `actions/` files only, and a
     regression test puts one exempt and one non-compliant handler in
     the same file.

   Replacing one generated screen once `actions/` is gone needs a new
   form that stays visible in the route table.
2. `examples/blog` and `examples/helloworld` move to the route table.
3. `rastrillo generate` and `doctor` warn when an app has an
   `actions/` directory, naming the release that removes support and
   pointing at the CHANGELOG's move guide.
4. The docs, `skill/api.md` and `README.md` stop teaching `actions/`.
   The rule that a hand-written action over an exposed resource calls
   its operation moves to handler methods. (`README.md` also still says
   `rastrillo new` writes "one starter action"; it writes
   `handlers.go`.)
5. Support for `actions/` is removed in a later release, once the
   warning has shipped.

## Left to the builder

- The names and signatures of the exchange value, the refusal helper
  and the show-again call, judged by how an action reads.
- How the gates are implemented (an analyzer, a test, a `rastrillo`
  subcommand), as long as an app's `make ci` and Rastrillo's gate run
  the same checks.
- The ratchet list format.
- Whether loop indices get an exception to rule 1. The default is no.
- How the framework pass is split into branches.
- How an app replaces one generated screen once `actions/` is gone.
- How named paths work.

## Calls made in writing this up

Not discussed with Paul; listed here so Paul can check them.

- A broken body is a 400 on every page form, including hand-written
  ones that ignore it today (behaviour change 3, from the step 1 plan
  review).
- Invalid forms answer 422 everywhere, so generated actions change
  from 400; Conflict becomes a re-shown form at 409. These are the two
  behaviour changes. (The first draft kept 400; Astra's review showed
  Patrix and `skill/forms.md` use 422.)
- `Visit` is the only shape taught to app code; `(w, r)` stays
  underneath, and nothing is removed.
- `t`, `b`, `f` and one-capital type parameters are exempt from rule 1.
- Existing apps opt in to the gates through a command of its own, not
  `doctor --fix`.
- The refusal helper is two calls, and `Visit` carries no app
  dependencies (both from the focus group).
- Named paths are in scope, as step 3 (from the focus group).
