# A page ships only what it uses (#158)

Status: design, 2026-10-31. Branch `page-weight`. Measured by the agent
benchmark (`bench/agent`).

## Why

In round 1 of the agent benchmark, every Rastrillo app's home page
downloaded about 170 kB compressed: 86 kB of CSS and 79 kB of JS. The
control's pages, built with no framework, were 6–12 kB with no JS. A
smoke-test page with one heading and a date was 164 kB. The weight
didn't depend on the app. It's the framework's own files, sent whole on
every page:

- every layout in `ui/layouts/` loads all eight component scripts;
- `tokens.css` is the whole design system in one file;
- **comments are 72% of the compressed weight.** The vendored files
  carry their design notes with them:

| file | gzip as shipped | gzip, comments removed |
|---|---|---|
| tokens.css | 74 kB | 21 kB |
| all eight scripts | 79 kB | about 33 kB |

## Goal

A scaffolded app's plain page (no select, date or calendar field) loads
under 40 kB compressed, CSS and JS together. A page that uses a select
or a date field adds only that component's script. Rastrillo's pages
are then lighter than round 1's control apps for what they do, and the
next round can show it.

## Non-goals

- Changing how anything looks or behaves. Every browser test passes
  unchanged against the served files.
- Stripping comments from the source. The commented files stay in the
  repo and in each app's `static/`. Agents read them (round 1's agents
  grepped `tokens.css` for class names), and so do people.
- A build step an app has to run.

## Changes

### 1. Serve the framework's files minified

`rastrillo.Assets` already serves an app's `static/` with fingerprinted
names. For the vendored files (`ui.VendoredNames`), it serves a minified
copy instead: comments removed and whitespace collapsed. The copy is
made once when the app starts and fingerprinted by the bytes actually
sent. An app's own scripts and stylesheets are served as written, so
minification can't break code the framework didn't ship. (My call.)
This reaches every existing app when it upgrades, with no edit.

- **The minifier (decided by the operator):** `github.com/tdewolff/minify/v2`.
  It's pure Go and MIT-licensed, and it handles JS regex and template
  literals correctly, which hand-rolled stripping gets wrong. It's one
  new dependency in the root module.
- Each file is served precompressed with gzip (and brotli if the
  minifier choice makes it cheap), so the app doesn't depend on the
  platform's edge to compress.
- A test loads the gallery's pages from the minified files in the
  browser rig, so minification can't change behaviour silently.

Expected: about 165 kB down to about 55 kB on every page, with no change
in any app's code.

### 2. Load a component's script only where it's used

The layouts load `select.js`, `datetime.js` and `calendar.js` (27 kB of
the stripped JS) on every page, though most pages have none of those
fields.

- A partial that needs a script names it, and the layout emits only the
  scripts the page's partials asked for, each once. The always-on
  scripts stay in every layout: `rastrillo.js`, `busy.js`, `live.js`
  (inert unless the page opts in), `light.js` and `anchor.js`.
- A page with a live region or a polling fragment loads every component
  script. Those swap in HTML after the page has loaded, and a select
  arriving in a fragment must still be enhanced. Live pages keep
  today's behaviour, and the browser tests stay unchanged. (My call.)
- The layouts are app-owned once scaffolded, so this reaches new apps
  and the in-repo examples. The changelog gives existing apps the
  one-line layout edit.
- An app's own markup can ask for a component script the same way,
  with the template function `needs`, which SKILL.md documents in one
  line.
- `rastrillo doctor` and the scaffold's vendored test are unchanged:
  the files are still vendored, they just aren't all loaded.

### 3. A first-load budget the gate enforces

A `perftest` check renders each GET route in-process and sums the
framework CSS and JS the page loads, as served: minified, gzipped at
the default level, deferred scripts included. The HTML isn't counted,
so the number doesn't grow with a page's rows, and the benchmark stays
the measure of the whole page as delivered. (My call.)

- **The limit:** 40 kB for a plain page in the column and topbar shells,
  plus the measured size of each component script the page loads.
  The sidebar, console and stage shells get their own limit: what they
  measure after changes 1 and 2, plus 10% headroom.
- **Where it runs:** the scaffold's perf test applies it to every GET
  route, so an app inherits it. The framework's gate renders each
  `ui` layout's pages in-process. That's cheap, with no new scaffold
  per shell.
- **On failure:** it names the file that grew.

### 4. What SKILL.md says

Your earlier steer: the skill guides the agent to load only what it
needs. With change 2, the agent mostly needs to do nothing. SKILL.md
says:

- the field partials load their own scripts;
- don't add `<script>` tags for framework components;
- use `needs` only for hand-written markup.

## Later, if the numbers ask for it

Splitting `tokens.css` into a base plus per-component layers saves
perhaps another 10 kB once it's minified. Do it only if round 2 shows
CSS still dominating.

## How we know

Each change lands with its own tests. Then a round with the carlos arm
on `groomer`, reusing round 1's control runs: home page weight should
drop from about 170 kB to under 40 kB with no change in axe or
Lighthouse. The held-out briefs confirm it before the change is
accepted.

## Left to the builder

- how a partial names its script, and how the layout collects them
  (a per-render set on the page's data is the obvious shape)
- whether minified copies are built at startup or lazily on first
  request
