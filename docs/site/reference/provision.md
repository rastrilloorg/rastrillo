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

Every refusal is an `*APIError` with the platform's status and sentence, and matches one of the errors above with `errors.Is`. The errors are for logs and records. Show people your own sentences.

`Instance` reads the platform's record of a host. A host that does not exist is `ErrNotFound`. An answer that does not name the host is a `*RetryError`: ask again later.

Set `Config.Observe` to log every console call. It never sees the token.

`provision` cannot delete an instance. A person deletes one with `carlos instances delete`.

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

## Credentials

Make one `Console` per credential, and use credentials bound to one app. An account-wide credential reaches every app in the account. A home needs an `instance` credential for the tenant app. A hub needs that one, and a `publish` or `instance` credential for the hub app to publish paths. Keep both in the central app; a tenant needs neither.

Set `Config.Account` to the account's sqid, not its name. The console answers 404 to a name.

If your hub publishes a leak profile, list two things in it: the hub-app credential can do much more than publish paths, and the `Rastrillo-Host` header shows any visitor the platform hostname behind each tenant's path.

## API

```go
const DefaultTimeout = 30 * time.Second
const MaxSlug = 25
const MaxRelabels = 2
const MaxPaths = 10000
const ProbeTimeout = 5 * time.Second

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
	Host             string
	Landed, Accepted bool
	Operation, Phase string
}
type Box struct{ Box, Region string }
type Instance struct {
	Host, App, Account, Region, Channel, CreatedBy string
	CreatedAt                                     time.Time
	ServedBy                                      []Box
	Gated                                         bool
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
func (c *Console) PublishPaths(ctx context.Context, product string,
	build func(context.Context) (map[string]string, error)) (Published, error)
func (s RouteState) String() string
func (r Route) Err() error
func (e *APIError) Error() string
func (e *APIError) Is(target error) bool
func (e *RetryError) Error() string
func (e *RetryError) Unwrap() error
```
