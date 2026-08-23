# 🤖 Scoping

Keeping one user's rows away from another's comes down to a filter in
the SQL, applied at one place every query goes through.

## Scoping separates users, not teams

A CARLOS app serves one team. A product with many teams gives each team
its own instance — instances hibernate, so idle ones cost nothing — and
the isolation between them is the platform's process-and-file boundary,
not a `WHERE` clause.

So there is no `team_id` column in a Rastrillo app. Adding one means
designing a SaaS tenancy layer the architecture deliberately does not
have. What follows separates the users inside one instance.

## The seam

One method, and every read and write goes through it:

```go
func (a *app) owned(r *http.Request) *gorm.DB {
	uid, _ := sessions.UserID(r)
	return scope.Owned(a.db, uid)
}
```

`scope.Owned(g, owner)` adds `WHERE user_id = ?`. If your model's owner
column is called something else, `scope.OwnedBy(g, "author_id", owner)`
takes any column and any owner type. It panics unless the column name is
a plain lowercase identifier, since the name is interpolated into the
SQL rather than bound.

After that, no query on an owned model gets written without it. Not
`First`, not `Find`, not `Update`, not `Delete`.

### The keymail exception

`sessions.UserID` returns `(id, ok)`, and the method above drops `ok`.
That is fine for a plugin whose subject is a numeric user id, which the
password plugin's is.

It is wrong for keymail, where the subject is a verified email address.
`UserID` returns `(0, false)`, so this seam scopes every query in your
app to `user_id = 0`. Read the viewer with `auth.From(r)` there and map
the address to your user row's id before scoping.
[Magic links](/docs/magic-links) has the detail.

## Scope the write, not just the read

It is tempting to load the row through the seam, check it came back, and
then update it by primary key:

```go
n, err := a.find(r)                  // scoped read
a.db.Model(&n).Updates(update)       // unscoped write
```

Do this instead:

```go
n, err := a.find(r)
update := map[string]any{"Title": title, "Body": body}
a.owned(r).Model(&n).Select("Title", "Body", "UpdatedAt").Updates(update)
```

Now the SQL carries `WHERE user_id = ? AND id = ?` itself. The
difference shows up later: a refactor that moves the read, caches it, or
splits the handler in two cannot quietly turn the update into an IDOR.
The safety lives in the statement instead of in the order of two
statements.

## Inside a transaction, scope the transaction

```go
d.G.Transaction(func(tx *gorm.DB) error {
	return scope.Owned(tx, uid).First(&n, id).Error
})
```

Scope `tx`, never `d.G`. A `d.G` statement inside the callback runs
outside the transaction, against a pool whose one writer connection the
transaction is already holding — so it does not error, it hangs.

## 404, never 403

A row that is not yours is a row that does not exist.

```go
var n Note
if err := a.owned(r).First(&n, id).Error; err != nil {
	http.NotFound(w, r)
	return
}
```

403 tells an attacker the id is real and belongs to someone else, which
is the one fact worth not confirming. The scoped query hands you 404 for
free: another user's row comes back as `gorm.ErrRecordNotFound`, the
same as a row that was never there.

A malformed `{id}` is a 404 too, and needs no special case.
`strconv.ParseInt` failing gives you a zero, whose lookup returns
`gorm.ErrRecordNotFound` down the same path.

## Creating

Creating is the one place the owner does not come from a filter. It
comes from the session:

```go
uid, _ := sessions.UserID(r)
n := Note{UserID: uid, Title: title, Body: body}
```

Never from the form. A `UserID` field in a request body is how an app
lets one user create rows owned by another, and it is why
[Forms](/docs/forms) tells you never to bind a request onto a model.

## Join tables

A row linking two owned things is scoped through both sides. A
membership row needs the caller authorized on each side it links,
checked explicitly, and the stricter reading wins.

Scoping a join table once is a common and quiet mistake: it proves the
caller owns one end while saying nothing about the other.
