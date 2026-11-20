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

To test a list, `ScriptList` queues answers for the next lists. `Sleep` makes an instance's health `Asleep`, `Fail` makes it `Failed` and its edge answer 502, and `MakeReady` ends the pending answers of an instance that exists. `Unready` only affects instances created after it. `CatchAll(account, app, parent)` makes `<label>.<parent>` serve the app's instance whose host starts with that label, and tells the app the instance's own host. A real route or an exact claim on the same name wins over it.

The list, like the platform's, reads at most 500 instances before it filters and pages, and answers `truncated` when there are more. Known differences: `Sleep` never clears when a request reaches the instance, a path target ignores `Fail`, the cursor is the host itself where the platform's is a JSON object, and the role gate is the platform's at commit 5b4c2f64, so `publish` is refused where current main admits it.

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
func (p *Platform) MakeReady(host string)
func (p *Platform) Sleep(host string)
func (p *Platform) Fail(host string)
func (p *Platform) CatchAll(account, app, parent string)
func (p *Platform) ScriptList(replies ...Reply)
func (p *Platform) Script(replies ...Reply)
func (p *Platform) ScriptPaths(replies ...Reply)
func (p *Platform) Instances(account, app string) []string
func (p *Platform) Paths(host string) map[string]string
func (p *Platform) Calls() []string
```
