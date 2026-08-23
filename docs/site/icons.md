# 🤖 Icons

`rastrillo new` writes an icon set into your app as an ordinary
app-owned package. Three flags choose what it writes, and all six set ×
delivery combinations scaffold, compile and pass
`rastrillo generate --check`.

```sh
rastrillo new --icons=lucide --icon-delivery=inline --ux=considered myapp
```

## The slugs are rastrillo's, not a vendor's

Eleven slugs, and they mean the same thing in every app whatever set
backs them:

```text
alert-triangle  check  check-circle  chevron-down  help-circle
info  kebab  plus  search  x  x-circle
```

`rastrillo.IconSlugs()` is the list.

This is load-bearing rather than pedantic. **Five of the eleven differ
from Lucide's canonical names** — `kebab` is Lucide's
`ellipsis-vertical`, and v1 renamed `check-circle`, `alert-triangle`,
`x-circle` and `help-circle` — so even the Lucide set carries a
translation table.

The payoff is that `{{icon "search"}}` means the same thing everywhere
and the shipped `ui/` partials never change when the set does.
`internal/iconsets` asserts that every scaffoldable set covers the whole
list, so an icon added to the framework that a set cannot answer fails
the build rather than vanishing the moment someone passes `--icons`.

An unknown slug at run time renders **nothing** rather than panicking a
page mid-response: a typo costs a missing icon, not a crash.

## --icons

`lucide` (default) or `font-awesome`.

`--icons=font-awesome` means Font Awesome **Free**. Pro is a paid
product Rastrillo cannot vendor or link on your behalf, so Pro-only
icons will not resolve; a Pro licensee wires their own kit through the
same seam, since the icons package is app-owned source.

Choosing it also writes the **CC BY 4.0 attribution** the licence
requires. That obligation is the app's, not the framework's, so it
travels with the code.

## --icon-delivery

`inline` (default), `cdn`, or `js`.

**Inline is the default and the recommendation**: no build step, no
second origin, works offline. That is what vendoring icons has always
been for.

`cdn` and `js` are fully supported rather than discouraged. Each prints
its specific cost once at scaffold time, records it as a comment in the
generated package, and is never mentioned again — a supported choice
that nags on every build is not really supported.

The cost worth repeating is `js`'s: **icons do not render at all without
JavaScript.**

Both remote modes pin exact versions with real SRI hashes.

## --ux

`considered` (default) or `standard`.

It seeds a UX convention profile into the app's `AGENTS.md`, which
carries the app's instructions and is the source of truth from then on.
`CLAUDE.md` is a one-line `@AGENTS.md` import, so the instructions reach
whatever agent someone uses rather than one particular one.

**The profile is a seed, not a live binding.** The resolved list is
written once; an explicit flag beats the profile's default so the file
never lies about what the app does; and nothing re-reads the profile
name afterwards. That is what makes editing a line as valid as picking a
profile, and what stops a Rastrillo upgrade changing a shipped app's UX.

Conventions marked `[x]` are enforced by a vendored component; `[ ]`
ones an agent applies by hand. The gap between the two is kept visible
rather than blurred.

The conventions in `considered` are Rastrillo's own, and the profile is
named for what it does rather than after anyone else's work. For wider
reading on interface quality, [impeccable.style](https://impeccable.style/),
the [WAI-ARIA Authoring Practices](https://www.w3.org/WAI/ARIA/apg/) and
[Inclusive Components](https://inclusive-components.design/) are all
worth your time — offered as reading, not as anything this framework
endorses or claims to implement.

## Wiring, and why the layout never changes

```go
tmpl := template.Must(template.New("").
	Funcs(ui.Funcs(ui.WithIcons(icons.Icon, icons.Assets))).
	ParseFS(ui.Templates(), "*.html"))
```

The scaffold wires both seams into the generated `render.go` and puts
`{{iconAssets}}` in the layout's `<head>`. That renders **empty** for the
inline default, so switching delivery later needs no template edit.

## Checking your icons

`rastrillo generate --check` fails when a template names an icon nothing
answers — both `{{icon "x"}}` and the commoner form where the slug
reaches a partial as data:

```html
{{template "list-row-action" dict "ActionIcon" "plus"}}
```

A slug computed at run time cannot be checked, as with any static gate.

## Checking the pins

```sh
go test -tags pins ./internal/iconsets/
```

Verifies that every pinned URL still hashes to the integrity value
shipped beside it. A mismatch means the bytes changed under a version
that is supposed to be immutable, which is serious. It separately
reports whether a newer release exists, which is only informational.

Versions are pinned (`lucide@1.33.0`, `lucide-static@1.33.0`,
`@fortawesome/fontawesome-free@7.3.1`) and nothing re-pins them
automatically. A version changed without its hash fails as an unstyled
page rather than an error, so check both together.

The test is build-tagged so the ordinary suite and CI never depend on
jsdelivr or the npm registry being up. A check that fails when someone
else's CDN has a bad afternoon teaches people to ignore it. Run it at
release.
