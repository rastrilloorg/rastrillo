# Agent benchmark, round accept-1

- Date: 2026-10-31
- Model: claude-opus-5-5
- Claude Code: 2.1.312
- Rastrillo: f99c96d2b252 (v0.30.0-bench.gf99c96d2b252)
- Control platform: Fly.io
- Platform poll: poll-2026-10-30.json
- Getting-started skill: e92307c11f5b
- Machine policy (sha256): 47433b0aa1a2018a
- Control runs reused from round 1.
- lending carlos 2 and 3 were rerun: Carloku refused to create their apps (the operator's ten app creations a day per address, shared by every agent on the machine, had run out), so neither deployed. That is the platform's limit, not the agent's. The carlos arm now deploys with a service credential scoped to the demo account, which has its own budget.
- groomer spec 3's build wrote three result records (the agent had started background work); the bench read only the last and scored a $7.03, 92-turn build as $0.32. Recomputed from the transcript with bench rescore.
- The round stopped once on the token's 5-hour usage limit and was resumed after it reset; the runs it interrupted were rerun.
- Control runs reused from round 1.
- lending carlos 2 and 3 and noticeboard carlos 1 to 3 are out, to be rerun: they deployed with a demo-account service credential, which Carloku refuses `carlos instances enable` (it needs a person), so none got an instance and every page answered 503. The agents stopped and said so rather than use the operator's own login. The credential is revoked; the carlos arm deploys as the operator again.
- The five carlos runs left out were not rerun, at Paul's call: iteration 2 of #158 and #159 supersedes iteration 1, and is measured against this round's spec arm on all three briefs instead. So this round's carlos arm covers groomer (3 runs) and lending (1 run) only.

## groomer

| | carlos | control | spec |
|---|---|---|---|
| runs (with agent failures) | 3 (0) | 3 (0) | 3 (0) |
| build cost ($) | 8.14 (6.90–9.34) | 1.78 (1.63–2.00) | 7.03 (6.83–8.51) |
| build turns | 117 (105–147) | 30 (24–39) | 92 (70–95) |
| build minutes | 20.9 (19.1–31.9) | 8.1 (7.8–8.3) | 31.4 (25.5–32.5) |
| build context at first write (k) | 119.9 (116.8–125.5) | 30.2 (29.3–31.0) | 56.5 (51.5–58.6) |
| build deploy and live-check messages | 14 (13–15) | 6 (5–7) | 9 (9–12) |
| build messages: framework-read | 14 (2–18) | 0 | 2 (2–3) |
| build messages: app-read | 15 (11–19) | 1 (0–2) | 2 (1–8) |
| build messages: scaffold | 2 (2–3) | 0 (0–1) | 3 (1–3) |
| build messages: write | 28 (23–36) | 9 (7–10) | 29 (29–44) |
| build messages: build-test | 14 (14–17) | 6 (5–7) | 9 (6–10) |
| build messages: gates | 3 (2–5) | 0 | 3 (1–4) |
| build messages: screenshots | 4 (1–5) | 1 (0–8) | 1 (0–7) |
| build messages: deploy | 12 (10–14) | 5 (5–6) | 7 (6–7) |
| build messages: live-check | 2 (1–3) | 0 (0–2) | 2 (2–6) |
| build messages: other | 21 (18–26) | 4 (4–7) | 13 (10–17) |
| build home: security headers present | 5 | 4 | 6 |
| build home: total over the wire (kB) | 36.1 (35.9–36.4) | 4.1 (3.6–5.4) | 6.5 (5.7–6.6) |
| build home: CSS over the wire (kB) | 22.6 (22.5–22.9) | 2.0 (0–2.3) | 2.7 (2.4–3.0) |
| build home: JS over the wire (kB) | 10.9 (10.9–10.9) | 0 | 0.8 (0.7–0.9) |
| build home: axe serious or critical | 0 | 0 (0–1) | 0 |
| build home: Lighthouse performance | 100 | 100 | 100 |
| build home: Lighthouse accessibility | 100 | 100 (86–100) | 100 |
| build home: Lighthouse best-practices | 96 | 96 (96–100) | 96 |
| build book: total over the wire (kB) | 35.9 (35.7–36.1) | 4.4 (4.0–5.3) | 6.5 (6.2–6.6) |
| build book: CSS over the wire (kB) | 22.6 (22.6–22.9) | 2.0 (0–2.4) | 2.7 (2.3–3.0) |
| build book: JS over the wire (kB) | 10.9 (10.9–11.0) | 0 | 0.8 (0.7–0.9) |
| build book: axe serious or critical | 0 | 1 | 0 |
| build book: Lighthouse performance | 100 | 100 | 100 |
| build book: Lighthouse accessibility | 100 | 94 (89–94) | 100 |
| build book: Lighthouse best-practices | 96 | 96 (96–100) | 96 |
| build signin: total over the wire (kB) | 44.9 (44.9–45.3) | 3.8 (3.1–5.0) | 6.2 (5.5–6.3) |
| build signin: CSS over the wire (kB) | 22.6 (22.6–22.9) | 2.0 (0–2.3) | 2.7 (2.3–3.0) |
| build signin: JS over the wire (kB) | 19.5 (19.5–19.6) | 0 | 0.8 (0.7–0.9) |
| build signin: axe serious or critical | 0 | 0 (0–1) | 0 |
| build signin: Lighthouse performance | 100 | 100 | 100 |
| build signin: Lighthouse accessibility | 100 | 100 (91–100) | 100 |
| build signin: Lighthouse best-practices | 96 | 96 (96–100) | 96 |
| build owner: total over the wire (kB) | 35.2 (35.1–35.5) | 3.7 (3.5–3.9) | 5.1 (4.9–6.1) |
| build owner: CSS over the wire (kB) | 22.5 (22.4–22.8) | 1.9 (0–2.3) | 2.6 (2.3–3.0) |
| build owner: JS over the wire (kB) | 10.8 | 0 | 0.8 (0.6–0.8) |
| build owner: axe serious or critical | 0 | 1 (0–2) | 0 |
| build owner: Lighthouse performance | – | – | – |
| build owner: Lighthouse accessibility | – | – | – |
| build owner: Lighthouse best-practices | – | – | – |
| follow-up cost ($) | 3.11 (2.16–5.85) | 2.10 (1.86–2.17) | 3.59 (2.98–3.94) |
| follow-up turns | 37 (25–68) | 30 (29–37) | 47 (38–52) |
| follow-up minutes | 6.5 (6.2–17.6) | 7.7 (7.2–8.8) | 8.9 (6.7–10.2) |
| follow-up context at first write (k) | 297.4 (266.8–317.1) | 86.4 (79.9–95.5) | 233.0 (227.3–276.4) |
| follow-up deploy and live-check messages | 1 | 1 (1–2) | 2 (1–2) |
| follow-up messages: framework-read | 1 (0–2) | 0 | 0 |
| follow-up messages: app-read | 8 (2–9) | 2 (1–2) | 1 (0–3) |
| follow-up messages: scaffold | 0 | 0 | 0 (0–1) |
| follow-up messages: write | 7 (4–14) | 8 (4–9) | 11 (10–12) |
| follow-up messages: build-test | 4 (3–6) | 2 (2–5) | 6 (2–7) |
| follow-up messages: gates | 1 (1–4) | 0 | 0 |
| follow-up messages: screenshots | 5 (5–12) | 7 (5–10) | 12 (11–16) |
| follow-up messages: deploy | 1 | 1 (1–2) | 1 (0–1) |
| follow-up messages: live-check | 0 | 0 | 1 (0–2) |
| follow-up messages: other | 5 (4–15) | 4 (2–4) | 6 (5–6) |
| follow-up home: security headers present | 5 | 4 | 6 |
| follow-up home: total over the wire (kB) | 41.0 (40.7–41.5) | 10.1 (9.3–11.0) | 11.4 (10.6–11.4) |
| follow-up home: CSS over the wire (kB) | 25.0 (24.8–25.4) | 4.9 (4.6–5.6) | 4.7 (4.6–5.4) |
| follow-up home: JS over the wire (kB) | 10.9 | 0 | 0.8 (0.7–0.9) |
| follow-up home: axe serious or critical | 0 | 0 | 0 |
| follow-up home: Lighthouse performance | 100 | 100 | 100 (93–100) |
| follow-up home: Lighthouse accessibility | 100 | 100 | 100 |
| follow-up home: Lighthouse best-practices | 100 | 100 | 100 |
| follow-up book: total over the wire (kB) | 38.9 (38.7–39.6) | 8.3 (8.0–10.0) | 9.2 (8.2–9.6) |
| follow-up book: CSS over the wire (kB) | 25.0 (24.8–25.4) | 4.9 (4.6–5.6) | 4.7 (4.6–5.5) |
| follow-up book: JS over the wire (kB) | 10.9 (10.9–10.9) | 0 | 0.8 (0.7–0.9) |
| follow-up book: axe serious or critical | 0 | 1 (0–1) | 0 |
| follow-up book: Lighthouse performance | 100 | 100 | 100 (94–100) |
| follow-up book: Lighthouse accessibility | 100 | 96 (96–100) | 100 |
| follow-up book: Lighthouse best-practices | 100 | 100 | 100 |
| follow-up signin: total over the wire (kB) | 47.9 (47.9–48.5) | 7.2 (7.0–8.5) | 8.0 (7.7–8.7) |
| follow-up signin: CSS over the wire (kB) | 25.0 (24.8–25.5) | 4.9 (4.6–5.6) | 4.7 (4.5–5.5) |
| follow-up signin: JS over the wire (kB) | 19.5 (19.5–19.5) | 0 | 0.8 (0.7–0.9) |
| follow-up signin: axe serious or critical | 0 | 0 | 0 |
| follow-up signin: Lighthouse performance | 100 | 100 | 99 (98–100) |
| follow-up signin: Lighthouse accessibility | 100 | 100 | 100 |
| follow-up signin: Lighthouse best-practices | 100 | 100 | 100 |
| follow-up owner: total over the wire (kB) | 38.3 (38.2–38.8) | 7.9 (7.3–9.1) | 7.8 (7.6–9.2) |
| follow-up owner: CSS over the wire (kB) | 24.9 (24.7–25.3) | 4.9 (4.6–5.5) | 4.6 (4.5–5.3) |
| follow-up owner: JS over the wire (kB) | 10.8 | 0 | 0.8 (0.6–0.8) |
| follow-up owner: axe serious or critical | 0 | 0 (0–1) | 0 |
| follow-up owner: Lighthouse performance | – | – | – |
| follow-up owner: Lighthouse accessibility | – | – | – |
| follow-up owner: Lighthouse best-practices | – | – | – |

Failures and measurement notes:

- carlos 1: build measurement: signin: requests still open after 30s; weight counts what finished
- carlos 1: follow-up measurement: signin: requests still open after 30s; weight counts what finished
- carlos 2: build measurement: signin: requests still open after 30s; weight counts what finished
- carlos 2: follow-up measurement: signin: requests still open after 30s; weight counts what finished
- carlos 3: build measurement: signin: requests still open after 30s; weight counts what finished
- carlos 3: follow-up measurement: signin: requests still open after 30s; weight counts what finished

## lending

| | carlos | control | spec |
|---|---|---|---|
| runs (with agent failures) | 1 (0) | 3 (0) | 3 (0) |
| build cost ($) | 9.10 | 1.41 (1.05–2.16) | 7.61 (6.62–9.00) |
| build turns | 123 | 24 (22–48) | 91 (77–92) |
| build minutes | 30.8 | 7.4 (5–10.2) | 25.9 (25–36.3) |
| build context at first write (k) | 156.9 | 29.5 (27.7–30.3) | 57.4 (55.8–61.4) |
| build deploy and live-check messages | 13 | 6 (5–8) | 9 (7–16) |
| build messages: framework-read | 22 | 0 | 1 (1–2) |
| build messages: app-read | 8 | 0 (0–1) | 4 (3–6) |
| build messages: scaffold | 4 | 1 (0–2) | 3 (1–3) |
| build messages: write | 20 | 8 (6–9) | 31 (27–37) |
| build messages: build-test | 17 | 8 (4–9) | 11 (8–17) |
| build messages: gates | 6 | 0 | 3 |
| build messages: screenshots | 5 | 0 (0–5) | 0 (0–2) |
| build messages: deploy | 10 | 5 (5–7) | 8 (6–12) |
| build messages: live-check | 3 | 1 (0–1) | 1 (1–4) |
| build messages: other | 15 | 4 (2–6) | 16 (13–22) |
| build home: security headers present | 5 | 4 | 6 |
| build home: total over the wire (kB) | 44.5 | 3.2 (2.5–3.7) | 4.7 (4.2–4.7) |
| build home: CSS over the wire (kB) | 22.2 | 1.4 (0–1.8) | 2.3 (2.3–2.4) |
| build home: JS over the wire (kB) | 19.5 | 0 (0–0.3) | 0 |
| build home: axe serious or critical | 0 | 0 | 0 |
| build home: Lighthouse performance | 100 | 100 | 100 |
| build home: Lighthouse accessibility | 100 | 100 | 100 |
| build home: Lighthouse best-practices | 96 | 100 (96–100) | 96 (96–100) |
| build signin: total over the wire (kB) | 44.4 | 3.2 (2.4–3.8) | 4.6 (4.0–4.6) |
| build signin: CSS over the wire (kB) | 22.2 | 1.4 (0–1.9) | 2.3 (2.2–2.3) |
| build signin: JS over the wire (kB) | 19.5 | 0 (0–0.3) | 0 |
| build signin: axe serious or critical | 0 | 0 | 0 |
| build signin: Lighthouse performance | 100 | 100 | 100 |
| build signin: Lighthouse accessibility | 100 | 100 | 100 |
| build signin: Lighthouse best-practices | 96 | 100 (96–100) | 96 (96–100) |
| build borrow: total over the wire (kB) | 34.7 | 3.4 (3.1–4.5) | 4.5 (4.3–4.5) |
| build borrow: CSS over the wire (kB) | 22.0 | 1.3 (0–1.8) | 2.2 (2.2–2.2) |
| build borrow: JS over the wire (kB) | 10.8 | 0 (0–0.3) | 0 |
| build borrow: axe serious or critical | 0 | 1 (0–1) | 0 |
| build borrow: Lighthouse performance | – | – | – |
| build borrow: Lighthouse accessibility | – | – | – |
| build borrow: Lighthouse best-practices | – | – | – |
| build overdue: total over the wire (kB) | 35.0 | 4.0 (3.2–4.8) | 4.2 (3.9–4.4) |
| build overdue: CSS over the wire (kB) | 22.0 | 1.4 (0–1.8) | 2.3 (2.2–2.3) |
| build overdue: JS over the wire (kB) | 10.8 | 0 (0–0.3) | 0 |
| build overdue: axe serious or critical | 0 | 1 (0–1) | 0 |
| build overdue: Lighthouse performance | – | – | – |
| build overdue: Lighthouse accessibility | – | – | – |
| build overdue: Lighthouse best-practices | – | – | – |
| follow-up cost ($) | 4.22 | 1.37 (1.35–2.75) | 3.85 (2.61–4.02) |
| follow-up turns | 43 | 32 (26–50) | 43 (30–55) |
| follow-up minutes | 17.3 | 6.2 (5.4–10.8) | 10.4 (7.7–11.9) |
| follow-up context at first write (k) | 320.3 | 72.5 (69.0–105.4) | 237.2 (222.8–275.1) |
| follow-up deploy and live-check messages | 2 | 2 (1–2) | 2 |
| follow-up messages: framework-read | 2 | 0 | 0 |
| follow-up messages: app-read | 8 | 4 (1–4) | 2 (1–3) |
| follow-up messages: scaffold | 0 | 0 (0–1) | 0 |
| follow-up messages: write | 4 | 4 (4–6) | 7 (7–14) |
| follow-up messages: build-test | 9 | 4 (4–5) | 8 (4–10) |
| follow-up messages: gates | 2 | 0 (0–2) | 0 |
| follow-up messages: screenshots | 10 | 7 (5–13) | 10 (5–10) |
| follow-up messages: deploy | 1 | 2 (1–2) | 0 |
| follow-up messages: live-check | 1 | 0 | 2 |
| follow-up messages: other | 2 | 5 (3–6) | 7 (5–7) |
| follow-up home: security headers present | 5 | 4 | 6 |
| follow-up home: total over the wire (kB) | 46.7 | 6.1 (5.7–7.6) | 6.6 (6.2–8.6) |
| follow-up home: CSS over the wire (kB) | 24.2 | 4.0 (3.5–4.2) | 4.2 (4.0–6.0) |
| follow-up home: JS over the wire (kB) | 19.5 | 0 (0–0.8) | 0 |
| follow-up home: axe serious or critical | 0 | 0 | 0 |
| follow-up home: Lighthouse performance | 100 | 100 | 100 |
| follow-up home: Lighthouse accessibility | 100 | 100 | 100 |
| follow-up home: Lighthouse best-practices | 96 | 100 | 100 |
| follow-up signin: total over the wire (kB) | 46.6 | 6.0 (5.6–7.6) | 6.4 (6.1–8.5) |
| follow-up signin: CSS over the wire (kB) | 24.2 | 4.0 (3.5–4.2) | 4.1 (4.0–6.0) |
| follow-up signin: JS over the wire (kB) | 19.6 | 0 (0–0.9) | 0 |
| follow-up signin: axe serious or critical | 0 | 0 | 0 |
| follow-up signin: Lighthouse performance | 100 | 100 | 100 |
| follow-up signin: Lighthouse accessibility | 100 | 100 | 100 |
| follow-up signin: Lighthouse best-practices | 96 | 100 | 100 |
| follow-up borrow: total over the wire (kB) | 37.5 | 7.3 (6.5–8.7) | 7.6 (7.4–10.0) |
| follow-up borrow: CSS over the wire (kB) | 24.1 | 3.9 (3.5–4.2) | 4.1 (4.0–5.9) |
| follow-up borrow: JS over the wire (kB) | 10.8 | 0 (0–0.8) | 0 |
| follow-up borrow: axe serious or critical | 0 | 0 | 0 |
| follow-up borrow: Lighthouse performance | – | – | – |
| follow-up borrow: Lighthouse accessibility | – | – | – |
| follow-up borrow: Lighthouse best-practices | – | – | – |
| follow-up overdue: total over the wire (kB) | 37.2 | 7.8 (6.5–9.2) | 6.6 (6.0–8.3) |
| follow-up overdue: CSS over the wire (kB) | 24.1 | 4.0 (3.5–4.2) | 4.1 (4.0–5.9) |
| follow-up overdue: JS over the wire (kB) | 10.8 | 0 (0–0.8) | 0 |
| follow-up overdue: axe serious or critical | 0 | 0 | 0 |
| follow-up overdue: Lighthouse performance | – | – | – |
| follow-up overdue: Lighthouse accessibility | – | – | – |
| follow-up overdue: Lighthouse best-practices | – | – | – |

Failures and measurement notes:

- carlos 1: build measurement: home: requests still open after 30s; weight counts what finished
- carlos 1: build measurement: signin: requests still open after 30s; weight counts what finished
- carlos 1: follow-up measurement: home: requests still open after 30s; weight counts what finished
- carlos 1: follow-up measurement: signin: requests still open after 30s; weight counts what finished

## noticeboard

| | carlos | control | spec |
|---|---|---|---|
| runs (with agent failures) | – | 3 (0) | 3 (0) |
| build cost ($) | – | 1.35 (1.28–2.03) | 7.68 (6.98–10.90) |
| build turns | – | 22 (22–30) | 89 (86–92) |
| build minutes | – | 6 (6.0–9.0) | 29.2 (27.1–33.9) |
| build context at first write (k) | – | 29.4 (27.2–35.0) | 55.3 (52.5–57.0) |
| build deploy and live-check messages | – | 5 (4–8) | 10 (9–14) |
| build messages: framework-read | – | 0 | 3 (1–4) |
| build messages: app-read | – | 1 (0–1) | 7 (2–18) |
| build messages: scaffold | – | 0 (0–1) | 3 (2–3) |
| build messages: write | – | 8 (7–9) | 26 (24–32) |
| build messages: build-test | – | 4 (3–7) | 14 (6–20) |
| build messages: gates | – | 0 | 3 (3–7) |
| build messages: screenshots | – | 0 | 4 (0–10) |
| build messages: deploy | – | 5 (4–5) | 8 (7–8) |
| build messages: live-check | – | 0 (0–3) | 2 (2–6) |
| build messages: other | – | 4 (4–5) | 16 (14–18) |
| build home: security headers present | – | 4 (4–5) | 6 |
| build home: total over the wire (kB) | – | 5.8 (5.5–6.5) | 6.9 (5.5–7.9) |
| build home: CSS over the wire (kB) | – | 2.4 (2.2–2.8) | 2.8 (2.7–3.4) |
| build home: JS over the wire (kB) | – | 0 | 0 (0–0.9) |
| build home: axe serious or critical | – | 0 (0–1) | 0 |
| build home: Lighthouse performance | – | 100 | 100 |
| build home: Lighthouse accessibility | – | 100 (96–100) | 100 |
| build home: Lighthouse best-practices | – | 96 | 100 (96–100) |
| build search: total over the wire (kB) | – | 4.5 (4.3–5.2) | 5.5 (4.8–6.3) |
| build search: CSS over the wire (kB) | – | 2.4 (2.2–2.9) | 2.8 (2.8–3.3) |
| build search: JS over the wire (kB) | – | 0 | 0 (0–0.8) |
| build search: axe serious or critical | – | 0 (0–1) | 0 |
| build search: Lighthouse performance | – | 100 | 100 |
| build search: Lighthouse accessibility | – | 100 (96–100) | 100 |
| build search: Lighthouse best-practices | – | 96 | 100 (96–100) |
| build listing: total over the wire (kB) | – | 4.6 (4.4–5.4) | 5.8 (5.3–6.7) |
| build listing: CSS over the wire (kB) | – | 2.4 (2.2–2.9) | 2.8 (2.8–3.3) |
| build listing: JS over the wire (kB) | – | 0 | 0 (0–0.8) |
| build listing: axe serious or critical | – | 0 (0–1) | 0 |
| build listing: Lighthouse performance | – | 100 | 100 |
| build listing: Lighthouse accessibility | – | 100 (95–100) | 100 |
| build listing: Lighthouse best-practices | – | 96 | 100 (96–100) |
| build contact: total over the wire (kB) | – | 4.6 (4.6–5.4) | 5.8 (5.3–6.7) |
| build contact: CSS over the wire (kB) | – | 2.4 (2.2–2.8) | 2.8 (2.7–3.3) |
| build contact: JS over the wire (kB) | – | 0 | 0 (0–0.9) |
| build contact: axe serious or critical | – | 0 (0–1) | 0 |
| build contact: Lighthouse performance | – | 100 | 100 (2 of 3) |
| build contact: Lighthouse accessibility | – | 100 (95–100) | 100 |
| build contact: Lighthouse best-practices | – | 96 | 100 (96–100) |
| follow-up cost ($) | – | 2.27 (2.14–2.28) | 3.13 (2.52–3.85) |
| follow-up turns | – | 51 (38–51) | 37 (30–38) |
| follow-up minutes | – | 7.3 (7.2–9.1) | 8.6 (8.6–10.7) |
| follow-up context at first write (k) | – | 72.0 (71.1–87.5) | 241.3 (225.4–295.5) |
| follow-up deploy and live-check messages | – | 2 (1–3) | 2 (2–3) |
| follow-up messages: framework-read | – | 0 | 0 |
| follow-up messages: app-read | – | 2 (2–5) | 2 (1–3) |
| follow-up messages: scaffold | – | 0 | 0 |
| follow-up messages: write | – | 7 (6–12) | 8 (7–8) |
| follow-up messages: build-test | – | 6 (5–7) | 7 (6–8) |
| follow-up messages: gates | – | 0 | 0 (0–1) |
| follow-up messages: screenshots | – | 14 (5–17) | 8 (3–11) |
| follow-up messages: deploy | – | 1 (0–3) | 1 (0–1) |
| follow-up messages: live-check | – | 1 (0–1) | 2 (1–2) |
| follow-up messages: other | – | 6 (6–8) | 5 (3–6) |
| follow-up home: security headers present | – | 4 (4–5) | 6 |
| follow-up home: total over the wire (kB) | – | 11.1 (9.1–12.9) | 12.6 (12.1–12.9) |
| follow-up home: CSS over the wire (kB) | – | 6.2 (5.0–6.3) | 6.8 (5.7–6.9) |
| follow-up home: JS over the wire (kB) | – | 0 | 0 (0–0.9) |
| follow-up home: axe serious or critical | – | 0 | 0 |
| follow-up home: Lighthouse performance | – | 100 | 100 |
| follow-up home: Lighthouse accessibility | – | 100 | 100 |
| follow-up home: Lighthouse best-practices | – | 100 | 100 (96–100) |
| follow-up search: total over the wire (kB) | – | 9.2 (7.6–9.6) | 10.8 (9.4–11.0) |
| follow-up search: CSS over the wire (kB) | – | 6.2 (5.0–6.2) | 6.8 (5.6–7.0) |
| follow-up search: JS over the wire (kB) | – | 0 | 0 (0–0.9) |
| follow-up search: axe serious or critical | – | 0 | 0 |
| follow-up search: Lighthouse performance | – | 100 | 100 |
| follow-up search: Lighthouse accessibility | – | 100 | 100 (98–100) |
| follow-up search: Lighthouse best-practices | – | 100 | 100 (96–100) |
| follow-up listing: total over the wire (kB) | – | 9.8 (8.1–10.0) | 11.0 (10.4–11.5) |
| follow-up listing: CSS over the wire (kB) | – | 6.2 (5.0–6.2) | 6.8 (5.6–6.8) |
| follow-up listing: JS over the wire (kB) | – | 0 | 0 (0–0.8) |
| follow-up listing: axe serious or critical | – | 0 | 0 |
| follow-up listing: Lighthouse performance | – | 100 | 100 |
| follow-up listing: Lighthouse accessibility | – | 100 | 100 |
| follow-up listing: Lighthouse best-practices | – | 100 | 100 (96–100) |
| follow-up contact: total over the wire (kB) | – | 9.8 (8.0–10.2) | 11.0 (10.4–11.8) |
| follow-up contact: CSS over the wire (kB) | – | 6.2 (5.0–6.2) | 6.8 (5.6–7.0) |
| follow-up contact: JS over the wire (kB) | – | 0 | 0 (0–0.9) |
| follow-up contact: axe serious or critical | – | 0 | 0 |
| follow-up contact: Lighthouse performance | – | 100 | 100 (2 of 3) |
| follow-up contact: Lighthouse accessibility | – | 100 | 100 |
| follow-up contact: Lighthouse best-practices | – | 100 | 100 (96–100) |
