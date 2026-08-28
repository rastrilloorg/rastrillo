# 🤖 Compare

Rastrillo is a narrow bet: server-rendered Go on CARLOS, small enough
that an agent can hold the whole framework in context. Most Go projects
do not want that bet. This page says who does, and names the better
answer when it is not us.

Everything here is checked against each project's own repository, not
its marketing. Versions are from tags on **2026-08-28**.

## Routers, not frameworks

These give you routing and middleware. Data, auth, templates and jobs
stay your problem. That is the point of them. All four are MIT.

| | Latest | Direct deps | Handlers are |
|---|---|---|---|
| [chi](https://github.com/go-chi/chi) | v5.3.2 | **none** | `http.Handler` |
| [echo](https://echo.labstack.com) | v5.3.1 | 3 | `echo.Context` |
| [gin](https://gin-gonic.com) | v1.12.0 | 15 | `gin.Context` |
| [fiber](https://gofiber.io) | v3.5.0 | 14 | `fiber.Ctx` |

Rastrillo's middleware is `func(http.Handler) http.Handler` and its
handlers take `(w, r)`, so chi composes with it and the other three do
not without adapters. Scaffolded apps use chi for route groups; the
framework itself imports no router at all.

Fiber is built on `fasthttp` rather than `net/http`. That buys speed
and costs the entire stdlib middleware ecosystem.

## Full-stack frameworks

| | Latest | Licence | Frontend | Data | Deploys to |
|---|---|---|---|---|---|
| [Buffalo](https://gobuffalo.io) | v1.1.4 | MIT | Plush templates | Pop | anywhere |
| [Encore](https://encore.dev) | v1.58.4 | MPL-2.0 | none — API-first | own SQL primitives | Encore Cloud, your AWS/GCP, Docker |
| [Goa](https://goa.design) | v3.30.0 | MIT | none — generates clients | none | anywhere |
| [Velocity](https://vel.build) | v0.76.2 | MIT | Inertia + React/Vue + Vite | own generic ORM | anywhere |
| Rastrillo | v0.19.0 | MPL-2.0 | `html/template` + `ui` | GORM + SQLite | CARLOS |

Encore needs its own CLI and compiler; everything else here builds with
`go build`. Velocity is pre-1.0 and moving fast — it has retracted a
`v1.0.0` published by mistake.

## Use something else if

**gin, echo, fiber, chi** — you are building a JSON API and want to
choose your own database, auth and deployment. A router plus four
libraries you picked is a legitimate architecture and the most common
one in Go.

**Encore** — you want infrastructure to follow from the code, across
Postgres, Pub/Sub and cron, provisioned into your own cloud account.
Nothing in Rastrillo does this.

**Goa** — the API contract is the artifact. You want OpenAPI, gRPC and
typed clients generated from one design, with no drift.

**Velocity** — you want Laravel: 30-plus services under one API, and a
React or Vue frontend over Inertia. It is far younger than the others
here, but it is the closest thing to Rails-for-Go with a modern
frontend attached.

**Buffalo** — the same ambition, a decade of history, and a stable
release. Still maintained, moving slowly.

## Do not use Rastrillo if

- **You are not deploying to CARLOS.** `main.go` calls `Resolve`, not
  `Run`, and expects platform activation argv. See
  [Deploying](/docs/deploying).
- **You want a React or Vue frontend.** There is no Inertia adapter and
  no asset pipeline beyond [Assets](/docs/assets). Velocity is the
  answer to that question.
- **You need Postgres or MySQL.** SQLite is not a default here, it is
  the assumption — see [Data](/docs/data).
- **You are writing a pure JSON API.** Sessions, CSRF, flash and
  owner-scoped 404s are most of what you would be paying for.
- **No agent is involved.** The [SKILL.md](/docs/agents) budget shapes
  a lot of the design. If a human writes every line, that constraint
  buys you nothing and costs you features.

## The axis nobody else is on

The other frameworks here optimise for what a developer can build
quickly. Rastrillo optimises for what an agent can hold at once: one
15 KB `SKILL.md`, budgeted and enforced by a test, covering the whole
framework. Nothing else on this page ships a single-file spec an agent
loads before it starts.

Whether that matters depends entirely on who is writing the code.
