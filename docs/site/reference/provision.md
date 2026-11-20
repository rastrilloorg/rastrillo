# 🤖 provision

`amadan.net/rastrillo/rastrillo/provision`

Use `provision` in a central app, such as a home or a hub, to create tenant instances on CARLOS and tell when each one is ready. A hub also uses it to publish its path map and to check that each tenant's path reaches that tenant.

A home gives each person an instance on its own host. A hub serves every tenant from one hostname, at `https://<product>/<slug>/`, and the platform sends each prefix to that tenant's instance.

`provision` keeps no records. It tells you what happened, and your app writes the record. Follow the record rules below, or a crash or a second refresh can leave an instance nobody owns.

## Creating an instance

Make the host with `NewLabel(slug)`, then a dot and the tenant app's instance domain. The label is the slug, cut to `MaxSlug` characters, then a hyphen and six random characters. A slug is lower-case letters, digits and inner hyphens; anything else is `ErrSlug`.

Pass the host to `Create`. Only the first call for a host leaves `Resume` false. Every later call sets `Resume: true`, so `Create` checks whether the instance already exists before it posts again.

For a hub, create each tenant in the hub's region. Leave `Region` empty when the tenant app's default region is the hub's. Otherwise pass the hub's `$CARLOS_REGION`.

What to do with each result:

| `Create` returns | What to do |
| --- | --- |
| `Created` | The instance exists. Wait for `Ready`. |
| `Created` with `Landed` | An earlier call for this host created it. Treat it as created. |
| `Created` with `Accepted` | The platform is still creating it. Keep the record `creating` and wait for `Ready`. You do not need to call `Create` again. |
| `ErrHostTaken` | Another creation owns this host. In the signup request only, make a new label and create again, at most `MaxRelabels` times. If a refresh has taken the record over, keep the host instead. |
| `*RetryError` | The create may have landed, or the platform said to try later. Keep the host. Call again with `Resume: true`, after `After` if it is set. |
| Your context's error | The create may have been sent. Keep the host and call again with `Resume: true`. |
| `ErrConflict` | The platform refused, for example because instances are not enabled for the app. A new label will not help. Mark the record `failed`. |
| `ErrUnauthorized`, `ErrForbidden` or `ErrNotFound` | The credential is wrong, lacks the role, or belongs to another app. Fix the credential. Retrying will not help. |
| `ErrBadRequest` | The platform refuses this host or message. `Create` checks both first and sends nothing when it can tell. Fix the request. |

Any error can come with `Created.MayHaveLanded` set. It means this call sent a create and never got its answer, so the instance may exist even when a later post was refused. Keep the host and call again with `Resume: true`, whatever the error says.

Every refusal is an `*APIError` with the platform's status and sentence, and matches one of the errors above with `errors.Is`. The errors are for logs and records. Show people your own sentences.

`Instance` reads the platform's record of a host. A host that does not exist is `ErrNotFound`. An answer that does not name the host is a `*RetryError`: ask again later.

Set `Config.Observe` to log every console call. It never sees the token.

`provision` cannot delete an instance. A person deletes one with `carlos instances delete`.

## Listing instances

`List` reads one page of the app's instances, each with the console's health word: `Setting up`, `Running`, `Serving`, `Asleep`, `Failed`, `Not responding`, `No readings` or `Can't tell right now`. The console also has `Waking`, `Crashed` and `Never started` for sidecar instances, and adds words over time, so show a word you do not know as `HealthUnknown`. The console works the word out from its own readings, so a list never touches an instance and never wakes a sleeping one. Use it to show a fleet; use `Ready` to ask whether one instance answers.

```go
page, err := console.List(ctx, provision.ListRequest{Query: "acme", Limit: 100})
```

`Limit` is 1 to 499, and 0 lets the console choose. When `page.Next` is not empty, pass it as `Cursor` for the next page. The credential needs the `instance` or `deploy` role.

`List` never retries. A 429, a 5xx or no answer is a `*RetryError`, and the credential's 60 calls a minute are shared with `Create`, so a caller that retries blindly can starve a signup. Back off by `After`, and ask again no more often than a person looks at the page. An answer over 4 MiB is an error, not a short page.

## Telling when it is ready

`Ready` asks the tenant's own host for `/api/version`. It is true only for a 200 whose `Rastrillo-Host` header names that host. The platform's setting-up page, a redirect and every other answer are not ready. An error also means not ready yet.

Call `Ready` once per page refresh, and mark the record `live` when it is true. In a script or a test, `WaitReady` checks until the instance is ready or your context ends.

Build every tenant you create with `rastrillo.Options.NameInstance` set. Without it, `Ready` and `CheckRoute` never see the tenant. Set it on the hub too, so `CheckRoute` can tell when the hub answered.

The probes never follow a redirect. Do not redirect `/api/version` on the tenant's own host, or it never reads ready.

## Publishing a hub's paths

`PublishPaths` sends the hub's whole path map, from each slug to its tenant's host. The platform replaces the map each time, so `build` must return every tenant, not only the new one. Include every `live` tenant, routed or not, and leave out tenants still `creating`.

Nothing is sent if `build` fails, if the map is empty (`ErrEmptyMap`), or if a prefix or host is one the platform would refuse (`ErrBadRequest`). Read every page of your records in `build`, and return an error if any page or record fails to read.

A 500 or 502 is retried once. A second one, or a 429, is a `*RetryError`. Publish again later.

Publishes for one hostname take turns within one process. Two processes can still overwrite each other's new prefix. Republish the map from your records on a daily `carlos schedule` tick to repair it.

You cannot publish an empty map. When your last live tenant fails, its prefix stays published and may still reach its instance. To clear it, send a `PUT` of `{"paths": {}}` to the same console endpoint yourself.

## Checking the path

After a publish, `CheckRoute` asks `https://<product>/<slug>/api/version` who answered. Call it once per refresh until it returns `Routed`. It reports one of four states:

- `Routed`: the path reaches the tenant. Mark the record routed and send people there.
- `RoutePending`: not known yet. Check again.
- `FellThrough`: the hub answered. A new map can take a few seconds to go live, so keep checking until your deadline.
- `NoBasePath`: the tenant answered 404 under its prefix. Build it to serve under `/<slug>`.

`Route.Err` returns the error for a state, for the record's failure text. `WaitRouted` checks until the path is routed or your context ends, and its error says why it was not.

## Keeping the record

Your app writes one record per tenant. Follow these rules, so that a crash, a retry or two refreshes at once never make a second instance or lose one.

1. Write the record as `creating`, with its host, before the first `Create`.
2. In that first write, also store a random token and the signup's deadline, and run the signup's `Create` calls under that deadline. When the signup is done calling `Create`, it clears the token.
3. Keep that host until `Create` returns `ErrHostTaken`. After any other error the instance may exist.
4. Only the signup request that wrote the record calls `Create` without `Resume`. A refresh, a retry button and a repair all pass `Resume: true`.
5. A refresh leaves a record that has a token alone until its deadline. After the deadline, the refresh clears the token with a conditional update before it calls `Create`.
6. Make every change a conditional update, such as `objrec.UpdateJSON` with the version you read. If the record changed (`objrec.ErrChanged`), read it again and decide again.
7. Pick a new label only in the signup request, and only with a conditional update on the version the signup wrote. If the record changed, a refresh has taken it over: stop, and keep the host. A record keyed by slug gets the new host. For a record keyed by label, first mark the old record `replaced`, naming the new label. Then create the new record, and then delete the old one.
8. Never call `Create` for a `replaced` record. A refresh that finds one creates the record it names, if that record is missing, and then deletes the `replaced` one.
9. Count new labels in the record, and stop at `MaxRelabels`. A count kept in memory starts again on every refresh.
10. Keep the record `creating` until `Ready` is true, then mark it `live`. A hub also keeps `routed`, false until `CheckRoute` returns `Routed`, and sends people only to a routed tenant. A tenant still not routed at your deadline becomes `failed`, with the error's text.

## Custom domains

`AttachDomain(ctx, host, route)` claims a hostname, such as `blog.example.org`, and serves it as `route`, one of the tenant app's instance hosts. The credential needs the `instance` or `publish` role.

```go
attached, err := console.AttachDomain(ctx, "blog.example.org", instanceHost)
```

On a platform that asks for proof of ownership, `attached.Verified` is false and `attached.Verify` is the TXT record the owner publishes at `_carlos-challenge.<host>`. There is no separate verify call: attach again once the record is published. Attaching a hostname the app already holds is safe to repeat, and moves it to the new `route`. A hostname another app or account holds is `ErrConflict`, and the platform never says who holds it.

`Domain(ctx, host)` reads one hostname. `State` is the platform's word for it, with a machine-readable `Reason`:

| `State` | Meaning |
| --- | --- |
| `DomainUnverified` | The TXT record is not published yet. Nothing is served. |
| `DomainChecking` | No edge has checked the name yet. It settles within minutes. |
| `DomainPendingDNS`, `DomainMisdirected` | DNS has no record, or points elsewhere. |
| `DomainIssuingCert` | DNS points here, and a certificate is on its way. |
| `DomainFailed` | The platform cannot get a certificate. Nothing for the owner to change. |
| `DomainLive` | DNS points here, and a current certificate covers it. |
| `DomainUnknown` | The platform could not check. Not the owner's fault. |
| `DomainRegional` | Served per region; not something an instance sets up. |

The platform adds words over time, so treat one you do not know as `DomainUnknown`. `Records` and `Guidance` say what to publish, and are empty once DNS points here. Records that share a `Set` are one choice, such as one CNAME or a set of A records: publish every record of one set. A `Required` record is needed whichever set is chosen. `DNS` is the platform's `dns_status`, `CNAMETarget` is the name a CNAME points at, `CheckedAt` is when an edge last looked, and `CertExpiry` is zero until there is a certificate. A hostname the app does not hold is `ErrNotFound`, even when someone else holds it.

`DetachDomain(ctx, host)` releases a hostname. Releasing one the app does not hold succeeds and changes nothing, so a retry is safe.

The three calls classify errors as `List` does. A 429, a 5xx or no answer is a `*RetryError`, and a 200 that cannot be read is one too, since it may be a proxy's page. Every attach and detach counts against the credential's limit for domain changes, so do not call them in a tight loop. A domain read is limited to 8 MiB, because it carries the hostname's whole path map.

## Credentials

Make one `Console` per credential, and use credentials bound to one app. An account-wide credential reaches every app in the account. A home needs an `instance` credential for the tenant app. A hub needs that one, and a `publish` or `instance` credential for the hub app to publish paths. Keep both in the central app; a tenant needs neither. An `instance` credential deletes as well as creates: it reaches every instance of the tenant app, with their domains and logs, and the platform has no create-only role yet. List that in the leak profile too.

Set `Config.Account` to the account's sqid, not its name. The console answers 404 to a name.

If your hub publishes a leak profile, list two things in it: the hub-app credential can do much more than publish paths, and the `Rastrillo-Host` header shows any visitor the platform hostname behind each tenant's path.

## API

```go
const DefaultTimeout = 30 * time.Second
const MaxSlug = 25
const MaxRelabels = 2
const MaxPaths = 10000
const ProbeTimeout = 5 * time.Second

const (
	DomainUnverified  = "unverified"
	DomainChecking    = "checking"
	DomainPendingDNS  = "pending-dns"
	DomainMisdirected = "misdirected"
	DomainIssuingCert = "issuing-cert"
	DomainFailed      = "failed"
	DomainLive        = "live"
	DomainUnknown     = "unknown"
	DomainRegional    = "regional"
)

const (
	HealthSettingUp     = "Setting up"
	HealthRunning       = "Running"
	HealthServing       = "Serving"
	HealthAsleep        = "Asleep"
	HealthFailed        = "Failed"
	HealthNotResponding = "Not responding"
	HealthNoReadings    = "No readings"
	HealthUnknown       = "Can't tell right now"
)

var ErrSlug, ErrEmptyMap error
var ErrHostTaken, ErrConflict, ErrNotFound, ErrUnauthorized, ErrForbidden, ErrBadRequest error
var ErrFellThrough, ErrNoBasePath, ErrNotRouted error

type Config struct {
	Console, Account, App, Token string
	HTTP                         *http.Client
	Timeout                      time.Duration
	Observe                      func(Exchange)
}
type Exchange struct {
	Method, Path string
	Status       int
	Header       http.Header
	Message      string
	Err          error
}
type Console struct{ /* unexported */ }
type CreateRequest struct {
	Host, Region, Message string
	Resume                bool
}
type Created struct {
	Host                            string
	Landed, Accepted, MayHaveLanded bool
	Operation, Phase                string
}
type Box struct{ Box, Region string }
type Instance struct {
	Host, App, Account, Region, Channel, CreatedBy string
	CreatedAt                                     time.Time
	ServedBy                                      []Box
	Gated                                         bool
}
type ListRequest struct {
	Query, Cursor string
	Limit         int
}
type Listed struct {
	Instance
	PublicHost, Health string
}
type ListPage struct {
	Instances []Listed
	Total     int
	Next      string
	Truncated bool
}
type DNSRecord struct {
	Type, Name, Value, Set string
	TTL                    int
	Required               bool
}
type Attached struct {
	Host     string
	Verified bool
	Verify   *DNSRecord
}
type Domain struct {
	Host, Route, State, Reason, DNS, CNAMETarget, Guidance string
	Verified                                               bool
	Records                                                []DNSRecord
	CheckedAt, CertExpiry                                  time.Time
}
type Published struct {
	Prefixes int
	Changed  bool
}
type Answer struct {
	URL                      string
	Status                   int
	Version, Build, Instance string
}
type RouteState int // RoutePending, Routed, FellThrough, NoBasePath
type Route struct {
	State               RouteState
	Prefix, Tenant, Hub Answer
}
type APIError struct {
	Status  int
	Message string
}
type RetryError struct {
	Status  int
	After   time.Duration
	Message string
	Err     error
}

func New(cfg Config) (*Console, error)
func NewLabel(slug string) (string, error)
func Ready(ctx context.Context, hc *http.Client, host string) (bool, Answer, error)
func WaitReady(ctx context.Context, hc *http.Client, host string, every time.Duration) (Answer, error)
func CheckRoute(ctx context.Context, hc *http.Client, product, slug, host string) (Route, error)
func WaitRouted(ctx context.Context, hc *http.Client, product, slug, host string,
	every time.Duration) (Route, error)

func (c *Console) Create(ctx context.Context, req CreateRequest) (Created, error)
func (c *Console) Instance(ctx context.Context, host string) (Instance, error)
func (c *Console) List(ctx context.Context, request ListRequest) (ListPage, error)
func (c *Console) AttachDomain(ctx context.Context, host, route string) (Attached, error)
func (c *Console) Domain(ctx context.Context, host string) (Domain, error)
func (c *Console) DetachDomain(ctx context.Context, host string) error
func (c *Console) PublishPaths(ctx context.Context, product string,
	build func(context.Context) (map[string]string, error)) (Published, error)
func (s RouteState) String() string
func (r Route) Err() error
func (e *APIError) Error() string
func (e *APIError) Is(target error) bool
func (e *RetryError) Error() string
func (e *RetryError) Unwrap() error
```
