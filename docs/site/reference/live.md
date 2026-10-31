# 🤖 live

`amadan.net/rastrillo/rastrillo/live`

Pages that update when their data changes: a hub of per-topic version
counters, a stream that says which topics moved, and the helpers that
answer a request for one region of a page.

[Live pages](/docs/live) is the guide.

## What it does and does not do

The stream carries topic names and versions, never data. A page that
hears a topic moved asks for the region again, from its own URL, as its
own viewer, so every check you already run still runs.

The hub lives in memory. A restart gives it a new boot ID, and every
open page then fetches its regions once. Nothing is stored.

Live streams are off unless your app builds a hub and mounts its
handler. With no hub, or with scripts off, a page is an ordinary static
page.

The package imports only the standard library. `ui` and the root
package import `live`, never the other way round.

## New and Config

```go
func New(cfg Config) (*Hub, error)

type Allow func(r *http.Request, key string) bool

type Config struct {
	Path          string
	Topics        map[string]Allow
	MaxStreams    int
	Lifetime      time.Duration
	Heartbeat     time.Duration
	WriteDeadline time.Duration
}
```

Build one hub at boot. `New` checks the config and gives the hub a fresh
boot ID. It errors when:

- `Path` is empty.
- A topic name does not match `^[a-z][a-z0-9-]{0,31}$`.
- A topic's `Allow` is nil.

| Field | Meaning | Default |
|---|---|---|
| `Path` | Where you mount the stream handler. `View.Head` puts it in the page. Required. | none |
| `Topics` | Each declared topic name and its `Allow` check. | none |
| `MaxStreams` | Open streams allowed per process. | 1000 |
| `Lifetime` | How long one connection lasts. | 5 minutes |
| `Heartbeat` | The quiet-stream `: ping` interval. | 15 seconds |
| `WriteDeadline` | How long one write may take. | 5 seconds |

A zero value takes the default.

`Lifetime` is how a revoked session loses its stream. The connection
ends cleanly, the page reconnects, and your auth runs again. Keep it
short enough that you can live with the gap.

`WriteDeadline` is re-armed on every write. It is what stops a client
that has stopped reading from holding a stream, or shutdown, open.

`New` copies `Topics`. A topic added to your map afterwards is not
declared.

## Allow and topics

A topic is a declared name with an optional key: `notes` or
`notes/42`. A name is lowercase letters, digits and hyphens, starting
with a letter, up to 32 characters. A key is letters, digits, `_` and
`-`, up to 64.

`Allow` answers one question: may this request follow the topic with
this key? The key is `""` for a topic with no key. It runs when a
stream opens, once per topic, against the request your auth produced.

Put the owner in the key. A topic with no key wakes every page that
shows it, whoever wrote, and that tells one person someone else wrote.
With `notes/<user id>` and an `Allow` that compares the key to the
signed-in user, only that user's pages hear about their notes.

Declaring names up front also means a client cannot grow the hub's
memory by asking for topics that do not exist.

No shared counter ever leaves the server. Each topic has its own
version, so the gaps between numbers tell a viewer nothing about topics
they may not follow.

## Hub

```go
func (h *Hub) Boot() string
func (h *Hub) Notify(topics ...string)
func (h *Hub) Middleware(next http.Handler) http.Handler
func (h *Hub) View(r *http.Request) View
func (h *Hub) Handler() http.Handler
func (h *Hub) Close()
```

Every method except `Handler` is safe on a nil hub, so an app whose hub
is optional can call them without a check. A nil hub means a static
page.

### Boot

`Boot` is the ID that is new each time `New` runs: twelve lowercase hex
characters. It is `""` for a nil hub.

It is how a page learns the process restarted. A stream opened with an
old boot ID treats every topic as changed. `View.Attrs` writes it for
you. You rarely need to read it.

### Notify

Call `Notify` after the write has committed, with the topics that
changed:

```go
a.db.Create(&n)
a.live.Notify("notes/" + strconv.FormatInt(uid, 10))
```

If you call it before the commit, a page can fetch at once, read the
old rows, and stay on them.

`Notify` never blocks and never fails. A malformed topic, or one whose
name you did not declare, is logged and skipped, so a typo does not
take down the request that made the write. A topic listed twice counts
once. `notes` and `notes/7` are different topics with different
versions. Notify the one the region shows.

### NotifyAt

```go
func (h *Hub) NotifyAt(at time.Time, topics ...string)
```

Some changes happen at a time, not on a write: a post that goes live at
9:00, an offer that ends at midnight. Nothing calls `Notify` for them,
so tell the hub when they are due:

```go
a.live.NotifyAt(p.PublishAt, "posts")
```

Call it at boot for every future time you have stored, and again
whenever a schedule changes. The hub forgets pending times when the
process restarts, but a restart also gives it a new boot ID, so every
open page fetches its regions once anyway. Your boot calls then cover
the times still ahead.

A time that has already passed notifies at once, before `NotifyAt`
returns. The same topic at the same time is kept once. Topics are
checked as `Notify` checks them.

There is no cancel. If you move a post's time, its old time still
fires, and the pages that show it fetch once and find nothing new.

At most 10,000 (time, topic) pairs wait. Past that, the pair due last
is dropped, whether it is the new one or one already waiting, and the
first drop is logged. A dropped pair only loses its live update: pages
still show the change the next time they load. `Close` stops the
timer and drops every pending time, and `NotifyAt` does nothing after
it. On a nil hub it does nothing.

### Middleware

`Middleware` records where the hub stood when the request began. Mount
it first, before any handler queries anything. A write that lands
while the page is being built then reads as newer than the page, and
costs one extra fetch, never a missed change.

Without it, `View` still works but reads every changed topic as one
older than it is. That is also safe, and costs the same extra fetch.

### Getting the View

`View` returns the request's [`View`](#view), for a template. Put it
in your page data, as `Live`.

### Handler

`Handler` is the change stream. Mount it at `Config.Path`, behind your
own sign-in, so `Allow` sees the session.

```go
r.Group(func(r chi.Router) {
	r.Use(sess.Require)
	r.Handle("/live", hub.Handler())
})
```

The request is `GET <Path>?boot=<boot>&t=<topic@version>&t=…`. Only
`GET` is accepted. Every topic is checked before the stream opens, and
one bad topic refuses the whole stream, because serving the rest would
let a page quietly lose updates.

| Status | When |
|---|---|
| 200 | the stream |
| 400 | no `t`, a malformed token, or more than 64 topics |
| 403 | a topic is undeclared, or its `Allow` said no |
| 405 | not a `GET` (with an `Allow: GET` header) |
| 500 | the response writer cannot flush |
| 503 | the hub is at `MaxStreams`, or closed |

A refusal is the status text alone. It never says which topics exist.

The stream sends one event per topic that moved, in one flush:

```text
event: change
data: notes/42@7
id: 3f9a0c51d2e8 notes/42@7 tags@2
```

`data` is the topic's new token. `id` is the boot ID and every followed
topic's version as this stream now knows it. After a restart, a topic is
left out until its first event from this process has been sent. When the
browser reconnects it sends that back as `Last-Event-ID`, and the stream picks
up from there, but only when the boot ID matches this process and only
for topics named in the URL. A malformed `Last-Event-ID` is ignored.

A quiet stream sends `: ping` every `Heartbeat`. It ends when the
client goes, when `Lifetime` runs out, or when the hub closes.

`Handler` is not nil-safe. Mount it only when you built a hub.

### Close

`Close` refuses new streams and ends every open one. A stream waiting
for a change ends at once. A stream stuck writing to a slow client ends
when that write's deadline passes, so within `WriteDeadline`. It is
safe to call twice. It also stops `NotifyAt`'s timer and drops the times still waiting.

If you set `Options.Live`, [`Serve`](/docs/reference/rastrillo) calls
`Close` the moment shutdown starts, and you need not. Call it yourself
only when you embed the handler under your own server, and call it
before that server's `Shutdown`. Otherwise open streams hold the
10-second drain until it runs out.

## View

```go
func (v View) Attrs(topics ...string) template.HTMLAttr
func (v View) Head(paused, unconfirmed string) template.HTML
```

`View` is for templates. The zero `View`, which a nil hub gives you,
renders nothing.

`Attrs` writes the attributes for a region that shows these topics:

```html
{{define "region:notes-list"}}<div id="notes-list" {{.Live.Attrs .Content.Topic}}>
```

It renders `data-live="notes/7@4" data-live-boot="3f9a0c51d2e8"`: each
topic at the version this page was built from, and the boot ID. Never
type them by hand.

A topic that is undeclared or malformed is left out, and logged the
first time the hub sees its name. If none are left, `Attrs` renders nothing. `Attrs` checks the name, not the
key. `Allow` runs when the stream opens, not here.

`Head` writes the `<meta name="rastrillo-live">` tag, which holds the
stream path and the two notes the script shows. Put it in your layout's
head:

```html
{{.Live.Head (T $L "rastrillo.ui.live.paused") (T $L "rastrillo.ui.live.unconfirmed")}}
```

`paused` is shown when updates stop arriving. `unconfirmed` is shown
when a form was sent and the page cannot tell whether it landed. Pass
them already translated. Both are HTML-escaped. No tag means no
stream, so leave `Head` out and the page stays static.

## WithBase

```go
func WithBase(r *http.Request, base string) *http.Request
```

An app served under a base path, such as `/acme` on a shared origin, needs its stream under that base too. `WithBase` records the base on the request, and `Head` then points the page at `/acme/live`. With `Options.BasePath` set, `rastrillo.Handler` calls it for you. Call it yourself only if you strip the base in your own middleware.

## Fragment requests

A page asks for a region by sending the same request it would send for
the whole page, plus a header. Your render function answers with the
region alone.

| Header | Direction | Meaning |
|---|---|---|
| `Rastrillo-Fragment` | request | Space-separated region names wanted. |
| `Rastrillo-Page` | request | On a form's fetch: the path of the page the form is on. |
| `Rastrillo-Regions` | response | The region names actually rendered. |
| `Rastrillo-Location` | response | With a `204`: navigate to this path. |

Region names match `^[a-z][a-z0-9-]{0,63}$`. A region is a template
named `region:<name>`. The prefix is the whole safety rule: a name from
a request can only reach a template you marked as a region, never your
layout or a partial.

Answer pages with `Render` (or `ui.Pages.Render`). It calls the
functions below in the one order that keeps a flash message from being
lost, and a mistake in that order fails without an error: a notice
disappears, or a form's errors never show.

If you call the building blocks yourself, the order is:

1. `Elsewhere`, before anything is taken.
2. `NoStore`.
3. `Wanted`.
4. `Has` (or `ui.Pages.HasRegions`), which ignores the notice. If it
   says no, answer a GET or HEAD with `Respond` and nothing rendered;
   any other method gets the whole page, with its status.
5. `TakesFlash`, to decide whether to take the flash.
6. `WriteRegions` (or `ui.Pages.WriteRegions`), or the whole page when
   `Wanted` or `Has` said no. To keep a status such as `422` on a
   fragment, send it with the first body byte, after `WriteRegions`
   has set its header.

### Templates

```go
type Templates interface {
	ExecuteTemplate(w io.Writer, name string, data any) error
}
```

The templates `Render`, `Has`, `ExecuteRegions` and `WriteRegions` take.
Pass a `*template.Template` or a [`*tmpl.Template`](/docs/reference/tmpl);
both satisfy it. A `*tmpl.Template` runs compiled code where it has it.

A region counts when the set has a template of that name: `Defined` on
a `*tmpl.Template`, `Lookup` on a `*template.Template`. A region that
was created but never parsed counts too, and rendering it is an error
for both.

### Render

```go
func Render(w http.ResponseWriter, r *http.Request, status int, t Templates, entry string,
	data func(takeFlash bool) (any, error)) error
```

Answers a request for a page, whole or as the regions it asks for. `t`
is the page's templates and `entry` the one to run for a whole page,
usually `"layout"`. `status` is the response's status; `0` means `200`.
A fragment answer keeps it, so a form's `422` still arrives as a `422`.

`data` builds your page data. `Render` calls it once, and tells it
whether this render shows the notice. Take the flash only when
`takeFlash` is true:

```go
err := live.Render(w, r, status, a.tmpl["show"], "layout", func(takeFlash bool) (any, error) {
	var fl flash.Flash
	if takeFlash {
		fl, _ = flash.Take(w, r)
	}
	return page{Flash: fl, Live: a.live.View(r), Content: content}, nil
})
if err != nil {
	http.Error(w, "something went wrong", http.StatusInternalServerError)
}
```

When the answer is a navigation (a form's redirect to another page, or
a GET for regions this page lacks), `data` is not called at all, so the
flash waits for the page you land on.

Everything renders into a buffer first. An error from `data` or from a
template comes back with nothing written, so you can send your own 500.
An error while writing the response can come after the status and
headers have gone out, when it is too late for a 500.

### Notice

```go
const Notice = "notice"
```

The reserved region name for the flash. Define `region:notice` as a
`<div id="notice" role="status">` that is always on the page, empty when
there is no flash.

### Wanted

```go
func Wanted(r *http.Request) (regions []string, ok bool)
```

Reads `Rastrillo-Fragment`: the names, in order, each once. `ok` is
false when the header is missing, when it lists more than 32 names, or
when any name is not a region name.
That keeps the jobs poll's older value, `1`, the jobs poll's, and
answers a bad request with the whole page rather than a guess.

### Elsewhere

```go
func Elsewhere(w http.ResponseWriter, r *http.Request) bool
```

A form's redirect can land on a different page from the one the form is
on. When the request is a `GET` or `HEAD` that carries `Rastrillo-Page`,
and that path is not this request's path, `Elsewhere` writes `204` with
`Rastrillo-Location` set to this request's URL, and returns true. Return
from your handler. The script navigates there.

Call it first, before you take the flash, so the page navigated to
still shows it. Other methods never match. A `POST` is not a redirect
target, and a `204` would hide what the write did.

### NoStore

```go
func NoStore(w http.ResponseWriter)
```

Sets `Cache-Control: no-store` and adds `Rastrillo-Fragment` to `Vary`,
keeping any `Vary` you already set. One URL now answers with a region or
a whole page by header, and a cache that did not know would serve one
for the other. Call it on every response of a live page.

### TakesFlash

```go
func TakesFlash(r *http.Request) bool
```

True when this render shows the notice region: a whole page always
does, a fragment request only when it names `Notice`. Take the flash
only when this is true. A refresh the stream started never asks for the
notice, and if it took the flash it would eat a message it never shows.

### Has

```go
func Has(t Templates, names []string) bool
```

Reports whether the template defines `region:<name>` for any of the
names other than `notice`, which every layout defines. Ask before
taking the flash. A GET that names only regions this page lacks is
answered by navigating, and the page navigated to must still find the
flash. Any other method is answered with the whole page.

### ExecuteRegions

```go
func ExecuteRegions(w io.Writer, t Templates, names []string, data any) (rendered []string, err error)
```

Renders each name the template defines as `region:<name>`, in order, and
skips the rest. It returns the names it rendered.

It buffers everything and writes to `w` only when every region
succeeded. `html/template` writes as it executes, so a failure halfway
would otherwise leave a torn fragment under a `200`.

Use it with your own template map. With `ui.Pages`, use
[`Pages.ExecuteRegions`](/docs/reference/ui#pages), which checks the
localizer first.

### WriteRegions

```go
func WriteRegions(w http.ResponseWriter, r *http.Request, t Templates, names []string, data any) error
```

Answers a fragment request: renders the regions, sets
`Rastrillo-Regions`, then writes the body. When nothing rendered, a
`GET` or `HEAD` gets `Answer`'s `204` and no body.

Pass it the `ResponseWriter` itself, not the result of
`ExecuteRegions`. Writing the body first would commit the headers with
the first byte, and `Rastrillo-Regions` would be dropped without a
word. On error nothing is sent, so you can write your own 500.

### Respond

```go
func Respond(w http.ResponseWriter, r *http.Request, rendered []string, body []byte) error
```

`WriteRegions`' second half, for when you rendered the regions yourself
(`ui.Pages` does). It calls `Answer`, then writes `body` if anything
rendered, with `Content-Type: text/html; charset=utf-8` unless you set
one.

`Respond(w, r, nil, nil)` is the answer to a request for regions the
page does not have: a `204` that sends the script to the page.

### Answer

```go
func Answer(w http.ResponseWriter, r *http.Request, rendered []string)
```

Sets `Rastrillo-Regions` to the names rendered. If none were, and the
request is a `GET` or `HEAD`, it also answers `204` with
`Rastrillo-Location`, and you write no body. A `POST` that rendered
nothing falls through with the header empty, because navigating would
hide that the write happened.

`WriteRegions` calls it for you. Call it yourself only when you write
the body some other way, and then before the first byte, since headers
set after it are lost.
