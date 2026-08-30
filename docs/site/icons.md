# 🤖 Icons

`rastrillo new` writes an icon set into your app as an ordinary
app-owned package. Three flags choose what it writes, and all six set ×
delivery combinations scaffold, compile and pass
`rastrillo generate --check`.

```sh
rastrillo new --icons=lucide --icon-delivery=inline --ux=considered myapp
```

Take the defaults unless you have a reason not to. The rest of this page
is the reasons.

## The slugs are Rastrillo's, not a vendor's

Twelve slugs, and they mean the same thing in every app whatever set
backs them:

```text
alert-triangle  check  check-circle  chevron-down  help-circle
info  kebab  menu  plus  search  x  x-circle
```

`rastrillo.IconSlugs()` is the list.

This matters more than it sounds. Five of the twelve differ from
Lucide's canonical names — `kebab` is Lucide's `ellipsis-vertical`, and
v1 renamed `check-circle`, `alert-triangle`, `x-circle` and
`help-circle` — so even the Lucide set carries a translation table.

`kebab` and `menu` are the pair worth keeping straight: `kebab` is the
three dots that mean "more actions on this row", `menu` the three lines
that mean navigation. The shells use `menu` when they collapse.

The payoff is that `{{icon "search"}}` means the same thing everywhere
and the shipped `ui/` partials never change when the set does.
`internal/iconsets` asserts that every scaffoldable set covers the whole
list, so an icon added to the framework that some set cannot answer
fails the build instead of vanishing the moment someone passes
`--icons`.

An unknown slug at run time renders nothing. A typo costs you a missing
icon, not a crashed page mid-response.

## --icons

`lucide` (default) or `font-awesome`.

`--icons=font-awesome` means Font Awesome Free. Pro is a paid product
Rastrillo cannot vendor or link on your behalf, so Pro-only icons will
not resolve. If you have a Pro licence, wire your own kit through the
same seam — the icons package is app-owned source.

Choosing it also writes the CC BY 4.0 attribution the licence requires.
That obligation is your app's, so it travels with your code.

## --icon-delivery

`inline` (default), `cdn`, or `js`.

Inline is the recommendation: no build step, no second origin, works
offline.

`cdn` and `js` are properly supported, not grudgingly tolerated. Each
prints its specific cost once at scaffold time, records it as a comment
in the generated package, and never mentions it again — a supported
choice that nags on every build is not really supported.

The one cost worth repeating: with `js`, icons do not render at all
without JavaScript.

Both remote modes pin exact versions with real SRI hashes.

## --ux

`considered` (default) or `standard`.

It seeds a UX convention profile into your `AGENTS.md`, which carries
your app's instructions and is the source of truth from then on.
`CLAUDE.md` is a one-line `@AGENTS.md` import, so the instructions reach
whatever agent someone uses.

The profile is a seed, not a live binding. The resolved list is written
once, an explicit flag beats the profile's default so the file never
lies about what your app does, and nothing re-reads the profile name
afterwards. That is what makes editing a line as valid as picking a
profile, and what stops a Rastrillo upgrade changing a shipped app's UX.

Conventions marked `[x]` are enforced by a vendored component; `[ ]`
ones an agent applies by hand. The gap between the two stays visible.

The conventions in `considered` are Rastrillo's own. For wider reading
on interface quality, [impeccable.style](https://impeccable.style/), the
[WAI-ARIA Authoring Practices](https://www.w3.org/WAI/ARIA/apg/) and
[Inclusive Components](https://inclusive-components.design/) are all
worth your time — offered as reading, not as anything this framework
implements.

## Wiring

```go
tmpl := template.Must(template.New("").
	Funcs(ui.Funcs(ui.WithIcons(icons.Icon, icons.Assets))).
	ParseFS(ui.Templates(), "*.html"))
```

The scaffold wires both seams into the generated `render.go` and puts
`{{iconAssets}}` in the layout's `<head>`. That renders empty for the
inline default, which is why switching delivery later needs no template
edit.

## Checking your icons

`rastrillo generate --check` fails when a template names an icon nothing
answers. It catches both `{{icon "x"}}` and the commoner form where the
slug reaches a partial as data:

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

Versions are pinned — `lucide@1.33.0`, `lucide-static@1.33.0`,
`@fortawesome/fontawesome-free@7.3.1` — and nothing re-pins them
automatically. A version changed without its hash fails as an unstyled
page rather than an error, so check both together.

The test is build-tagged so the ordinary suite and CI never depend on
jsdelivr or the npm registry being up. A check that fails when someone
else's CDN has a bad afternoon teaches people to ignore it. Run it at
release.
