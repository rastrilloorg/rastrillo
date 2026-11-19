# Agent benchmark, round cost-1

- Date: 2026-10-31
- Model: claude-opus-5-5
- Claude Code: 2.1.312
- Rastrillo: c89896cd6ebd (v0.30.0-bench.gc89896cd6ebd)
- Control platform: Fly.io
- Platform poll: poll-2026-10-30.json
- Getting-started skill: e92307c11f5b
- Machine policy (sha256): 47433b0aa1a2018a
- Control runs reused from round 1.

## groomer

| | carlos | control |
|---|---|---|
| runs (with agent failures) | 3 (0) | 3 (0) |
| build cost ($) | 6.11 (5.86–7.35) | 1.78 (1.63–2.00) |
| build turns | 101 (99–103) | 30 (24–39) |
| build minutes | 14.2 (14.0–18.1) | 8.1 (7.8–8.3) |
| build context at first write (k) | 112.4 (107.5–145.2) | 30.2 (29.3–31.0) |
| build deploy and live-check messages | 12 (11–15) | 6 (5–7) |
| build messages: framework-read | 2 (1–16) | 0 |
| build messages: app-read | 7 (5–9) | 1 (0–2) |
| build messages: scaffold | 3 (2–5) | 0 (0–1) |
| build messages: write | 24 (19–27) | 9 (7–10) |
| build messages: build-test | 10 (8–11) | 6 (5–7) |
| build messages: gates | 5 (3–5) | 0 |
| build messages: screenshots | 0 (0–5) | 1 (0–8) |
| build messages: deploy | 10 (9–11) | 5 (5–6) |
| build messages: live-check | 3 (1–4) | 0 (0–2) |
| build messages: other | 28 (13–30) | 4 (4–7) |
| build home: security headers present | 5 | 4 |
| build home: total over the wire (kB) | 165.0 (165.0–165.2) | 4.1 (3.6–5.4) |
| build home: CSS over the wire (kB) | 83.5 (83.4–83.5) | 2.0 (0–2.3) |
| build home: JS over the wire (kB) | 79.1 (79.1–79.2) | 0 |
| build home: axe serious or critical | 0 | 0 (0–1) |
| build home: Lighthouse performance | 99 (98–100) | 100 |
| build home: Lighthouse accessibility | 100 | 100 (86–100) |
| build home: Lighthouse best-practices | 96 | 96 (96–100) |
| build book: total over the wire (kB) | 165.2 (164.9–165.5) | 4.4 (4.0–5.3) |
| build book: CSS over the wire (kB) | 83.5 (83.4–83.7) | 2.0 (0–2.4) |
| build book: JS over the wire (kB) | 79.2 (79.1–79.3) | 0 |
| build book: axe serious or critical | 0 | 1 |
| build book: Lighthouse performance | 100 (99–100) | 100 |
| build book: Lighthouse accessibility | 100 | 94 (89–94) |
| build book: Lighthouse best-practices | 96 | 96 (96–100) |
| build signin: total over the wire (kB) | 174.0 (174.0–174.4) | 3.8 (3.1–5.0) |
| build signin: CSS over the wire (kB) | 83.5 (83.5–83.6) | 2.0 (0–2.3) |
| build signin: JS over the wire (kB) | 87.8 (87.8–87.8) | 0 |
| build signin: axe serious or critical | 0 | 0 (0–1) |
| build signin: Lighthouse performance | 100 | 100 |
| build signin: Lighthouse accessibility | 100 | 100 (91–100) |
| build signin: Lighthouse best-practices | 96 | 96 (96–100) |
| build owner: total over the wire (kB) | 164.2 (164.1–164.5) | 3.7 (3.5–3.9) |
| build owner: CSS over the wire (kB) | 83.4 (83.3–83.5) | 1.9 (0–2.3) |
| build owner: JS over the wire (kB) | 79.0 (79.0–79.0) | 0 |
| build owner: axe serious or critical | 0 | 1 (0–2) |
| build owner: Lighthouse performance | – | – |
| build owner: Lighthouse accessibility | – | – |
| build owner: Lighthouse best-practices | – | – |
| follow-up cost ($) | 3.34 (2.25–3.67) | 2.10 (1.86–2.17) |
| follow-up turns | 39 (29–54) | 30 (29–37) |
| follow-up minutes | 7.3 (5.7–7.9) | 7.7 (7.2–8.8) |
| follow-up context at first write (k) | 243.4 (238.8–293.1) | 86.4 (79.9–95.5) |
| follow-up deploy and live-check messages | 1 (1–3) | 1 (1–2) |
| follow-up messages: framework-read | 0 | 0 |
| follow-up messages: app-read | 5 (4–9) | 2 (1–2) |
| follow-up messages: scaffold | 0 | 0 |
| follow-up messages: write | 7 (5–8) | 8 (4–9) |
| follow-up messages: build-test | 4 (1–5) | 2 (2–5) |
| follow-up messages: gates | 2 (1–3) | 0 |
| follow-up messages: screenshots | 6 (6–9) | 7 (5–10) |
| follow-up messages: deploy | 1 (1–3) | 1 (1–2) |
| follow-up messages: live-check | 0 | 0 |
| follow-up messages: other | 4 (3–15) | 4 (2–4) |
| follow-up home: security headers present | 5 | 4 |
| follow-up home: total over the wire (kB) | 169.8 (165.9–170.6) | 10.1 (9.3–11.0) |
| follow-up home: CSS over the wire (kB) | 85.8 (82.3–86.3) | 4.9 (4.6–5.6) |
| follow-up home: JS over the wire (kB) | 79.2 (79.1–79.2) | 0 |
| follow-up home: axe serious or critical | 0 | 0 |
| follow-up home: Lighthouse performance | 100 (99–100) | 100 |
| follow-up home: Lighthouse accessibility | 100 | 100 |
| follow-up home: Lighthouse best-practices | 100 (96–100) | 100 |
| follow-up book: total over the wire (kB) | 168.4 (164.7–168.5) | 8.3 (8.0–10.0) |
| follow-up book: CSS over the wire (kB) | 85.7 (82.4–86.3) | 4.9 (4.6–5.6) |
| follow-up book: JS over the wire (kB) | 79.2 (79.2–79.2) | 0 |
| follow-up book: axe serious or critical | 0 | 1 (0–1) |
| follow-up book: Lighthouse performance | 99 (99–100) | 100 |
| follow-up book: Lighthouse accessibility | 100 | 96 (96–100) |
| follow-up book: Lighthouse best-practices | 100 (96–100) | 100 |
| follow-up signin: total over the wire (kB) | 177.4 (173.2–177.6) | 7.2 (7.0–8.5) |
| follow-up signin: CSS over the wire (kB) | 85.8 (82.4–86.3) | 4.9 (4.6–5.6) |
| follow-up signin: JS over the wire (kB) | 87.8 (87.8–87.9) | 0 |
| follow-up signin: axe serious or critical | 0 | 0 |
| follow-up signin: Lighthouse performance | 100 (99–100) | 100 |
| follow-up signin: Lighthouse accessibility | 100 | 100 |
| follow-up signin: Lighthouse best-practices | 100 (96–100) | 100 |
| follow-up owner: total over the wire (kB) | 167.7 (163.3–167.8) | 7.9 (7.3–9.1) |
| follow-up owner: CSS over the wire (kB) | 85.6 (82.3–86.2) | 4.9 (4.6–5.5) |
| follow-up owner: JS over the wire (kB) | 79.0 (79.0–79.0) | 0 |
| follow-up owner: axe serious or critical | 0 | 0 (0–1) |
| follow-up owner: Lighthouse performance | – | – |
| follow-up owner: Lighthouse accessibility | – | – |
| follow-up owner: Lighthouse best-practices | – | – |

Failures and measurement notes:

- carlos 1: build measurement: signin: requests still open after 30s; weight counts what finished
- carlos 1: follow-up measurement: signin: requests still open after 30s; weight counts what finished
- carlos 2: build measurement: signin: requests still open after 30s; weight counts what finished
- carlos 2: follow-up measurement: signin: requests still open after 30s; weight counts what finished
- carlos 3: build measurement: signin: requests still open after 30s; weight counts what finished
- carlos 3: follow-up measurement: signin: requests still open after 30s; weight counts what finished
