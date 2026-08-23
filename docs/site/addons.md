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

**It ships its own `SKILL.md`, fetchable over HTTP.** The framework's
sits at the repo root a scaffolded app points to; an addon's would
otherwise land in a module-cache directory nobody names. An addon that
an agent cannot read is an addon that saves the typing and none of the
reading.

**It does not re-implement the core.** Sessions, CSRF, migrations,
forms and flash are already there. An addon that brings its own is a
fork wearing a smaller name.

## The directory

### idear — accounts, roles, invitations

**Module:** `amadan.net/rastrillo/idear` ·
**Source:** <https://amadan.net/rastrillo/idear>

The roster for an instance: who is in it, at what role, and who may
change that. Three strictly ordered roles — Owner, Admin, Member, with
exactly one Owner at all times — plus invitations, member management,
and the middleware that makes the membership gate the short path.

Load its authoring doc before building on it:

```sh
curl -s https://amadan.net/rastrillo/idear/SKILL.md
```

It sits on top of [sessions](/docs/sessions) and whichever identity
plugin the app already chose, so [passwords](/docs/passwords) and
[magic links](/docs/magic-links) both keep working:

```go
r, err := idear.New(idear.Config{DB: d.G, OpenSignUp: false})
if err != nil {
	return nil, err
}
ph, err := password.New(password.Config{
	Sessions: sess,
	Lookup:   lookupUser(d.G),
	Create:   r.Admitting(createUser(d.G)),
})
```

**What it deliberately does not do.** It is not an identity provider: it
never mints a session, never hashes a password, never renders a sign-in
form. It has no tenant field and no tenant scope — a CARLOS app serves
one team, and separating teams stays the platform's job. See
[Scoping](/docs/scoping).

## Publishing an addon

Follow the four rules above, then serve `SKILL.md` at a stable URL and
send a patch adding an entry here. An addon nobody can find and no agent
can read is a library, not an addon.
