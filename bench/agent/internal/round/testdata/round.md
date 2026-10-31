# Agent benchmark, round 1

- Date: 2026-10-30
- Model: claude-opus-5-5
- Claude Code: 2.1.312
- Rastrillo: abc123 (v0.30.0-bench.gabc123)
- Control platform: Fly.io
- A note.

## groomer

| | carlos | control |
|---|---|---|
| runs (with agent failures) | 3 (1) | 3 (0) |
| build cost ($) | 7.00 (5.00–9.00) | 3.00 (3.00–4.00) |
| build turns | 80 (40–120) | 80 (40–120) |
| build minutes | 0 | 0 |
| build context at first write (k) | 100 (50–150) | 100 (50–150) |
| build deploy and live-check messages | 5 | 5 |
| build messages: framework-read | 0 | 0 |
| build messages: app-read | 0 | 0 |
| build messages: scaffold | 0 | 0 |
| build messages: write | 0 | 0 |
| build messages: build-test | 0 | 0 |
| build messages: gates | 0 | 0 |
| build messages: screenshots | 0 | 0 |
| build messages: deploy | 0 | 0 |
| build messages: live-check | 0 | 0 |
| build messages: other | 0 | 0 |
| build home: security headers present | – | – |
| build home: total over the wire (kB) | 22 (12–32) | 22 (12–32) |
| build home: CSS over the wire (kB) | 20 (10–30) | 20 (10–30) |
| build home: JS over the wire (kB) | 0 | 0 |
| build home: axe serious or critical | 1 (0–2) | 1 (0–2) |
| build home: Lighthouse performance | 92 (91–93) | 92 (91–93) |
| build home: Lighthouse accessibility | 100 | 100 |
| build home: Lighthouse best-practices | – | – |
| follow-up cost ($) | 1.00 | 1.00 |
| follow-up turns | 10 | 10 |
| follow-up minutes | 0 | 0 |
| follow-up context at first write (k) | – | – |
| follow-up deploy and live-check messages | 0 | 0 |
| follow-up messages: framework-read | 0 | 0 |
| follow-up messages: app-read | 0 | 0 |
| follow-up messages: scaffold | 0 | 0 |
| follow-up messages: write | 0 | 0 |
| follow-up messages: build-test | 0 | 0 |
| follow-up messages: gates | 0 | 0 |
| follow-up messages: screenshots | 0 | 0 |
| follow-up messages: deploy | 0 | 0 |
| follow-up messages: live-check | 0 | 0 |
| follow-up messages: other | 0 | 0 |
| follow-up home: security headers present | – | – |
| follow-up home: total over the wire (kB) | 22 (12–32) | 22 (12–32) |
| follow-up home: CSS over the wire (kB) | 20 (10–30) | 20 (10–30) |
| follow-up home: JS over the wire (kB) | 0 | 0 |
| follow-up home: axe serious or critical | 1 (0–2) | 1 (0–2) |
| follow-up home: Lighthouse performance | 92 (91–93) | 92 (91–93) |
| follow-up home: Lighthouse accessibility | 100 | 100 |
| follow-up home: Lighthouse best-practices | – | – |

Failures and measurement notes:

- carlos 2: build: the deploy does not work
