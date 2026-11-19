# Agent benchmark, round weight-1

- Date: 2026-10-31
- Model: claude-opus-5-5
- Claude Code: 2.1.312
- Rastrillo: a386b11d1cc8 (v0.30.0-bench.ga386b11d1cc8)
- Control platform: Fly.io
- Platform poll: poll-2026-10-30.json
- Getting-started skill: e92307c11f5b
- Machine policy (sha256): 47433b0aa1a2018a
- Control runs reused from round 1.

## groomer

| | carlos | control |
|---|---|---|
| runs (with agent failures) | 3 (0) | 3 (0) |
| build cost ($) | 8.38 (6.94–9.69) | 1.78 (1.63–2.00) |
| build turns | 123 (104–133) | 30 (24–39) |
| build minutes | 25.2 (16.2–28.1) | 8.1 (7.8–8.3) |
| build context at first write (k) | 123.2 (119.9–134.0) | 30.2 (29.3–31.0) |
| build deploy and live-check messages | 12 (11–16) | 6 (5–7) |
| build messages: framework-read | 26 (4–29) | 0 |
| build messages: app-read | 11 (5–13) | 1 (0–2) |
| build messages: scaffold | 3 (2–4) | 0 (0–1) |
| build messages: write | 28 (17–43) | 9 (7–10) |
| build messages: build-test | 16 (16–18) | 6 (5–7) |
| build messages: gates | 4 (3–7) | 0 |
| build messages: screenshots | 0 (0–2) | 1 (0–8) |
| build messages: deploy | 10 (8–12) | 5 (5–6) |
| build messages: live-check | 3 (2–4) | 0 (0–2) |
| build messages: other | 18 (9–20) | 4 (4–7) |
| build home: security headers present | 5 | 4 |
| build home: total over the wire (kB) | 35.6 (35.5–35.9) | 4.1 (3.6–5.4) |
| build home: CSS over the wire (kB) | 22.3 (22.2–22.4) | 2.0 (0–2.3) |
| build home: JS over the wire (kB) | 10.9 (10.9–10.9) | 0 |
| build home: axe serious or critical | 0 | 0 (0–1) |
| build home: Lighthouse performance | 100 | 100 |
| build home: Lighthouse accessibility | 100 | 100 (86–100) |
| build home: Lighthouse best-practices | 96 | 96 (96–100) |
| build book: total over the wire (kB) | 36.0 (35.3–36.5) | 4.4 (4.0–5.3) |
| build book: CSS over the wire (kB) | 22.3 (22.2–22.5) | 2.0 (0–2.4) |
| build book: JS over the wire (kB) | 10.9 | 0 |
| build book: axe serious or critical | 0 | 1 |
| build book: Lighthouse performance | 100 | 100 |
| build book: Lighthouse accessibility | 100 | 94 (89–94) |
| build book: Lighthouse best-practices | 96 | 96 (96–100) |
| build signin: total over the wire (kB) | 44.6 (36.0–44.7) | 3.8 (3.1–5.0) |
| build signin: CSS over the wire (kB) | 22.3 (22.2–22.5) | 2.0 (0–2.3) |
| build signin: JS over the wire (kB) | 19.5 (10.9–19.5) | 0 |
| build signin: axe serious or critical | 0 | 0 (0–1) |
| build signin: Lighthouse performance | 100 | 100 |
| build signin: Lighthouse accessibility | 100 | 100 (91–100) |
| build signin: Lighthouse best-practices | 96 | 96 (96–100) |
| build owner: total over the wire (kB) | 34.9 (34.7–35.0) | 3.7 (3.5–3.9) |
| build owner: CSS over the wire (kB) | 22.1 (22.1–22.3) | 1.9 (0–2.3) |
| build owner: JS over the wire (kB) | 10.8 (10.8–10.8) | 0 |
| build owner: axe serious or critical | 0 | 1 (0–2) |
| build owner: Lighthouse performance | – | – |
| build owner: Lighthouse accessibility | – | – |
| build owner: Lighthouse best-practices | – | – |
| follow-up cost ($) | 3.93 (2.90–4.54) | 2.10 (1.86–2.17) |
| follow-up turns | 48 (34–49) | 30 (29–37) |
| follow-up minutes | 7.7 (6.0–9.8) | 7.7 (7.2–8.8) |
| follow-up context at first write (k) | 301.1 (280.5–315.4) | 86.4 (79.9–95.5) |
| follow-up deploy and live-check messages | 1 | 1 (1–2) |
| follow-up messages: framework-read | 0 (0–2) | 0 |
| follow-up messages: app-read | 5 (5–6) | 2 (1–2) |
| follow-up messages: scaffold | 0 | 0 |
| follow-up messages: write | 6 (6–8) | 8 (4–9) |
| follow-up messages: build-test | 3 (2–10) | 2 (2–5) |
| follow-up messages: gates | 2 (1–3) | 0 |
| follow-up messages: screenshots | 8 (4–12) | 7 (5–10) |
| follow-up messages: deploy | 1 | 1 (1–2) |
| follow-up messages: live-check | 0 | 0 |
| follow-up messages: other | 9 (8–13) | 4 (2–4) |
| follow-up home: security headers present | 5 | 4 |
| follow-up home: total over the wire (kB) | 41.3 (39.6–42.3) | 10.1 (9.3–11.0) |
| follow-up home: CSS over the wire (kB) | 25.1 (24.1–25.9) | 4.9 (4.6–5.6) |
| follow-up home: JS over the wire (kB) | 10.9 | 0 |
| follow-up home: axe serious or critical | 0 | 0 |
| follow-up home: Lighthouse performance | 100 (98–100) | 100 |
| follow-up home: Lighthouse accessibility | 100 | 100 |
| follow-up home: Lighthouse best-practices | 100 (96–100) | 100 |
| follow-up book: total over the wire (kB) | 39.9 (37.8–40.6) | 8.3 (8.0–10.0) |
| follow-up book: CSS over the wire (kB) | 25.1 (24.1–26.0) | 4.9 (4.6–5.6) |
| follow-up book: JS over the wire (kB) | 10.9 | 0 |
| follow-up book: axe serious or critical | 0 | 1 (0–1) |
| follow-up book: Lighthouse performance | 100 | 100 |
| follow-up book: Lighthouse accessibility | 100 | 96 (96–100) |
| follow-up book: Lighthouse best-practices | 100 (96–100) | 100 |
| follow-up signin: total over the wire (kB) | 46.9 (39.4–49.1) | 7.2 (7.0–8.5) |
| follow-up signin: CSS over the wire (kB) | 25.1 (24.1–26.0) | 4.9 (4.6–5.6) |
| follow-up signin: JS over the wire (kB) | 19.5 (10.9–19.5) | 0 |
| follow-up signin: axe serious or critical | 0 | 0 |
| follow-up signin: Lighthouse performance | 100 | 100 |
| follow-up signin: Lighthouse accessibility | 100 | 100 |
| follow-up signin: Lighthouse best-practices | 100 (96–100) | 100 |
| follow-up owner: total over the wire (kB) | 38.5 (37.1–39.7) | 7.9 (7.3–9.1) |
| follow-up owner: CSS over the wire (kB) | 25.0 (24.0–25.9) | 4.9 (4.6–5.5) |
| follow-up owner: JS over the wire (kB) | 10.8 (10.8–10.8) | 0 |
| follow-up owner: axe serious or critical | 0 | 0 (0–1) |
| follow-up owner: Lighthouse performance | – | – |
| follow-up owner: Lighthouse accessibility | – | – |
| follow-up owner: Lighthouse best-practices | – | – |

Failures and measurement notes:

- carlos 1: build measurement: signin: requests still open after 30s; weight counts what finished
- carlos 1: follow-up measurement: signin: requests still open after 30s; weight counts what finished
- carlos 3: build measurement: signin: requests still open after 30s; weight counts what finished
- carlos 3: follow-up measurement: signin: requests still open after 30s; weight counts what finished
