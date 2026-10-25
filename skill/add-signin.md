# Add email-and-password sign-in to a fresh scaffold

`rastrillo new` scaffolds no accounts. These steps, applied to one,
pass its `make ci` (`<app>` is the app's name; `/account` stands for
your owned routes). Sessions, traps and other plugins: skill/signin.md.

**1. Model.** In `models.go`, add `&User{}` to `Models` and
`type User struct { ID int64; Email string \`gorm:"uniqueIndex"\`;
PasswordHash string; CreatedAt time.Time }`; run `rastrillo migration
generate`.

**2. Schemas.** In `migrations.go`: `var BootSchema =
migrate.Merge(sessions.Schema, pow.Schema, Schema)`.

**3. A background group, through every caller.** `App` and `Router`
take `bg *background.Group` after `origin`. Five callers change:
`cmd/<app>/main.go` (`bg := &background.Group{}`, and after
`Configure`, `opts.Background = bg`, which shutdown stops); in
`internal/<app>test`, `newApp` and the browser test (`bg :=
&background.Group{}; t.Cleanup(bg.Stop)` after the `d.Close` cleanup,
so it runs first) and both perf tests (`opts.Background = bg` after
`Configure`; `closeAll` stops it).

**4. Router**, after `migrate.Apply`:

```go
key, err := carlos.InstanceKey(os.Getenv("<APP>_INSTANCE_KEY"))
if errors.Is(err, carlos.ErrNotOnCarlos) {
	key, err = "<app>-development-only-instance-key", nil
}
if err != nil {
	return nil, err // ErrNoInstanceKey: on CARLOS with no key; never start
}
writer, err := d.G.DB()
if err != nil {
	return nil, err
}
sess, err := sessions.New(sessions.Config{DB: writer, Origin: origin, Logger: logger})
if err != nil {
	return nil, err
}
powAssets := rastrillo.NewAssets(pow.Assets())
guard, err := pow.New(pow.Config{InstanceKey: key, Nonces: pow.SQLNonces(writer),
	Difficulty: 16, MinAge: 500 * time.Millisecond,
	ScriptURL: "/pow" + powAssets.Path("pow.js"), WorkerURL: "/pow" + powAssets.Path("pow-worker.js")})
if err != nil {
	return nil, err
}
bg.Loop(context.Background(), 10*time.Minute, func() { guard.Sweep(time.Now()) })
ph, err := password.New(password.Config{Sessions: sess, Proof: guard,
	Lookup: lookupUser(d.G), Create: createUser(d.G),
	RenderSignin: renderSignin, RenderSignup: renderSignup})
if err != nil {
	return nil, err
}
r := chi.NewRouter()
r.Use(csrf.Protect(origin))
r.Use(sess.Middleware) // the layout reads the session on every page
r.Get("/signin", ph.SigninPage)
r.Post("/signin", ph.Signin)
r.Get("/signup", ph.SignupPage)
r.Post("/signup", ph.Signup)
r.Post("/signout", ph.Signout)
r.Handle("/pow/*", http.StripPrefix("/pow/", powAssets.Handler()))
r.Get("/", a.index)
r.Group(func(r chi.Router) {
	r.Use(sess.Require)
	r.Get("/account", a.index) // your owned routes
})
```

**5. `auth.go`.** `lookupUser(g *gorm.DB)` returns `func(ctx, email)
(int64, string, error)`: `g.WithContext(ctx).Where("email = ?",
email).First(&u)`, mapping `gorm.ErrRecordNotFound` to `sql.ErrNoRows`
(no scope: a visitor signing in is owned by nobody yet).
`createUser(g)` returns `func(ctx, email, hash) (int64, error)`, which
creates the `User` and returns its id. `renderSignin`/`renderSignup`
are `func(w, r, d password.PageData)`: `render(w, r, "signin",
map[string]any{"Content": d})` and `render(w, r, "signup", ...)`
respectively (the forms' proofs are scoped, so signup's form must post
to `/signup`); never write a status in them.

**6. Pages.** Add `"signin", "signup"` to `render.go`'s page list. In
`render`, after the page data: `_, page["SignedIn"] =
sessions.Current(r)`. In `layout.html`, after the scheme menu's
`{{end}}`: `{{if opt $ "SignedIn"}}<form method="post"
action="/signout"><button rst-btn>Sign out</button></form>{{end}}`.
`templates/signin.html` (for `signup.html`: `/signup`, `new-password`,
`Sign up`, and a link back to `/signin`):

```
{{define "content"}}{{$L := localizer $}}
{{template "page-header" dict "L" $L "Title" "Sign in"}}
{{with .Content}}
{{template "form-error" (withL (.Error) $L)}}
<form rst-form method="post" action="/signin"{{with .Proof}} {{.Attrs}}{{end}}>
{{with .Proof}}{{.Fields}}{{end}}
<input type="hidden" name="return_to" value="{{.ReturnTo}}">
{{template "field-text" dict "L" $L "Name" "email" "Label" "Email" "Type" "email" "Value" .Email "Required" true "Autocomplete" "email"}}
{{template "field-text" dict "L" $L "Name" "password" "Label" "Password" "Type" "password" "Required" true "Autocomplete" "current-password"}}
{{template "form-foot" dict "L" $L "Submit" "Sign in" "Proof" .Proof}}
</form>
{{with .Proof}}{{.Script}}{{end}}
{{end}}
<p><a href="/signup">Need an account? Sign up</a></p>
{{end}}
```

**7. Budgets.** Add `perf  GET /pow/*  skip  proof-of-work script and
worker assets, not a screen` to `.rastrillo/budgets.txt`.

**8. Tests.** Sign up through the real form: `page := get(t, h,
"/signup")`; `time.Sleep(600 * time.Millisecond)` (past `MinAge`);
`rec := post(t, h, "/signup", powtest.Fill(t, page.Body.Bytes(),
url.Values{"email": {e}, "password": {p}}))`; expect 303 and take the
cookie whose name ends `rastrillo_session` from
`rec.Result().Cookies()`; add it to later requests. The scaffold's
`post` already sends `Origin: testOrigin`. In `TestPerfScreens`,
`ScreenConfig.Signin: func(c *http.Client, base string)` does the same
over HTTP: GET `base+"/signup"`, sleep, `powtest.Fill`, then POST with
`Content-Type: application/x-www-form-urlencoded` and **`Origin:
testOrigin`**, not `base` (`csrf.Protect` compares with the app's
origin, not the test server's). Owned GET routes are then measured.
