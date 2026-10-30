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

A change that happens at a time, such as a post going live at 9:00,
has no write to follow. Tell the hub when it is due:

```go
a.live.NotifyAt(p.PublishAt, "posts")
```

Call it at boot for each future time you have stored, and whenever a
schedule changes. See [`NotifyAt`](/docs/reference/live#notifyat).

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
`Rastrillo-Fragment` header. Answer it with `Pages.Render`. It handles the header in the one order
that keeps flash messages from being lost:

```go
func (a *app) renderStatus(w http.ResponseWriter, r *http.Request, status int, name string, content any) {
	err := a.pages.Render(w, r, status, name, func(takeFlash bool) (any, error) {
		var fl flash.Flash
		var hasFlash bool
		if takeFlash {
			fl, hasFlash = flash.Take(w, r)
		}
		return page{ /* ... */ Live: a.live.View(r), Flash: fl, HasFlash: hasFlash, Content: content}, nil
	})
	if err != nil {
		http.Error(w, "something went wrong", http.StatusInternalServerError)
	}
}
```

Your function builds the page data, and takes the flash only when
`takeFlash` is true. A refresh caused by the stream does not show the
notice, so it must not take the flash. When the answer is "go
somewhere else" (a form whose redirect went to another page, or a
request for regions this page does not have), your function is not
called at all, and the flash waits for the page you land on.

`status` is your response's status, such as `422` for a form with
errors; `0` means `200`. If your function, the localizer check or a
template fails, nothing has been written, and you send your own error.
An error while writing the response can come after it has started.

With your own template map instead of `ui.Pages`, call
[`live.Render`](/docs/reference/live#render) with the page's
templates. See `examples/notes/internal/notes/render.go` for the full
version.

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
