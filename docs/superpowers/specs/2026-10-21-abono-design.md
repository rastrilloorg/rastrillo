# Abono: payments and subscriptions as a rastrillo addon

Written 2026-10-21. Status: design, for Paul's approval. Nothing is built.

## 0. What this is, and why now

Abono is a rastrillo addon (`amadan.net/rastrillo/abono`, its own repository,
like idear and aviso) that lets an app take money through Stripe: recurring
memberships, lifetime access, one-off purchases, and donations or tips.

Five Go apps already take money, and every one hand-rolled its own Stripe
client and the same webhook signature check: titogo, comercio, eleven,
carloku's billing branch and slopbox. Patrix is about to be the sixth.
Farshad built paid memberships into Patrix on 2026-10-21
(`internal/patrix/billing`, branch `claude/dazzling-chandrasekhar-6f277c` on
amadan.net/patrix/patrix, not merged) so asymco.com can leave WordPress and
Paid Memberships Pro (PMPro). Their spec's own trigger for a shared version
is "a second app takes payments". That is already true several times over.

**The first consumer is Patrix**, a WordPress / Ghost / Substack-style
publishing app, and its first site is asymco.com: 83 active memberships, 69
of them on live Stripe subscriptions that must keep renewing with no card
re-entered.

**Success** is:

1. Patrix's `internal/patrix/billing` is replaced by abono with no visible
   change for asymco.com's readers or owner, and the 69 subscriptions keep
   renewing.
2. A second Patrix site, hosted by patrix.cc, can connect its own Stripe
   account through Connect and pay patrix.cc a cut the developer set.
3. comercio and eleven *could* switch: abono covers what they do today, with
   a written path for their data. Switching is their choice, not part of
   this project.

## 1. Decisions

Paul's, 2026-10-21:

- **An addon.** Not app code in Patrix, and not a CARLOS/carloku platform
  feature. (This answers decision 1 of Farshad's spec.)
- **Stripe only, through a hand-rolled client** with the API version pinned,
  as every existing app does. No stripe-go. (Farshad's decision 2.)
- **Two ways to hold a Stripe account, both supported:**
  - **Own key:** the site's own (restricted) Stripe key in its environment.
    No platform cut, no relay. This is how Farshad built it, and how
    asymco.com moves first.
  - **Connect:** the writer connects their own Standard account to the
    app developer's platform, and **the developer decides the cut**, from
    nothing (Ghost) to a share (Substack).
- **Connect goes through a relay or the app's main app; the developer
  chooses.** The relay is the CARLOS idiom (the platform page lists "the
  OAuth relay" among the blind shared services). It is not a carloku
  feature to request.
- **Four ways to charge:** recurring memberships (monthly, yearly), lifetime
  access, one-off purchases, and donations or tips.
- **UI is three layers:** Go answers that work alone (headless); starter
  pages generated into the app, which then owns them; and a few partials for
  the fiddly pieces, which keep receiving fixes.
- **Abono never decides access.** It reports what someone has paid for
  (which plan, in what state). The app maps that to roles or reading rights.
  In Patrix that stays Patrix's call (Farshad's decision 3).
- **Abono never makes people.** When a payment needs a holder and none is
  known, it asks the app through a callback. (Farshad's decision 6, the
  alternative side.)
- **comercio and eleven may switch; nothing forces them.**

Mine, open to override:

- **A core hook first: CSRF exemption for machine POSTs.** `csrf.Protect`
  refuses any POST without browser evidence, which is every Stripe webhook
  and every `carlos schedule` tick. Patrix works round it by hand
  (`exceptMachines` in `app.go`). Rastrillo gets a way to name paths that
  machines reach and that check their own credential, on its own branch,
  before abono, following "ask core for a hook first" in
  `docs/site/writing-addons.md`. The exact API is the builder's (an option
  to `Protect`, or `csrf.Exempt(paths...)`), with a reference page and the
  platform docs' "Scheduled work" example corrected to show it.
  (Farshad's decision 5 and request 1.)
- **Order of work:** Farshad's branch is merged into Patrix once Paul has
  ruled on its six decisions; abono is extracted from it; Patrix switches.
  Extraction starts from working code, and asymco.com's move is not held up
  by the addon.
- **The name "abono"** (approved by Paul): a subscription or season pass,
  and a payment.

## 2. Goals, non-goals, anti-goals

**Goals**

- Every Stripe app on rastrillo stops hand-rolling the client, the webhook
  check, idempotency and reconcile.
- Adopting existing subscriptions is first class: an app moving off
  WordPress, Ghost or anything else on Stripe brings its paying members
  across without them noticing.
- An app with nothing connected still works end to end in development.

**Non-goals (v1, named so nobody builds them by accident)**

- Tax calculation. The writer is the seller; Stripe Tax is a switch on their
  account. Abono passes `automatic_tax` through when the app turns it on, and
  does nothing else.
- Stripe's embedded card form (Elements). Hosted Checkout and the hosted
  Customer Portal only.
- PayPal or any other provider. The internal seam allows one later; no
  provider interface is exported in v1.
- Terminals, bank transfers, invoices and credit notes (titogo's).
- Metered usage and overage (slopbox, carloku) and per-seat quantities
  (eleven's hosted spaces, carloku).
- Changing a subscription's plan or cancelling it from the app. Readers do
  that in the Customer Portal, owners in Stripe's dashboard (as Farshad
  built it).
- Trials. Checkout supports them; nobody needs them yet.

**Anti-goals: things that would make it a failure even if it works**

- Card data ever touching the app. Checkout and the Portal are Stripe's.
- Two sources of truth. Stripe is the truth; abono's tables are a copy that
  every path re-derives from Stripe.
- Two systems acting on one renewal during a move (PMPro and Patrix both
  live on the same subscriptions with both able to cancel).
- A reader who paid and can't read, or a reader whose payment failed for
  good and still can after Stripe's retries end.
- A platform key, OAuth token or connected-account choice that an instance
  can forge or widen. In Connect mode the instance never holds the platform
  key.
- Abono deciding who may read what, or creating accounts on its own.

## 3. What an app sees

### 3.1 Offers and plans

An **offer** is something a reader can buy. Each has a kind:

| Kind | Checkout mode | What it grants |
|---|---|---|
| `recurring` | `subscription` | a plan, while Stripe says the subscription is good |
| `lifetime` | `payment` | a plan, for good, unless refunded or a dispute is lost |
| `purchase` | `payment` | nothing by itself; the app is told it was paid |
| `donation` | `payment` | nothing; recorded, and the app is told |

A **plan** is a name the app uses for access (`annual`, `monthly`, `member`).
A recurring plan may have several prices: the one sold now, one per interval
(monthly and yearly of the same plan), and **legacy prices** whose
subscriptions count as the same plan. PMPro's prices are legacy prices of
asymco.com's plans. This generalises Farshad's
`PATRIX_STRIPE_PLANS=annual=price_A|price_old`.

Offers live in abono's tables, not only in the environment, because the
writer manages them from the app on a hosted site. An offer is made either
by **naming existing Stripe prices** (adoption; asymco.com) or by **abono
creating the product and price** on the account (a new site). The builder
decides whether an environment variable can also seed offers at boot, which
would keep Patrix's current configuration working unchanged.

Purchases carry their own line items: the app passes a name, unit amount,
currency and quantity per line (inline `price_data`), plus an opaque
**reference** (comercio's order ref). Donations take an amount the reader
chose, checked against the offer's minimum, maximum and currencies (eleven:
eur, usd, gbp; minimum 110 minor units), or one of the offer's suggested
amounts.

Money is integer minor units with rastrillo's `money` package, in any
currency Stripe takes.

### 3.2 Holders

Abono keys everything to a **subject**: the app's own id for a person, a
string, the way aviso does (`sessions` subject). It never reads the app's
user table. A signed-out buyer is known only by the address they paid with
until the app names a subject for them:

- `Holder(ctx, tx, email, name) (subject string, err error)` is the app's
  callback. Patrix's makes a spectator from the address (its decision 4,
  "paying is a way in", stays Patrix's). An app that wants sign-in before
  payment returns "", and the payment is recorded unclaimed by address.
- An unclaimed payment is attached when the app later calls
  `Claim(subject, email)` (the builder may fold this into sign-in).

### 3.3 Holdings: the one answer access needs

`Holdings(ctx, subject) []Holding` is what this person has, newest first.
A holding is `{Plan, Source, State, Until, CancelAtPeriodEnd}`.

| Source | State | Grants? |
|---|---|---|
| stripe subscription | `active`, `trialing` | yes |
| stripe subscription | `past_due` | yes, while Stripe retries, but never more than 30 days past the paid period's end |
| stripe subscription | `canceled`, `unpaid`, `incomplete`, `incomplete_expired`, `paused` | no |
| lifetime payment | `paid` | yes, with no end |
| lifetime payment | `refunded`, `disputed` | no, at once |
| grant (a comp) | `active` before `ends_at`, or with none | yes |
| grant | `canceled`, or past `ends_at` | no |

`Grants(ctx, subject, plan) bool` is the shortcut. The table is Farshad's,
plus lifetime from eleven. Grace is Stripe's own retry schedule, with one
backstop (my call): Stripe lets an account leave a subscription `past_due`
for good after its retries end, so `past_due` stops granting 30 days after
the paid period's end (eleven's grace) whatever the account's setting. The
owner's screen flags subscriptions held by that backstop.

A **cancelling** subscription (`cancel_at_period_end`) still grants until
its period ends, and the status partial says so.

### 3.4 Being told

Every write to a holding or payment runs in one transaction with the app's
hooks, so the app's own state never disagrees with abono's:

- `OnChange(ctx, tx, subject)`: a holding of this subject changed. Patrix
  runs its `settleRole` here (entitled spectator becomes subscriber, and
  back; staff never move).
- `OnPaid(ctx, tx, Payment)` and `OnReversed(ctx, tx, Payment)`: a
  purchase, donation or lifetime payment settled, or was fully refunded or
  lost a dispute. comercio fulfils or cancels its order by
  `Payment.Reference` here. `OnPaymentChanged(ctx, tx, Payment)` reports
  everything in between (section 4.2a).

A hook's error rolls the write back, and the webhook answers 5xx so Stripe
retries.

### 3.5 Actions

- `Checkout(ctx, CheckoutRequest) (url, error)`: an offer, an optional
  subject, a promotion code (looked up as Farshad does; without one,
  Checkout shows its own code field), and for purchases the lines and
  reference. Returns Stripe's hosted page.
- `Finish(ctx, sessionID)`: Checkout's return. Syncs at once so the next
  page already reflects the payment, verifying what comercio verifies:
  the session belongs to this account, its amount and currency match what
  was minted, and it is paid. It signs nobody in.
- `Portal(ctx, subject, returnURL)`: the Customer Portal.
- `Grant(...)` and `EndGrant(...)`: comps, as Farshad built them.
- `Refund(ctx, paymentID, amount)`: for purchases and donations (comercio's
  admin refunds), full or partial, with an idempotency key. Never offered
  for subscriptions in v1.
- `Adopt(ctx, subscriptionID, subject, plan)`: take an existing Stripe
  subscription as this subject's holding. The PMPro import in Patrix calls
  this; it stays Patrix's code, since PMPro is a WordPress thing.
- `Reconcile(ctx)`: see 4.3.

## 4. How it stays right

### 4.1 Webhooks

One handler, `POST <prefix>/webhook`, mounted by the app (behind the core
CSRF hook):

- Signature checked: HMAC-SHA256 over `t.payload`, any matching `v1`, five
  minutes' tolerance, several secrets while rotating. One function, the
  same as all five apps'.
- **Events carry ids only.** The handler reads which object the event is
  about and fetches it again in the pinned API version (Farshad). That
  makes event order and the endpoint's API version irrelevant, so eleven's
  out-of-order watermark is not needed.
- Events already handled are skipped by id (`abono_events`), recorded after
  the work so a failure is retried in full (Farshad, carloku).
- A new holding is only created by a checkout completion, a
  `customer.subscription.created`, an adoption or a sync that has a hint
  (eleven's "no resurrection" rule; Farshad's "an ended subscription first
  seen now is not recorded").
- **Which subscriptions are this site's** (my call): only those on a price
  that belongs to one of its offers, sold or legacy. An account may sell
  other things, and those must never grant membership. Farshad's fallback,
  naming an unknown price's plan by its interval, is dropped. A granting
  subscription on an unknown price is listed on the owner's members screen
  as not recognised, with an action to adopt it onto a plan.
- Responses: bad signature 400; failure worth retrying (Stripe unreachable,
  database, a hook's error) 500; anything abono doesn't read 200.
- Events read: `checkout.session.completed`,
  `checkout.session.async_payment_succeeded`, `checkout.session.expired`,
  `customer.subscription.*`, `invoice.paid`, `invoice.payment_failed`,
  `charge.refunded`, `charge.dispute.created`, `charge.dispute.closed`.
- A dispute on a lifetime payment ends the holding at once; a lost dispute
  keeps it ended; a won one restores it. A refunded lifetime payment is
  tombstoned so no later event can re-grant it (eleven's
  `revoked_payments`). Subscription disputes run through the subscription's
  own lifecycle, as in eleven.

### 4.2 Pending one-off payments

A purchase or donation is recorded `pending` when its session is minted,
bound to the account, the currency, and the lines with their **subtotal**
(comercio's `checkout_session_bindings`). Return and webhook both check the
binding against the session's `amount_subtotal`, not its total, because a
promotion code or automatic tax entered at Checkout legitimately changes
the total (my call). The payment records what Stripe actually charged:
subtotal, discount, tax and total, and the hooks report those.

### 4.2a Refunds and disputes on one-off payments

My call, for every one-off kind:

- A **full refund** or a **lost dispute** reverses the payment:
  `OnReversed`. A reversed lifetime payment ends its holding and is
  tombstoned.
- A **partial refund** is recorded and reported through `OnPaymentChanged`;
  it does not end a lifetime holding.
- A **dispute opened** suspends a lifetime holding at once (eleven) and is
  reported through `OnPaymentChanged`; a **won** dispute restores the
  holding and is reported the same way. A purchase's app decides what a
  dispute means for its order.

### 4.3 Reconcile

`Reconcile` is the daily `carlos schedule` tick (guarded by `carlos.Tick`,
idempotent by `TickOccurrence`), plus an owner's "Sync with Stripe now":

- Lists every subscription on the account and syncs each (Farshad).
- Re-checks pending one-off payments older than a day: paid and matching
  fulfils; expired cancels; anything else stays pending (comercio's sweep).
- Ends grants past `ends_at`, and calls `OnChange` for every subject whose
  answer moved.

## 5. Accounts: own key and Connect

### 5.1 Own key

Configuration is all or nothing (Farshad): a restricted key, the webhook
secret(s), and the offers. Half a setup refuses to boot. With none, nothing
is for sale and the starter pages say so. The restricted key's scopes are
written in SKILL.md (Farshad's list, plus Refunds write and Products/Prices
write when abono creates offers).

The key sees every subscription on the account, whoever created it, which
is why asymco.com moves this way: PMPro's subscriptions were made through
PMPro's own Connect platform, and the site's own key reads them regardless.

### 5.2 Connect

The developer runs a **broker**: the one place holding the platform's
Stripe secret key, the OAuth client id, and the Connect webhook secret. An
instance never holds them. Abono ships the broker as a Go package that can
be hosted two ways, the developer's choice:

- **Relay:** a small standalone service for all of the developer's
  instances (`abono/cmd/relay`), the CARLOS idiom. Extracted from titogo's
  `internal/relay`, which grew out of eleven's connect broker.
- **Main app:** mounted inside the developer's own central app (patrix.cc's
  dashboard, comercio's main app), the comercio pattern.

Both speak one protocol to instances:

1. **Connect.** The instance asks the broker for a connect URL. The broker
   mints a single-use, hashed, ten-minute state bound to the instance and
   the mode (live or test), shows its own confirm page, sends the writer
   to Stripe, and on return does the code exchange itself, **keeps no
   token**, records `acct_ → instance`, and delivers only the account id and
   livemode to the instance over a signed hop. A delivery that fails rolls
   the mapping back.
2. **Calls.** The instance's Stripe client sends its calls to the broker's
   egress proxy instead of api.stripe.com. The broker adds the platform key
   for the connection's mode and a **pinned** `Stripe-Account` from its own
   mapping; inbound `Authorization` and `Stripe-Account` are dropped. The
   instance cannot reach an account it wasn't given.
3. **The cut.** The broker injects the developer's fee:
   `subscription_data[application_fee_percent]` on subscription Checkout
   sessions, and `payment_intent_data[application_fee_amount]` on payment
   sessions. The instance can't lower it. The developer configures it on
   the broker, per kind of offer. Its economics are fixed (my call) so
   that Checkout-time injection is all the work there is:
   - It is a **percentage of what the reader pays**, after discounts and
     including tax, which is Stripe's own meaning of
     `application_fee_percent` on every renewal invoice. One-off fees are
     computed the same way on the subtotal the binding fixes; a minimum
     applies to one-off payments only.
   - **A refund returns the fee in proportion** (`refund_application_fee`),
     so a refunded reader never leaves the platform paid for nothing. A
     developer can turn that off.
4. **Webhooks.** One Connect webhook endpoint on the broker verifies
   Stripe's signature, routes by the event's `account` to the instance,
   and forwards it with Stripe's signature replaced by a signed hop. An
   unmapped account is 200 and logged; a known instance that can't be
   reached is 503 so Stripe retries.
5. **Disconnect.** The instance asks the broker, which deauthorizes with
   Stripe before deleting the mapping, and only for grants it made itself.
   `account.application.deauthorized` (which none of the existing apps
   handle) is handled: the instance is told and stops selling.
   **Existing holdings after a disconnect** (my call): the subscriptions
   carry on billing on the writer's own account, so readers keep paying
   the writer. Abono can no longer see them, so each holding keeps its last
   known state until its recorded period end, then stops granting. The
   owner sees a warning naming that date. Reconnecting the same account
   runs a reconcile and restores everything still paid; a different account
   cannot take the old holdings over.

Live and test are twins (comercio): separate client ids, chosen by the
connection's livemode, never falling through to each other, and an
exchange whose livemode doesn't match the request is refused in both
directions.

**Signed hops use rastrillo's `assertion` package** (comercio's handoff
uses it) rather than titogo's own ed25519 envelope, so the broker and
instance agree on one rastrillo primitive. If `assertion` lacks something
the hop needs, that is a core request, made first.

Accounts are Standard accounts through OAuth in v1, as titogo and eleven
use. Stripe says OAuth "isn't recommended for new Connect platforms" and
steers new ones to Accounts v2 with hosted onboarding; an account already
controlled by another platform can't connect over OAuth. Moving to
Accounts v2 is a later slice. The builder confirms against Stripe's current
docs before building that OAuth for Standard accounts still works for a new
platform.

### 5.3 Adopting subscriptions under Connect

Under Connect, a site that already has subscriptions on its account
(created by the account itself, or by another platform such as PMPro's)
needs two facts the existing apps never checked. The builder verifies both
against Stripe's docs and a test account before building adoption for
Connect:

- whether the developer's platform, connected over OAuth with `read_write`,
  can read and manage subscriptions another platform created; and
- whether it can set `application_fee_percent` on a subscription it didn't
  create.

If the second is no, adopted subscriptions pay no cut until a reader
re-subscribes, and the owner's screen says so. If the first is no, adoption
under Connect is not offered and such sites use own-key mode. Neither
affects asymco.com, which is on its own key.

PMPro's own Connect fee on asymco.com's subscriptions (Farshad's risk)
stays PMPro's to remove; abono changes nothing about it.

## 6. Schema

Abono's own migrations, namespaced `abono`, merged into the app's
`BootSchema` (the addon rule). Table names prefixed `abono_`. Roughly:

- `abono_offers` and `abono_prices`: kind, plan, prices with interval,
  sold/legacy, suggested amounts and limits for donations.
- `abono_account`: mode (own or connect), account id, livemode, connected
  and disconnected times. One row: one instance is one site.
- `abono_customers`: subject to Stripe customer.
- `abono_subscriptions`: Stripe subscription id, customer, subject, plan,
  status, period end, cancel at period end.
- `abono_payments`: one-off payments of every kind: session, payment
  intent, kind, offer, amount, currency, reference, subject or unclaimed
  address, status (pending, paid, refunded, partially refunded, disputed,
  canceled), and the session binding (4.2).
- `abono_grants`: comps.
- `abono_events`: event ids handled.
- `abono_tombstones`: refunded or lost-dispute lifetime payments.

Frozen migration checksums as idear's `schema_test.go`. The builder may
reshape these; the anti-goals and the holding table in 3.3 are what must
hold.

## 7. UI

### 7.1 Partials

Shipped through `abono.Templates()`, on core's tokens and classes, so they
follow the app's theme, light and dark, and language:

- **status:** the reader's membership in one line: active, renews on a
  date; cancelling, ends on a date; payment failing, Stripe is retrying;
  lifetime; given by the owner. This is the one apps get wrong.
- **plan card** and **plan picker** with a monthly/yearly switch.
- **paywall block**, for where locked content would be.
- **subscribe button** and **manage billing button** (each a POST form,
  since both mint a Stripe session).
- **amount picker** for donations.

A partial that needs something core lacks (a price-with-interval format,
say) gets it in core's `ui/partials` first.

### 7.2 Starter pages

A generator writes readable pages into the app, committed and owned by it
from then on, the way `rastrillo generate` does for manifests. Running it
again never overwrites a page the app has. The builder chooses between a
`rastrillo generate` extension and abono's own command.

- **Reader:** pricing (all offers), one plan's page, Checkout's return, the
  membership section of an account page, a donation page, and the paywall
  wiring for a post.
- **Owner:** Stripe status (own key) or Connect and disconnect (Connect),
  offers and prices, members (every holding, sync now, give a comp, end a
  comp on its own confirm page), payments (one-offs, with refund).

Patrix keeps its existing pages and switches to the Go API (headless).

All starter-page and partial copy goes through the copy-review gate before
it is written into the repository, with no em dashes. Strings ship as
locale keys the app's language packs carry, as core's do; translations of
approved copy are not reviewed.

## 8. Development and testing

- **Sandbox.** With nothing configured in development, abono can run a
  local stand-in checkout that completes or fails on a click, so the whole
  flow is clickable without Stripe (comercio's demo rail, titogo's
  simulated payments). It is refused in production. The builder chooses how
  "development" is known.
- **`abonotest`:** a fake Stripe (and fake broker) that apps' tests use end
  to end. Farshad's six end-to-end tests move into abono's suite against it:
  checkout to holder to cancel, refused signatures, no Stripe, staff
  untouched (as an `OnChange` test), import and reconcile, comps.
- Unit tests for signatures, offers and price parsing, the holding table in
  3.3, both API versions' subscription shapes, the binding checks, and the
  broker's state, proxy pinning and fee injection.
- A browser drive of the starter pages in the example app.
- The addon repository's usual pieces (`docs/site/writing-addons.md`): its
  own CI on the amadan fleet, `SKILL.md` with its byte budget, `example/`
  as a working app, `LICENSE`, and an entry in rastrillo's
  `docs/site/addons.md` when released.

## 9. Slices

My call on the order; each ends releasable.

1. **Core hook:** the CSRF exemption for machine POSTs, in rastrillo.
2. **Abono v0.1, own key, memberships:** client, webhook, reconcile,
   offers by naming prices, holdings, grants, adoption, portal, `OnChange`,
   `Holder`, partials, starter pages, sandbox, `abonotest`, example app.
   **Patrix switches** to it: a Patrix migration copies `memberships` and
   `billing_events` into abono's tables, and Patrix's PMPro import calls
   `Adopt` and `Grant`. Behaviour for asymco.com is unchanged.
3. **One-off kinds:** lifetime, purchase, donation, refunds, tombstones,
   disputes, `OnPaid`/`OnReversed`/`OnPaymentChanged`, pending sweep; offers created by abono.
4. **Connect:** the broker package, relay command and main-app mounting,
   fees, disconnect and deauthorization, after the 5.3 checks.
5. **Written paths for comercio and eleven** to switch, listing what maps to
   what. Not their switching.

## 10. What is left to the builder

- Package layout and exported names, within the shapes in sections 3 to 5.
- Exact table shapes.
- The CSRF hook's API.
- Whether offers can be seeded from the environment.
- How the generator is invoked.
- How the sandbox knows it is in development.
- The broker's fee configuration format.

## Appendix: where each idea comes from

- Patrix (`internal/patrix/billing`, Farshad): the holding table, id-only
  webhooks with re-fetch, event-id idempotency, reconcile, comps, plans
  with legacy prices, `settleRole` as the app's hook, the PMPro import,
  restricted keys, all-or-none configuration.
- eleven (`subscriptions.go`, `donate.go`, `broker/`): lifetime purchases
  and the refund tombstone, dispute handling, "no resurrection", Connect
  fees on Checkout, donations with validated custom amounts, the connect
  broker.
- comercio (`internal/mainapp`, `internal/shopapp`): the main-app pattern,
  live/test twins, session bindings checked on return and webhook, the
  pending sweep, refunds with idempotency keys, the demo rail.
- titogo (`internal/relay`, `docs/oauth-relay.md`): the relay, egress proxy
  with pinned `Stripe-Account`, fee injection, signed webhook hops,
  deauthorize-before-delete, Stripe's OAuth caveats.
- carloku (`internal/billingstripe`, unmerged): event-id table, several
  webhook secrets.
