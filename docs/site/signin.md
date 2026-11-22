# 🤖 The default sign-in

A new app with accounts signs people in by emailed link, and lets each
person add a faster way in afterwards: a passkey, an authenticator app
or a passphrase. The next time they come back, the sign-in screen leads
with whichever way that browser used last.

`rastrillo new --scaffold accounts <name>` sets all of it up. The
`signin` package does the assembling, on top of
[magic links](/docs/magic-links), [passkeys](/docs/passkeys) and
[authenticator apps](/docs/second-factors#totp), so this page is mostly
about what people see and the few things you are likely to change.

## What a new app gets

The first screen is one email field. If the address has a Keymail
inbox, the person goes straight to Keymail to approve the sign-in;
anyone else gets a link by email. Nothing on the screen names Keymail.

After a link, a person with no other way in sees "Sign in faster next
time". It offers a passkey first, marked as recommended, then an
authenticator app, then a passphrase, and "Not now". Once they add a
way or say "Not now", it never asks again. Someone who signed in with
Keymail never sees the offer, because Keymail already protects their
account with a passkey.

Each way they add is a way in on its own, not a second step. The email
link always stays: it is how someone gets back in after losing a phone,
a passkey or a passphrase.

The account page links to **Sign-in options**, at `/account/signin`,
where a person sees the ways they have, adds the ones they lack and
removes any but the email link. Adding or removing a way needs a
sign-in from the last ten minutes; if theirs is older, they sign in
again first and come straight back.

## Coming back

The screen remembers, in each browser, how it was last used to sign in
and which address signed in. It leads with that way:

| Last used | What the screen shows |
|---|---|
| Email link or Keymail | "Continue as" the address, one tap |
| Passkey | "Sign in with your passkey" |
| Authenticator app | The address, filled in, and a field for the code |
| Passphrase | The address, filled in, and a field for the passphrase |

Under it, "Other ways to sign in" opens a list of every way the app
turns on, plus "Use a different email", which forgets the remembered
address. The list is the same for everyone. It cannot show only the
ways a given address has, because that would tell a stranger which
addresses have accounts. Choosing a way the person never added fails
exactly as it would for an address with no account.

Signing out does not forget the way, so the person who just left finds
their way first when they come back. The memory lasts 400 days, renewed by each sign-in.
A wrong code or passphrase counts against the account, whichever of its
addresses was typed: after five misses in fifteen minutes it rests for
a while, and the screen tells
them to email themselves a link, which always works.

## Adding a sign-up step

Some apps need something from a new person before they start, such as
their name. Give `signin.Config` a `Step` for it: a page of yours, and a
function that says whether this person still owes it.

```go
const namePage action.Path = "/welcome/name"

// needsName is true until the person has told us their name.
func (app *App) needsName(
	ctx context.Context, subject string,
) (bool, error) {
	var count int64
	err := app.database.G.WithContext(ctx).Model(&Profile{}).
		Where("owner = ? AND name <> ''", subject).Count(&count).Error
	return count == 0, err
}
```

In `newSignins` in `auth.go`, add the step to the config:

```go
return signin.New(signin.Config{
	// ...the fields the scaffold wrote, then:
	Steps: []signin.Step{{Path: namePage, Needed: app.needsName}},
})
```

The step is your page, so you mount and draw it. Put it in the owned
group, behind `RequireSession`, since only a signed-in person has a
name to give:

```go
owned.Get(namePage, app.askName)
owned.Post(namePage, app.saveName)
```

The form posts `name`, plus a hidden `return_to` copied from the page's
query. Once you have saved the name, call `Next` instead of redirecting
yourself:

```go
func (app *App) saveName(visit *action.Visit) {
	request := visit.Request()
	profile := Profile{Owner: ownerOf(request),
		Name: strings.TrimSpace(request.PostFormValue("name"))}
	if visit.Refused(app.database.G.Create(&profile).Error) {
		return
	}
	app.signins.Next(visit.Response(), request)
}
```

`Next` goes on to the next step still needed, then the offer if it is
due, then wherever the person was going. Until the step is done, the
gate sends every page a signed-in person opens to it, carrying the page
they asked for as `return_to`. Closing the tab halfway does not lose
anything: the next visit lands on the unfinished step, whichever way
they signed in. The gate only redirects page loads, so form posts, API
calls, assets and the sign-in routes pass through untouched.

The step runs after the email is proved, never before. The first
screen cannot tell a new person from a returning one without saying
who has an account, and someone using Keymail never sees a link.

## Turning ways off

`Config.Ways` says which ways an app offers beyond the link. A new app
has all three:

```go
return signin.New(signin.Config{
	// ...
	Ways: signin.Passkey | signin.Authenticator | signin.Passphrase,
})
```

Leave a way out and nothing of it is mounted: no add page, no door on
the screen, no row in the offer or on Sign-in options. With `Ways: 0`
the app signs in by link alone, and the offer and Sign-in options pages
are not mounted at all. If you turn off a way people already use, their
browsers fall back to the email field and the stored credentials stay
in the database, unused.

## Keymail

Keymail is on in a new app, for any server an address's domain names.
Your server is the one that connects to that server, so every
connection it makes for Keymail may only reach a public address: never
this machine, a private network, the 100.64.0.0/10 range Tailscale
uses, or a link-local, multicast or unspecified address. The check runs as each
connection opens, after DNS resolves the name, so a domain that changes
its answer cannot get around it, and each redirect is checked too.
[Magic links](/docs/magic-links#aside-the-keymail-upgrade) explains how
to limit Keymail to servers you list instead.

## What signin will not assemble

`signin.New` refuses an `auth` in records mode, with blind links, or
with a `SecondFactor`. Every way here is a whole sign-in, and a second
factor on top would ask an authenticator user for the same code
twice. If your `auth.Config.SubjectFor` creates rows, give
`signin.Config.Lookup`, which finds a subject without creating one. The
[signin reference](/docs/reference/signin) has the rest of the
configuration.
