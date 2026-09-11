# Optional PWA and native clients

Approved direction: Paul, 2026-09-11. Implementation tracking:
`rastrillo/rastrillo` discussion 26, branch `feat/client-kits`.

## Boundary

Rastrillo remains the web framework. PWA and Native are optional sibling
kits with separate repositories, releases and skills. The main skill points
to them rather than loading platform instructions for every app. Core never
depends on either kit.

PWA covers installation, worker lifecycle and a public offline fallback.
Offline data and writes are a separate capability, usable through browser
and native adapters once a second application proves the contracts. Crypto
is optional and composes through compatible existing crypto/keyring APIs;
the worker does not acquire keys merely because an app uses a PWA kit.

This supersedes the September 7 aviso spec's decision to offer installation
only as a recipe. It preserves aviso's transport/policy boundary: one
app-owned worker composes PWA navigation and aviso notification handlers.
Neither the PWA package nor the web framework imports aviso at runtime.
The PWA example imports it to prove composition.

## Extraction audit

| Source inspected | Finding | Initial action |
| --- | --- | --- |
| Rastrillo `crypto`, `keyring`, vectors and `aviso` addon | Existing shared contracts already cover primitives and push transport. | Reuse; do not add a second push sender or encryption protocol. |
| Eleven `web/static/sw.js`, `store.js`, `crypto.js` | Worker decrypts app-specific notifications and writes ciphertext rows into the page's message store. It has no generic navigation cache. | Keep message and key policy in the app; build the small missing PWA lifecycle/fallback kit. |
| Ocho at `000b882c`, `ios/LChatCore` | Native core has a refresh coalescer with tests for burst behaviour and waiter freshness. | Extract as `RastrilloNative.CoalescedRunner`. |
| Keymail at `59d785f`, `apple/KeymailCore` | Same coalescer implementation copied from Eleven; API, crypto and linking remain app-specific. | Adopt the shared package with a public type alias and retain app regression tests. |
| Woodstar local checkout, README and source listing | Go/ES-module prototype with crypto parity vectors; no Swift/Kotlin/Java source found. | Keep as a future protocol/offline candidate, not proof of a native component. "Woodstart" was not identified separately. |
| Eleven `messenger-mobile/mobile` local checkout | Separate Go engine module exists. Its application protocol is not the shared Swift refresh primitive. | Record as a later Go Mobile source; no automatic engine rewrite. |

Eleven and Ocho share ancestry. Two copies there alone would not establish
an independent second consumer. Keymail supplies the second use case for
the refresh component, not for an offline data model.

## First delivery

- `amadan.net/rastrillo/pwa`: manifest handler, embedded browser/worker
  helpers, explicit updates, public fallback, aviso example and skill.
- `amadan.net/rastrillo/native`: Swift refresh package, iOS/macOS companion
  scaffold, skill and optional Go Mobile binding guidance.
- Keymail and Ocho adoption branches replace the copied implementation and
  pin the shared package to an immutable amadan revision. No account,
  notification or encryption behaviour changes.

The PWA fallback is embedded in worker code rather than fetched into Cache
Storage. This prevents an installation fetch from caching a login redirect
or content rendered for the installing account. No runtime data cache is
introduced. Only failed in-scope GET navigations receive the fallback;
HTTP errors, API calls and writes retain their network behaviour.

No unconditional `skipWaiting` or `clients.claim`: a waiting worker is
reported to the app, which resolves unsaved work across tabs before asking
for activation. The basic example recommends saving and closing all tabs.
Copy was reviewed before implementation; it tells the person what to do.

Native UI stays platform-owned. The scaffold checks a server's public
version endpoint and does not claim authenticated linking. Go Mobile is an
optional bridge for app-owned Go logic; Android adapters, native push,
signing and store delivery are not part of this first implementation.

Paul's follow-up makes fully native UI the preferred starting point. Menus,
right-click/contextual actions, links, keyboard interaction, accessibility
and navigation should meet the platform's expectations. Complex apps can
retain selected webview surfaces behind native navigation and typed action
bridges. A shared app manifest may describe destinations, capabilities and
commands for distinct web and native renderers; it should not encode a
common DOM or force a shared layout. This is a proposed architecture, not
an implemented dual-target compiler or an extension already supported by
the existing resource manifest generator.

## Validation and landing

Each kit has `make ci` and matching `.amadan/ci.d` steps. PWA checks include
the separate example module and Chromium/WebKit navigation and two-tab
updates. Native checks include the shared tests and unsigned iOS simulator
and macOS scaffold builds. Adoption runs each app's existing Swift suite
and native builds. Physical-device installation and actual push delivery
remain distinct checks, not implied by browser or compile gates.

New kit repositories begin with an empty root commit on their feature
branch. Amadan refuses an unborn default branch; the operator must seed
`main` from that empty commit before implementation lands through
`amadan branch merge`. No implementation is pushed directly to `main`.
Consumer branches wait until their pinned native revision is landed.

## Offline acceptance gate

Do not advertise an offline sync kit until a second, different app proves:
durable pending operations across restart; duplicate-safe retries after a
lost acknowledgement; isolation and revocation across accounts; local
migrations; conflicts and deletes; foreground recovery when background
execution is unavailable. Encrypted storage also needs tested key recovery,
locking, account removal and notification-preview rules. These are future
deliverables, not unchecked claims of the PWA fallback kit.
