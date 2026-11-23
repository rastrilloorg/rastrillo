# 🤖 handofftest

`amadan.net/rastrillo/rastrillo/handoff/handofftest`

Use `handofftest` as a fake root app in your tests. This saves you from signing tokens by hand.

```go
func New(t testing.TB) *Root

type Root struct {
	Origin string
	Key    *crypto.Keypair
	KeyID  string
	Now    func() time.Time
}

func (r *Root) Setup() instance.Setup
func (r *Root) Assert(t testing.TB, enter *http.Response, subject string) url.Values
func (r *Root) Roster(t testing.TB, audience string, sequence int64,
	state roster.State, members ...roster.Member) string
func (r *Root) RosterOrigin(t testing.TB, audience string, sequence int64,
	state roster.State, origin string, members ...roster.Member) string
func (r *Root) GrantToken(t testing.TB, claims granttoken.Claims) string
func (r *Root) Call(t testing.TB, method, target string, body []byte) *http.Request
```

`New` makes a root at `https://root.test` with a fresh key, `k1`. Pass `Setup` to [`handoff`](/docs/reference/handoff) and [`roster`](/docs/reference/roster) to configure an instance for this root. Use it in place of `instance.FromEnv`.

`Assert` responds to the redirect from the instance's `Enter` handler as the root would for a signed-in subject. Pass `Assert` the response from `Enter`. Then POST its form to `handoff.EnterPath` with the nonce cookie set by `Enter`. `Roster` signs a snapshot for one instance, ready to POST to `roster.Path`. `RosterOrigin` signs one that also carries the account's custom origin, such as `https://blog.example.org`. The audience is still the instance's platform host.

`GrantToken` signs a [`granttoken`](/docs/reference/granttoken) as this root. It fills `Version`, `Issuer`, `KeyID` and `IssuedAt`, and sets `ExpiresAt` to an hour from now when you leave it zero. You choose the rest.

`Call` signs a [`rootcall`](/docs/reference/rootcall) request to `target`, an `https://` URL on the instance's platform host. It returns the request as the instance's server reads it. Pass it to the handler behind `Guard.Wrap`. Each request can be served once.

`Assert`, `Roster` and `Call` sign tokens with a 60-second lifetime. Set `Now` to the clock used by the instance under test, or its clock may reject those tokens.
