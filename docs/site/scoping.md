# 🤖 Scoping

A Rastrillo app keeps one user's rows away from another's with a filter
in the SQL, applied at one seam that every query goes through.

## Scoping is not tenancy

Scoping separates *users* within one instance. It never separates
teams.

A CARLOS app serves **one team**. A product with many teams gives each
team its own instance — instances hibernate, so idle ones cost nothing —
and the isolation between them is the platform's process-and-file
boundary, not a `WHERE` clause. There is no `team_id` column in a
Rastrillo app, and adding one is designing a SaaS tenancy layer the
architecture deliberately does not have.

What follows separates the users inside one instance.

## The seam

One method, and every read and write goes through it:

```go
func (a *app) owned(r *http.Request) *gorm.DB {
	uid, _ := sessions.UserID(r)
	return scope.Owned(a.db, uid)
}
```

`scope.Owned(g, owner)` adds `WHERE user_id = ?`. For a model whose
owner column is named something else, `scope.OwnedBy(g, "author_id",
owner)` takes any column and any owner type — and panics unless the
column name is a plain lowercase identifier, because the name is
interpolated into SQL rather than bound.

Then no query on an owned model is ever written without it. Not
`First`, not `Find`, not `Update`, not `Delete`.

### The seam has one sharp edge

`sessions.UserID` returns `(id, ok)`, and the seam above drops `ok`.
That is fine for a plugin whose subject is a numeric user id — the
password plugin — and wrong for one whose subject is something else.

Under the keymail plugin the subject is a **verified email address**, so
`UserID` returns `(0, false)` and this seam scopes every query in the
app to `user_id = 0`. Read the viewer with `auth.From(r)` there and map
the address to your user row's id before scoping.
[Magic links](/docs/magic-links) has the detail.

## Scope the write, not just the read

It is tempting to load the row through the seam, check it came back, and
then update it by primary key. Do not:

```go
n, err := a.find(r)                  // scoped read
a.db.Model(&n).Updates(update)       // unscoped write
```

Instead:

```go
n, err := a.find(r)
update := map[string]any{"Title": title, "Body": body}
a.owned(r).Model(&n).Select("Title", "Body", "UpdatedAt").Updates(update)
```

The SQL then carries `WHERE user_id = ? AND id = ?` itself. The
difference matters because of what happens later: a refactor that moves
the read, caches it, or splits the handler in two cannot silently turn
the update into an IDOR. The safety is in the statement rather than in
the sequence of statements.

## Inside a transaction, scope the transaction

```go
d.G.Transaction(func(tx *gorm.DB) error {
	return scope.Owned(tx, uid).First(&n, id).Error
})
```

Scope `tx`, never `d.G`. A `d.G` statement inside the callback runs
outside the transaction — against a pool whose one writer connection the
transaction is already holding — so it does not error, it **hangs**.

## 404, never 403

A row that is not yours is a row that does not exist. Answer 404.

```go
var n Note
if err := a.owned(r).First(&n, id).Error; err != nil {
	http.NotFound(w, r)
	return
}
```

403 tells an attacker that the id is real and belongs to someone else,
which is exactly the fact worth not confirming. The scoped query gives
you 404 for free: a row belonging to another user comes back as
`gorm.ErrRecordNotFound`, the same as one that was never there.

A malformed `{id}` is a 404 too. `strconv.ParseInt` failing hands you a
zero, whose lookup returns `gorm.ErrRecordNotFound` — the same path, no
special case needed.

## Creating

Creating is the one place the owner does not come from a filter. It
comes from the session:

```go
uid, _ := sessions.UserID(r)
n := Note{UserID: uid, Title: title, Body: body}
```

Never from the form. A `UserID` field in a request body is how an app
lets one user create rows owned by another, and it is the reason
[Forms](/docs/forms) says never to bind a request onto a model.

## Join tables

A row that links two owned things is scoped through **both** sides. A
membership row needs the caller authorized on each side it links,
checked explicitly, and the stricter reading wins.

A single scope on a join table is a common and quiet mistake: it proves
the caller owns one end while saying nothing about the other.
