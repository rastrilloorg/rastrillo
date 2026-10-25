# 🤖 Agents

An agent in a Rastrillo app always acts for a person. Paul makes a
token for Claude, Claude calls the app's [JSON API](/docs/api) with it,
and the app records each call as "Claude for Paul". An agent cannot act
for anyone else, and it cannot act as itself.

## Tokens are made for a named agent

A person makes a token on the app's token page and names the agent it
is for: "Claude", "Codex", "Nightly import". The name is 1 to 40
letters, digits, spaces, hyphens and dots, and it is shown as typed.

The name is a label. The token's public ID is what tells two tokens
apart, so two tokens both named "Claude" are still two tokens.

A token for a script with no AI in it is a deliberate choice: the person
ticks "This token is for me, not an agent". Calls with that token act as
the person, with no agent named.

See [Tokens](/docs/api#tokens) for the page itself.

## Actor says who is calling

Every operation gets a `rastrillo.Actor` on `Ctx.Actor`:

| Field | Holds |
|---|---|
| `Subject` | the person: the session's subject, or the token owner's |
| `Agent` | the agent's name, or empty when the person acts directly |
| `Credential` | the token's public ID, or empty for a browser |
| `Access` | `Read` or `Write`: what the token allows, and `Write` for a browser |
| `Session` | the agent's `Agent-Session` header, when it sent one |
| `Harness` | the agent's `Agent-Harness` header, when it sent one |

`Subject`, `Agent`, `Credential` and `Access` come from the verified
token, never from the request. `Session` and `Harness` are what the
agent says about itself. They are recorded as its claims, and only for a
token made for an agent.

The zero `Actor` is the app itself: a job, a sidecar, a migration.

## What the audit trail records

`Actor.String()` is the short form. `eventlog` stores it on every event
it appends:

| Who | `String()` |
|---|---|
| the app | `app` |
| a person in a browser | `user:42` |
| a person's own token | `user:42 token:k3Jx9QpL2mWa` |
| an agent | `agent:Claude for user:42 token:k3Jx9QpL2mWa` |

Events written before this release say `human` or `agent:<name>`. If
your code compares an actor string with `"human"`, change it.

`Actor.LogAttrs()` gives the same as `slog` attributes, plus
`agent_session` and `agent_harness` when they were sent. The API logs
them on every request.

## Showing it on a page

In a template, `{{actor $L .Actor}}` writes "Paul" for a person, "Claude
for Paul" for an agent, and nothing for the app. It is plain text: style
it yourself.

Set `ui.ActorName` once at startup to turn a subject into a name.
Without it, the page shows the subject. In Go, `rastrillo.ActorPhrase`
builds the same words. See [ui](/docs/reference/ui#actor).

## Coming later

Three later pieces build on the API: an MCP endpoint, sign-in for
agents with OAuth, and a generated skill that describes the app to an
agent.

## The sidecar

You can run scheduled or delivery-driven work outside a request:

```go
opts.Sidecar = func(ctx context.Context) (time.Time, error) {
	// one pass; return when the next pass is due
}
```

`Options.Sidecar` plus the `sidecar run` argv speak the platform's
sidecar contract, and `Options.NextDue` answers the activator's
`GET /api/next-due` scheduled-wake poll.

A pass returning a zero next-due time means no scheduled work, and the
loop sleeps for a minute, the family's polling floor.

A sidecar never dies on a pass error. The platform would only respawn it
into the same failure, so it backs off and retries, capped at five
minutes so a dead dependency cannot turn the loop into a busy-wait.
