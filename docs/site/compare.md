# 🤖 Compare

Rastrillo is designed specifically for CARLOS: built with AI in mind
(but AI not required), to be deployed with high-resilience and low cost.

Everything validated against each project's repo **2026-08-28**.

## Routers

Simple frameworks for routing and middleware. All four MIT licensed.

| | Latest | Direct deps | Handlers are |
|---|---|---|---|
| [chi](https://github.com/go-chi/chi) | v5.3.2 | **none** | `http.Handler` |
| [echo](https://echo.labstack.com) | v5.3.1 | 3 | `echo.Context` |
| [gin](https://gin-gonic.com) | v1.12.0 | 15 | `gin.Context` |
| [fiber](https://gofiber.io) | v3.5.0 | 14 | `fiber.Ctx` |

Rastrillo's middleware is `func(http.Handler) http.Handler` and its
handlers take `(w, r)`, so chi drops straight in. The other three need
adapters. Scaffolded apps use chi for its route groups, though the
framework itself imports no router at all.

Fiber is built on `fasthttp` instead of `net/http`. That buys speed.
stdlib middleware no longer usable.

## Full-stack frameworks

| | Latest | Licence | Frontend | Data | Deploys to |
|---|---|---|---|---|---|
| [Buffalo](https://gobuffalo.io) | v1.1.4 | MIT | Plush templates | Pop | anywhere |
| [Encore](https://encore.dev) | v1.58.4 | MPL-2.0 | none — API-first | own SQL primitives | Encore Cloud, your AWS/GCP, Docker |
| [Goa](https://goa.design) | v3.30.0 | MIT | none — generates clients | none | anywhere |
| [Velocity](https://vel.build) | v0.76.2 | MIT | Inertia + React/Vue + Vite | own generic ORM | anywhere |
| Rastrillo | v0.19.0 | MPL-2.0 | `html/template` + `ui` | GORM + SQLite | CARLOS |

Encore needs its own CLI and compiler. Everything else here builds with
`go build`. Velocity is pre-1.0 and moving fast enough that it has
already retracted a `v1.0.0` published by mistake.

## Where the others win

Reach for a router — chi, echo, gin, fiber — when you are building a
JSON API and want to pick your own database, auth and deployment. A
router plus four libraries you chose is a legitimate architecture, and
the most common one in Go.

Reach for Encore when you want infrastructure to follow from the code,
across Postgres, Pub/Sub and cron, provisioned into your own cloud
account. Rastrillo is a different direction.

Goa suits a project where the API contract is the artifact: define it
once, and OpenAPI, gRPC and typed clients all generate from that design.

Velocity is the closest thing here to a full-stack framework with a
modern frontend attached — thirty-odd services under one API, React or
Vue over Inertia. It is much younger than the rest. Buffalo has the same
ambition and a decade of history behind it, still maintained, moving
slowly.

## Where Rastrillo is the wrong answer

You need CARLOS — see [Deploying](/docs/deploying). CARLOS prefers
server-rendered apps with progressive enhancement. If you want a React
or Vue frontend there is no Inertia adapter and no asset pipeline.
[Assets](/docs/assets) keeps things very simple: no bundling, just
fingerprinting.

Velocity has more JS tooling. Rastrillo assumes SQLite, so Postgres and
MySQL have to be own-rolled — [Data](/docs/data) has the detail. And for
a pure JSON API, sessions, CSRF, flash and owner-scoped 404s are most of
what you would be paying for.

The last one is the real filter. Since Rastrillo is built by AI for AI,
the [SKILL.md](/docs/agents) budget that shapes so much of the design is
irrelevant to you.

## What the budget buys

The other frameworks here optimise for what a developer can build
quickly. Rastrillo optimises for what an agent can hold at once: one 15
KB `SKILL.md`, budgeted and enforced by a test, covering the whole
framework. Rastrillo aims for token efficiency and thus time and cost
efficiency.

Nothing else on this page ships a single-file spec an agent loads before
it starts. In an AI-first world, Rastrillo is an AI-built solution.

You can still write every line yourself; the budget just stops being the
reason to choose it.

Whether that matters depends entirely on who is writing the code.
