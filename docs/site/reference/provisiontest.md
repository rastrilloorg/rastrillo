# 🤖 provisiontest

`amadan.net/rastrillo/rastrillo/provision/provisiontest`

Use `provisiontest` to test code that uses `provision`, with no network. `New` returns a fake console and edge that give the platform's own answers.

Pass `p.Client()` as `Config.HTTP` and to the probes, and `p.Console()` as `Config.Console`.

Set it up as a person sets up the platform. `Enable` turns instances on for an app, `Credential` makes an app-bound credential with a role, `Attach` gives the hub app its product hostname, and `Serve` sets what the app's instances run.

`Tenant(slug)` and `Hub()` are handlers that answer `/api/version` and name themselves, as `NameInstance` does. With `slug` set, a tenant also answers under `/<slug>`. With `""`, it answers 404 there, which `CheckRoute` reads as `NoBasePath`.

Use instance domains under `AppsDomain`, such as `bdf.oncarlos.test`. An unknown host there gets the setting-up page. Any other unknown host gets a 404.

`Release` sets the version header on an app's answers. Give the hub and the tenant different versions to test `FellThrough` and `NoBasePath`.

To test what can go wrong, `Script` queues answers for the next creates, and `ScriptPaths` for the next publishes. A `Reply` with `Record` set writes first, then answers, as a lost answer after the write does. One with `Hang` waits until the request's context ends. Every other `Reply` needs a `Status`, or `Script` panics.

`Unready(n)` makes each new instance answer its first n requests with the setting-up page. `OtherBox` moves an instance to another box, so a path to it falls through to the hub.

`Instances`, `Paths` and `Calls` show what your code did. `Calls` leaves out query strings.

## API

```go
const AppsDomain = "oncarlos.test"

type Platform struct{ /* unexported */ }
type Reply struct {
	Status     int
	RetryAfter string
	Hang       bool
	Record     bool
}

func New() *Platform
func Tenant(slug string) func(host string) http.Handler
func Hub() func(host string) http.Handler

func (p *Platform) RoundTrip(req *http.Request) (*http.Response, error)
func (p *Platform) Client() *http.Client
func (p *Platform) Console() string
func (p *Platform) Enable(account, app, domain string)
func (p *Platform) Credential(account, app, role string) string
func (p *Platform) Release(account, app, version string)
func (p *Platform) Serve(account, app string, h func(host string) http.Handler)
func (p *Platform) Attach(account, app, host string)
func (p *Platform) OtherBox(host string)
func (p *Platform) Unready(n int)
func (p *Platform) Script(replies ...Reply)
func (p *Platform) ScriptPaths(replies ...Reply)
func (p *Platform) Instances(account, app string) []string
func (p *Platform) Paths(host string) map[string]string
func (p *Platform) Calls() []string
```
