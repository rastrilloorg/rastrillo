# Add email-and-password sign-in to a fresh scaffold

For a new app, `rastrillo new --auth=magic` already signs people in by
emailed link. This page is for password sign-in, or for an app made
without `--auth`: `rastrillo new` alone scaffolds no accounts. These
steps, applied to one, pass its `make ci` (`<app>` is the app's name).
Sessions, traps and other plugins: skill/signin.md.

**1. Model.** In `models.go`, add `&User{}` to `Models` and
`type User struct { ID int64; Email string \`gorm:"uniqueIndex"\`;
PasswordHash string; CreatedAt time.Time }`; run `rastrillo migration
generate`.

**2. Schemas.** In `migrations.go`, importing `sessions` and `pow`:

```go
var BootSchema = migrate.Merge(
	sessions.Schema, pow.Schema, apitoken.Schema, Schema)
```

**3. A background group, through every caller.** `New` and `Router`
take `group *background.Group` after `origin`. Five callers change:
`cmd/<app>/main.go` (`group := &background.Group{}`, and after
`Configure`, `options.Background = group`, which shutdown stops); in
`internal/<app>test`, `newApp` and the browser test (`group :=
&background.Group{}; t.Cleanup(group.Stop)` after the `database.Close`
cleanup, so it runs first) and both perf tests (`options.Background =
group` after `Configure`; `closeAll` stops it).

**4. Sign-in on `App`.** Add `sessions *sessions.Sessions` and
`passwords *password.Handlers` to `App`. In `Router`, after
`migrate.Apply`, call `signins, passwords, err := newSignin(database,
origin, group, logger)` and set both fields in `app := &App{...}`.
In a new `auth.go`:

```go
var powAssets = rastrillo.NewAssets(pow.Assets())

func newSignin(
	database *db.DB, origin string, group *background.Group,
	logger *slog.Logger,
) (*sessions.Sessions, *password.Handlers, error) {
	key, err := carlos.InstanceKey(os.Getenv("<APP>_INSTANCE_KEY"))
	if errors.Is(err, carlos.ErrNotOnCarlos) {
		key, err = "<app>-development-only-instance-key", nil
	}
	if err != nil {
		return nil, nil, err // ErrNoInstanceKey: on CARLOS with no key
	}
	signins, err := sessions.New(sessions.Config{
		DB: database.Writer(), Origin: origin, Logger: logger})
	if err != nil {
		return nil, nil, err
	}
	guard, err := pow.New(pow.Config{InstanceKey: key,
		Nonces:     pow.SQLNonces(database.Writer()),
		Difficulty: 16, MinAge: 500 * time.Millisecond,
		ScriptURL: "/pow" + powAssets.Path("pow.js"),
		WorkerURL: "/pow" + powAssets.Path("pow-worker.js")})
	if err != nil {
		return nil, nil, err
	}
	group.Loop(context.Background(), 10*time.Minute, func() {
		if err := guard.Sweep(time.Now()); err != nil {
			logger.Warn("sweep spent challenges", "err", err)
		}
	})
	passwords, err := password.New(password.Config{
		Sessions: signins, Proof: guard,
		Lookup: lookupUser(database.G), Create: createUser(database.G),
		RenderSignin: renderSignin, RenderSignup: renderSignup})
	return signins, passwords, err
}
```

**5. Routes.** In `routes.go`, add `const accountPage action.Path =
"/account"` (it stands for your owned pages), and make `Routes`:

```go
func Routes(routes *action.Routes, app *App) {
	routes.Mount("/static", assets.Handler(), "static")
	routes.Mount("/api/v1", app.api, "api")
	routes.Mount("/pow", http.StripPrefix("/pow/", powAssets.Handler()),
		"pow")
	routes.Group(func(guarded *action.Routes) {
		guarded.Guard("csrf", csrf.Protect(app.origin))
		guarded.Guard("pages", pages.Middleware)
		// The layout reads the session on every page.
		guarded.Guard("signedIn", app.sessions.Middleware)
		app.mountSignin(guarded)
		guarded.Get(homePage, app.home)
		guarded.Group(func(owned *action.Routes) {
			owned.Guard("session", app.sessions.Require)
			owned.Get(accountPage, app.home)
		})
	})
}
```

and in `auth.go`:

```go
func (app *App) mountSignin(routes *action.Routes) {
	passwords := app.passwords
	routes.Handle("GET", "/signin",
		http.HandlerFunc(passwords.SigninPage), "signinPage")
	routes.Handle("POST", "/signin",
		http.HandlerFunc(passwords.Signin), "signin")
	routes.Handle("GET", "/signup",
		http.HandlerFunc(passwords.SignupPage), "signupPage")
	routes.Handle("POST", "/signup",
		http.HandlerFunc(passwords.Signup), "signup")
	routes.Handle("POST", "/signout",
		http.HandlerFunc(passwords.Signout), "signout")
}
```

`lookupUser(database *gorm.DB)` returns `func(ctx, email) (int64,
string, error)`: `database.WithContext(ctx).Where("email = ?",
email).First(&user)`, mapping `gorm.ErrRecordNotFound` to
`sql.ErrNoRows` (no scope: a visitor signing in is owned by nobody
yet). `createUser(database)` returns `func(ctx, email, hash) (int64,
error)`, which creates the `User` and returns its id.
`renderSignin`/`renderSignup` are `func(response, request, data
password.PageData)`: `renderPage(response, request, "signin",
http.StatusOK, map[string]any{"Content": data})`, and `"signup"`
for the other (the forms' proofs are scoped, so signup's form must post
to `/signup`). Pass `http.StatusOK`, never another status: the
password handlers write theirs first, and `writePage` writes no 200.

**6. Pages.** Add `"signin", "signup"` to `render.go`'s page list,
wrapped to fit 80 columns. In
`withRequest`, before its return: `_, values["SignedIn"] =
sessions.Current(request)`. In `layout.html`, after the scheme menu's
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

**8. Tests.** Sign up through the real form: `page := get(t, handler,
"/signup")`; `time.Sleep(600 * time.Millisecond)` (past `MinAge`);
`recorder := post(t, handler, "/signup", powtest.Fill(t,
page.Body.Bytes(), url.Values{"email": {email}, "password":
{secret}}))`; expect 303 and take the cookie whose name ends
`rastrillo_session` from `recorder.Result().Cookies()`; add it to later
requests. The scaffold's
`post` already sends `Origin: testOrigin`. In `TestPerfScreens`,
`ScreenConfig.Signin: func(client *http.Client, base string)` does the same
over HTTP: GET `base+"/signup"`, sleep, `powtest.Fill`, then POST with
`Content-Type: application/x-www-form-urlencoded` and **`Origin:
testOrigin`**, not `base` (`csrf.Protect` compares with the app's
origin, not the test server's). Owned GET routes are then measured.
