# Interop checklist: real Entra ID and Google Workspace tenants

This package's own test suite exercises every check this flow makes
against a fake identity provider — real TLS, real RS256 signing, the
exact claim shapes both providers publish (multi-value `aud`, string-vs-
array `email_verified`, Microsoft's placeholder-issuer discovery
document, Google's `hd`). What it cannot do is prove this package
actually works against the REAL providers, which need a registered
application and a live sign-in a human has to perform.

Per the design (`docs/superpowers/specs/2026-10-26-oidc-signin-design.md`,
Testing item 7 and Open question 2), this checklist is written so the run
can be reproduced and its outcome posted, not kept in one person's head.
Whoever has reachable Entra and Google tenants runs this before the
branch merges and posts the result (pass/fail per step, and anything that
needed a workaround) to a discussion on the `oidc-signin` branch.

## Setup

A minimal test app wiring `oidc.New` with:

```go
Connections: func(ctx context.Context, kind oidc.Kind, key oidc.ConnectionKey) (oidc.Connection, bool, error) {
    switch kind {
    case oidc.Microsoft:
        return oidc.Connection{
            Issuer:       mustIssuer(oidc.Microsoft, os.Getenv("TEST_ENTRA_TENANT")),
            ClientID:     os.Getenv("TEST_ENTRA_CLIENT_ID"),
            ClientSecret: os.Getenv("TEST_ENTRA_CLIENT_SECRET"),
            RedirectURI:  origin + "/signin/oidc/microsoft/return",
        }, true, nil
    case oidc.Google:
        return oidc.Connection{
            Issuer:         "https://accounts.google.com",
            ClientID:       os.Getenv("TEST_GOOGLE_CLIENT_ID"),
            ClientSecret:   os.Getenv("TEST_GOOGLE_CLIENT_SECRET"),
            RedirectURI:    origin + "/signin/oidc/google/return",
            AllowedDomains: strings.Split(os.Getenv("TEST_GOOGLE_ALLOWED_DOMAINS"), ","),
        }, true, nil
    }
    return oidc.Connection{}, false, nil
},
Admit: func(ctx context.Context, conn oidc.Connection, id oidc.Identity) (string, error) {
    return "test-subject-" + id.Address, nil // admit everyone for this checklist only
},
```

### Microsoft Entra ID

1. Register an application in a real Entra tenant (any tenant you
   control — a free developer tenant works). App registrations ->
   New registration.
2. Add a Web redirect URI matching `RedirectURI` above, exactly.
3. Create a client secret. Set `TEST_ENTRA_CLIENT_ID`,
   `TEST_ENTRA_CLIENT_SECRET`, `TEST_ENTRA_TENANT` (the Directory/tenant
   ID — a GUID, not the domain) from the registration's Overview page.
4. Add `openid`, `email`, `profile` under API permissions (these are
   the default `scope` this package requests and are usually granted by
   default).

| Step | Expected | Pass/fail |
| --- | --- | --- |
| Visit `/signin/oidc/microsoft` | 303 to `login.microsoftonline.com`, with `prompt=select_account` | |
| Sign in with a real account IN the tenant | Redirected back, session minted, `Identity.HostedDomain` empty (Entra does not emit `hd`), `Identity.Tenant` is the tenant GUID | |
| Sign in with a Microsoft personal account (not in the tenant) | Refused — Entra itself should refuse this at its own consent screen before returning, since the app registration is single-tenant | |
| Configure the connection with `/common` as the directory value | `Admitted`/`Discover` refuses it (this package's own multi-tenant placeholder check) before any real sign-in attempt | |
| Check `Identity.AddressVerified` | `Unstated` — confirm Entra really does omit `email_verified`, as the design assumes | |
| Check `Identity.Address` | Comes from `email`, or `upn`/`preferred_username` if `email` is absent — confirm which one a REAL Entra token actually carries for your tenant's account shape | |

### Google Workspace

1. Create an OAuth client (Web application) in a Google Cloud project
   tied to a real Workspace domain you control. APIs & Services ->
   Credentials.
2. Add an authorised redirect URI matching `RedirectURI` above, exactly.
3. Set `TEST_GOOGLE_CLIENT_ID`, `TEST_GOOGLE_CLIENT_SECRET`,
   `TEST_GOOGLE_ALLOWED_DOMAINS` (comma-separated, your Workspace
   domain(s)) from the client.

| Step | Expected | Pass/fail |
| --- | --- | --- |
| Visit `/signin/oidc/google` | 303 to `accounts.google.com`, `prompt=select_account` | |
| Sign in with a real account IN the Workspace domain | Redirected back, session minted, `Identity.HostedDomain` equals the domain, `Identity.AddressVerified == Asserted` | |
| Sign in with a personal Gmail account | Refused by `Admitted` — `HostedDomain` is empty, which this package treats as "that account carries no organisation claim this connection can check" | |
| Sign in with an account in a DIFFERENT Workspace domain not in `AllowedDomains` | Refused by `Admitted` — `HostedDomain` present but not allowed | |

## After running this

Post the filled-in tables, which provider(s) you actually reached, and
anything that needed a workaround not described above, to a discussion
on the `oidc-signin` branch. A checklist step that cannot be exercised
(no reachable tenant of that kind) is recorded as "not run", not
silently skipped — this is exactly the kind of gap the design's own
honesty rules ask to be stated rather than implied.
