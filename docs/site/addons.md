# 🤖 Addons

Rastrillo's core holds what every app needs and what is hard to get
right twice. Some things are neither — real for many apps, wrong for
every app to carry. Those ship as **addons**: separate modules,
versioned separately, that an app pulls in when it wants them.

An addon is not a plugin system. There is no registry, no lifecycle and
no hook table. An addon is an ordinary Go module that happens to obey
four rules.

## What makes an addon

**It depends on Rastrillo; Rastrillo never depends on it.** The arrow
points one way, always. Nothing in the framework knows an addon exists,
which is what lets an addon ship on its own schedule.

**It ships its own migrations, namespaced.** A `migrate.Set` the app
merges into `BootSchema` — never into its own `Schema`, or
`rastrillo migration check` proposes dropping tables that `Models` does
not know about. See [Migrations](/docs/migrations).

**It ships its own `SKILL.md`, inside the module.** The framework's sits
at the repo root a scaffolded app points to; an addon's lands in the
module cache, so the directory page's job is to name the one command
that prints it. An addon that an agent cannot read is an addon that
saves the typing and none of the reading.

**It does not re-implement the core.** Sessions, CSRF, migrations,
forms and flash are already there. An addon that brings its own is a
fork wearing a smaller name.

## The directory

### idear — accounts, roles, invitations

**Status:** released, v0.1.1.

**Module:** `amadan.net/rastrillo/idear` ·
**Source:** <https://amadan.net/rastrillo/idear>

The vanity path is `amadan.net`, not `github.com/carlosframework`,
because an addon is a separate module in a separate repository on its
own release schedule — do not guess `github.com/carlosframework/idear`,
which does not exist. Use the path above verbatim.

The roster for an instance: who is in it, at what role, and who may
change that. Three strictly ordered roles — Owner, Admin, Member, with
exactly one Owner at all times — plus invitations, member management,
and the middleware that makes the membership gate the short path.

Load its authoring doc before building on it. An addon's `SKILL.md`
ships **inside the module**, so `go get` has already put it on disk —
and the copy you read is pinned to the version you are building
against, which a URL would not be:

```sh
go get amadan.net/rastrillo/idear
cat "$(go list -m -f '{{.Dir}}' amadan.net/rastrillo/idear)/SKILL.md"
```

It sits on top of [sessions](/docs/sessions) and whichever identity
plugin the app already chose, so [passwords](/docs/passwords) and
[magic links](/docs/magic-links) both keep working:

```go
roster, err := idear.New(idear.Config{DB: d.G, OpenSignUp: false})
if err != nil {
	return nil, err
}
ph, err := password.New(password.Config{
	Sessions:     sess,
	Lookup:       lookupUser(d.G),
	Create:       roster.Admitting(createUser(d.G)),
	RenderSignin: renderSignin,
	RenderSignup: renderSignup,
})
if err != nil {
	return nil, err
}
```

Not `r`: that name is the chi router everywhere else, including
[Passwords](/docs/passwords), and shadowing it here would cost you the
router for the rest of the function.

**What it deliberately does not do.** It is not an identity provider: it
never mints a session, never hashes a password, never renders a sign-in
form. It has no tenant field and no tenant scope — a CARLOS app serves
one team, and separating teams stays the platform's job. See
[Scoping](/docs/scoping).

Recording refusals is the addon's job, not the framework's. `password`
answers a `Refuse` at 403 and logs nothing: a refusal is expected
policy rather than an error, and writing a caller-supplied string and a
visitor's address to the log on every uninvited signup is noise and
personal data both. An addon that wants the audit trail keeps it
itself, at whatever fidelity its own policy calls for.

### aviso — Web Push

**Status:** released, v0.1.0. The manual iOS smoke test
(`docs/ios-smoke.md` in the module) is written down and not yet run.

**Module:** `amadan.net/rastrillo/aviso` ·
**Source:** <https://amadan.net/rastrillo/aviso>

The subscriptions a signed-in person enrols from their browsers, one
VAPID key per app, and a sender that fans a payload out to those
devices through the browser vendors' push services. Extracted from
Eleven messenger's push transport; the design is in
`docs/superpowers/specs/2026-09-07-aviso-web-push-design.md`.

```sh
go get amadan.net/rastrillo/aviso
cat "$(go list -m -f '{{.Dir}}' amadan.net/rastrillo/aviso)/SKILL.md"
```

It moves bytes to devices; the app owns policy. Who is notified, what
the payload means, and the service worker's lifecycle stay the app's —
aviso never decides who should be told what. Ownership of a
subscription is the [session](/docs/sessions) subject; the request
body never names one, and an endpoint another account enrolled is
refused rather than reassigned. The VAPID private key is provisioned
once, as one environment variable refused when empty, the way every
family secret is; nothing mints a key at boot, because a key minted
into local state is a key lost at the next restore.

Three halves: a Go transport (subscriptions table merged into
`BootSchema`, an SSRF-guarded sender over `webpush-go`, three handlers
gated by session and origin), an embedded browser module for
enrolment, and an embedded service-worker helper the app's own
`sw.js` loads with `importScripts`. Every push ends in a visible
notification, because WebKit revokes push for a worker that receives
silently.

**What it deliberately does not do.** It does not cache, work offline,
or own the worker's lifecycle. It does not retry, queue, or report
delivery: a push service's acceptance is where its knowledge ends. It
does not make the app installable — the manifest is the app's
identity — but it ships the recipe, because iOS delivers push only to
a Home Screen app.

## Client kits

Add a client kit when the app needs installation or a native companion.
Each lives in its own repository, with its own skill and release schedule.
The web framework does not import either kit. Browser and Swift helpers
can be used without adding a server-framework dependency.

### PWA — installation and an offline fallback

**Status:** released, v0.1.0.

**Source and Go module:** `amadan.net/rastrillo/pwa` ·
[Repository](https://amadan.net/rastrillo/pwa)

Use the kit to serve an app manifest, register one service worker and show
a public offline page when navigation fails. Its worker never stores
application pages, API responses, keys or pending writes. Waiting updates
are reported to the app; activation and reloading remain explicit so an
update cannot silently discard edits.

Install a reviewed revision, then read the bundled skill:

```sh
go get amadan.net/rastrillo/pwa@v0.1.0
cat "$(go list -m -f '{{.Dir}}' amadan.net/rastrillo/pwa)/SKILL.md"
```

The repository's `examples/basic` is the complete wiring example, including
manifest icons and aviso's worker helper. Add aviso's handlers to the same
worker and use the same registration for push enrolment. The example does
not provision push subscriptions or send notifications; use aviso's skill
for those steps.

### Native — shared components and an Apple companion scaffold

**Status:** released, v0.1.0; adopted by Keymail and Ocho.

**Swift package:** `RastrilloNative` ·
[Repository](https://amadan.net/rastrillo/native)

Add the repository URL as a Swift package dependency pinned to a reviewed
revision. Read `SKILL.md` from that checkout. The initial package supports
iOS 17+ and macOS 14+; its `examples/companion` supplies an app-owned SwiftUI
starting point for both platforms.

`CoalescedRunner` shares the refresh gate previously copied between
Keymail and Eleven/Ocho. A burst of refresh requests queues one trailing
pass, and callers wait for fresh data before resuming. Use one runner per
operation and account.

The native skill includes optional Go Mobile binding guidance. This first
kit does not ship Android adapters, account linking, native push or a
cross-platform UI renderer. Those components need their own extraction and
consumer proof before joining the package.

Prefer fully native UI so menus, right-click actions, links and navigation
work as people expect on their platform. For complex apps, use native
navigation around selected webview screens. Consider a shared manifest of
destinations and commands rendered separately for web and native; the kit's
architecture guide describes that approach, but no dual-target compiler
ships yet. The existing resource manifests still generate web CRUD only.

### Offline data

For offline reading or editing, define the app's local data and sync
contract first. The PWA fallback is not an offline data engine. A reusable
offline kit must prove restart recovery, duplicate-safe retries, account
isolation, migrations and conflict handling in a second, different app.
Encrypted offline storage also needs explicit locking, key recovery and
notification-preview policy. Reuse the existing crypto and keyring
contracts where compatible; do not import an app's message policy into a
general storage library.

## Publishing an addon

Follow the four rules above, then serve `SKILL.md` at a stable URL and
send a patch adding an entry here. An addon nobody can find and no agent
can read is a library, not an addon.
