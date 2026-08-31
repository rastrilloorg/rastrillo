# 🤖 scope

`amadan.net/rastrillo/rastrillo/scope`

Owner-filtered GORM scopes. Two functions, and that is the whole
package. It exists to make one discipline the short path, so every query
on a model somebody owns carries its owner in the SQL and a handler
cannot read or write another user's row by accident.

Scoping separates users within one instance. It is not tenancy: a CARLOS
app serves one team, and a product with many teams gives each team its
own instance. [Scoping](/docs/scoping) has the reasoning and the handler
patterns; this page is the surface.

## Owned

```go
func Owned(g *gorm.DB, owner int64) *gorm.DB
```

Returns `g` with `WHERE user_id = ?` applied. This is the common case,
and the one most apps wrap in an `owned(r)` method:

```go
func (a *app) owned(r *http.Request) *gorm.DB {
	uid, _ := sessions.UserID(r)
	return scope.Owned(a.db, uid)
}
```

Notice what that method drops. `sessions.UserID` returns `(id, ok)`, and
`ok` is false for an identity plugin whose subject is not a numeric user
id. The magic-link plugin's subject is a verified email address, so
`UserID` returns `(0, false)` and this scopes every query to
`user_id = 0`. Read the viewer with `auth.From(r)` there and map the
address to your user row's id first —
[Magic links](/docs/magic-links) covers it.

## OwnedBy

```go
func OwnedBy(g *gorm.DB, column string, owner any) *gorm.DB
```

The same filter against any owner column, for a model that does not call
it `user_id`, and against any owner type — a string subject as readily
as an `int64` id.

```go
rows := scope.OwnedBy(a.db, "author_id", subject).Find(&posts)
```

The column name is interpolated into the SQL rather than bound, because
a column name cannot be a placeholder. So `OwnedBy` panics unless the
name matches `^[a-z][a-z0-9_]*$`.

The panic is the design. A column name reaching this function from
anywhere but a Go string literal is a bug, and crashing on the first
call in development is louder than a subtly wrong query in production.
There is no error-returning variant to reach for instead: if the name is
not a constant in your source, make it one.
