# 🤖 Live pages

A live page changes when the data behind it changes. Someone adds a
note in one tab, and the list in another tab updates. Nobody presses
reload.

The page still works with scripts off. It is an ordinary page, as
current as the moment it loaded. Live updates are an extra on top.

## How it works

Your server keeps a counter for each topic. A topic is a name for a
thing that can change, like `notes/7`, the notes of user 7. When you
write to the database, you tell the server that topic changed.

Every open page holds a stream open to the server. When a topic
moves, the stream says so. It sends the topic name and its new
number, and nothing else. No data goes down the stream.

The page then asks for the part that changed, from its own URL, as its
own viewer. Your handler answers with the same template and the same
checks it uses for the whole page. That part is called a region.

## Declare your topics

Make the hub once, at boot:

```go
hub, err := live.New(live.Config{
	Path: "/live",
	Topics: map[string]live.Allow{
		"notes": func(r *http.Request, key string) bool {
			uid, ok := sessions.UserID(r)
			return ok && strconv.FormatInt(uid, 10) == key
		},
	},
})
```

`Path` is where the stream lives. Each topic gets a check that says
whether this request may follow this key.

Put the owner in the key. A topic called `notes` with no key would
wake every signed-in page when anyone writes, and that tells Bob that
Alice wrote something. With `notes/<user id>`, only Alice's pages hear
about Alice's notes.

Names are lowercase letters, digits and hyphens, up to 32 characters.
Keys are letters, digits, `_` and `-`, up to 64.

## Mount it

Do three things:

```go
r.Use(hub.Middleware) // first, before any handler queries anything

r.Group(func(r chi.Router) {
	r.Use(sess.Require)
	r.Handle("/live", hub.Handler()) // behind sign-in
})

opts.Live = hub // before rastrillo.Serve
```

`Middleware` marks when each request began. If a write lands while a
page is still being built, the page looks one step behind and fetches
once more. It never misses a change.

Put the stream behind your sign-in. The topic checks read the session,
so the stream must come after your session middleware or behind sign-in.

Set `Options.Live`. Without it, an open stream holds shutdown for the
full 10-second drain.

A scaffolded app already has `static/live.js` and links it from its
layout. In an older app, run `rastrillo doctor --fix` and add the
`<script defer>` tag after `busy.js`.

## Tell the server after you write

```go
a.db.Create(&n)
a.live.Notify("notes/" + strconv.FormatInt(uid, 10))
```

Call `Notify` after the write has committed. If you call it before, a
page can fetch at once, read the old rows, and stay on them.

`Notify` never blocks. A topic you did not declare is logged and
skipped. On a nil hub it does nothing.

## Mark a region

A region is an element with an `id`, and a template named after it.

```html
{{define "region:notes-list"}}<div id="notes-list" {{.Live.Attrs .Content.Topic}}>
  ...the list...
</div>{{end}}
```

`Attrs` writes the attributes the script reads: the topics, each at
the version this page was built from. Never type them by hand.

The `region:` prefix matters. A request can only ever render a
template whose name starts with it. It cannot reach your layout or a
partial.

Use the region in the page like any template:

```html
{{template "region:notes-list" .}}
```

Add the head tag to your layout, and the notice region above your
content:

```html
{{.Live.Head (T $L "rastrillo.ui.live.paused") (T $L "rastrillo.ui.live.unconfirmed")}}
...
{{template "region:notice" .}}
```

`Head` writes the meta tag that tells the script where the stream is.
Leave it out and the page stays static. The two strings are notes the
script shows when updates pause, or when a change could not be
confirmed. Both come translated with Rastrillo.

The notice is the flash message. Define `region:notice` as a
`<div id="notice" role="status">` that is always on the page, empty
when there is no message. Screen readers announce a change to it.

Your page data needs a `Live live.View` field, set to `hub.View(r)`.

## Answer fragment requests

When a page asks for a region, the request carries a
`Rastrillo-Fragment` header. Your render function handles it in a
fixed order. The order is what keeps flash messages from being lost.

```go
func (a *app) renderStatus(w http.ResponseWriter, r *http.Request, status int, name string, content any) {
	if live.Elsewhere(w, r) {
		return
	}
	live.NoStore(w)
	names, frag := live.Wanted(r)
	if frag && !a.pages.HasRegions(name, names) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			live.Respond(w, r, nil, nil)
			return
		}
		frag = false // a form whose errors are another page: send that page whole
	}
	var fl flash.Flash
	var hasFlash bool
	if !frag || live.TakesFlash(r) {
		fl, hasFlash = flash.Take(w, r)
	}
	data := page{ /* ... */ Live: a.live.View(r), Flash: fl, HasFlash: hasFlash, Content: content}
	if frag {
		a.pages.WriteRegions(w, r, name, names, data)
		return
	}
	a.pages.Execute(w, name, data)
}
```

In words:

1. `Elsewhere` handles a form whose redirect went to another page. It
   answers "go there" and stops. It runs first so the flash waits for
   the page you land on.
2. `NoStore` stops caches. One URL now answers with a whole page or
   with a region, depending on the header.
3. `Wanted` reads the regions asked for. If the header is missing or
   odd, you render the whole page.
4. If the page has none of the regions asked for (the notice doesn't
   count, since every page has one), answer "go there" too. If the
   request is a form's POST, render the whole page instead: the form's
   errors are on another page.
5. `TakesFlash` says whether this render shows the notice. A whole page
   always does. A refresh
   caused by the stream does not, so it must not take the flash.
6. Render with `WriteRegions`. Pass it the `ResponseWriter` itself.
   It renders first, sets `Rastrillo-Regions`, then writes. If you
   wrote the body yourself, the header would be lost. See
   `examples/notes/internal/notes/render.go` for the full version,
   with error handling and a status code.

## Forms that swap in place

Add `data-live-form` to a form:

```html
<form method="post" action="/notes/{{.ID}}/delete" data-live-form>
```

The script sends the form with `fetch`. It swaps in the region the
form sits in, any regions you list in the attribute's value, and the
notice. With scripts off, the form posts and the redirect reloads the
page, as it always did.

Your handler does not change. It writes, calls `Notify`, sets a flash
and redirects.

If the answer is a 400 or 422, the script puts focus on the first
invalid field. If the answer is a 5xx, or nothing comes back, the
script does not know whether the change happened. It keeps what you
typed, says so in the notice, fetches the regions again, and does not
send the form a second time. Making a repeat submit safe stays your
job.

## What the script leaves alone

It never swaps a region while someone is editing in it, meaning a
focused field or a field changed from its default. The region waits
and updates when editing ends. Focus moves to the replacement, never
to `<body>`.

It reconnects on its own, with growing pauses. If updates stay down for
about ten seconds, or fetches keep failing, it shows the paused note,
unless the notice is already showing a message.

## On CARLOS

Do not turn live streams on for a route that hibernates. An open
stream keeps the instance awake. Until the platform can hold the
stream for you (carlos/platform#432), live pages belong on routes
that are always on.

For a route that hibernates, build no hub. A nil hub is safe
everywhere: `Attrs` and `Head` write nothing, `Notify` does nothing,
and the page is static, as it was before.
