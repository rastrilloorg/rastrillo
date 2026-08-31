# 🤖 flash

`amadan.net/rastrillo/rastrillo/flash`

One-shot notice messages carried in a cookie and cleared once read.

A flash is display state, not a record. Losing one to a cleared cookie
costs a notice, not data, which is what makes a cookie the right place
for it instead of a database table with a lifecycle to manage.

## Flash

```go
type Flash struct {
	Kind    string
	Message string
}
```

`Kind` is your own vocabulary — `"notice"`, `"error"` — and your layout
decides how each renders.

## Set

```go
func Set(w http.ResponseWriter, kind, message string)
```

Writes the cookie. Call it immediately before the redirect that will
show it:

```go
flash.Set(w, "notice", "Note created.")
http.Redirect(w, r, "/notes/"+id, http.StatusSeeOther)
```

## Take

```go
func Take(w http.ResponseWriter, r *http.Request) (Flash, bool)
```

Reads and clears in one call, which is the one-shot part. Call it once
per page from your render helper and let the layout render what it
returns; call it twice in one request and the second caller gets
nothing.

A missing or unparseable cookie answers `false` with a zero `Flash`
instead of an error. There is nothing useful an app could do about a
corrupt notice cookie except ignore it.
