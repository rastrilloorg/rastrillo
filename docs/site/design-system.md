# Your app's design system

`rastrillo new` hands your app a stylesheet, a theme, and three scripts. They land in `static/` and they are yours from that moment — edit them, delete what you do not use, replace them entirely. Nothing in the framework reaches back in.

That leaves a question this page answers: when you want something the design system does not give you, where does it go?

## Customising, cheapest first

**Pick a different theme.** `rastrillo new --theme=signal`, or swap `static/theme.css` later. Three ship: `day`, `plain`, `signal`. This changes colour, type family and shape, and nothing else has to move.

**Override tokens in your own CSS.** The `head` block is your slot, and it comes last, so your stylesheet wins the ties it should win:

```html
{{define "head"}}<link rel="stylesheet" href="/static/app.css">{{end}}
```

```css
:root { --rst-accent: light-dark(#7c3aed, #a78bfa); --rst-radius: 4px; }
```

Every component paints itself from these, so one line changes the accent everywhere.

**Write your own theme.** Replace `static/theme.css`. The only contract is the token set: declare every name `day` declares and every component already knows what to do with it. A theme of your own reads to `rastrillo doctor` as "custom or drifted" — supported, not damage.

**Edit `tokens.css` itself.** Allowed. It is your file. Name it in `vendoredIsMine` in the generated test so an upgrade does not report your deliberate edit as drift.

The ladder is deliberate. Each rung costs more to carry across upgrades than the one above it.

## Components the framework does not ship

Write them in your app, in your app's CSS. Do not put them in `ui` — the framework's vocabulary is what every rastrillo app shares, and a component only your app needs is not that.

Write them in the same grammar so they read like the rest of your markup: an attribute for what a thing **is**, `class` for utilities that apply anywhere.

```html
<div app-invoice-row app-invoice-row="overdue">
```

```css
[app-invoice-row] { … }
[app-invoice-row~="overdue"] { … }
```

Use your own prefix. `rst-` is the framework's, and a future version may claim a name you took.

## When a second app needs the same component

Move it to a module both depend on. Not before — two specs describing the same want are two guesses, where two working implementations are two facts, and the second use is what tells you the shape the first one got wrong.

**A copy is the trigger.** The day one app copies or imports another's component, extract it that week and delete the copy. Waiting for "a second consumer appears" never fires; copying is cheap and immediate, so name the cheap act.

A shared module depends on `ui`, keeps its bones — the tokens, the vocabulary, the accessibility floors — and adds its own components and themes on top. It is not a fork, and it is more than a theme.

## Upgrading

`tokens.css` is copied into your app once and frozen there, while the partials it styles keep upgrading with the module. An app can run new markup against old CSS and see nothing but a slightly wrong screen.

So re-copy it when you upgrade:

```
rastrillo doctor          # what drifted
rastrillo doctor --fix    # re-copy it
```

`doctor` compares your frozen files against the module's. It reports a version mismatch as a mismatch rather than as drift, and refuses `--fix` across one — copying newer assets into an app that compiles against an older framework manufactures the fault the tool is looking for.

## Which spelling to write

From v0.21.0, `tokens.css` matches both spellings, so an app mid-migration renders identically either way. Write attributes — that is what the framework itself writes now, and the class spelling is kept so existing apps can migrate rather than because it is the one to learn.

`rastrillo markup` converts an app. Run `rastrillo doctor --fix` first, or the new markup meets an old stylesheet.

Adopt per screen rather than per element. Mixed spellings inside one component match neither.
