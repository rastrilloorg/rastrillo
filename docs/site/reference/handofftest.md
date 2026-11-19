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
```

`New` makes a root at `https://root.test` with a fresh key, `k1`. Pass `Setup` to [`handoff`](/docs/reference/handoff) and [`roster`](/docs/reference/roster) to configure an instance for this root. Use it in place of `instance.FromEnv`.

`Assert` responds to the redirect from the instance's `Enter` handler as the root would for a signed-in subject. Pass `Assert` the response from `Enter`. Then POST its form to `handoff.EnterPath` with the nonce cookie set by `Enter`. `Roster` signs a snapshot for one instance, ready to POST to `roster.Path`.

`Assert` and `Roster` sign tokens with a 60-second lifetime. Set `Now` to the clock used by the instance under test, or its clock may reject those tokens.
