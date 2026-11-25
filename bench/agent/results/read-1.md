# Agent benchmark, round read-1

- Date: 2026-11-22
- Model: claude-opus-5-5
- Claude Code: 2.1.321
- Rastrillo: 85cbb9f2e4ba (v0.45.0-bench.g85cbb9f2e4ba)
- gopls: golang.org/x/tools/gopls v0.24.0
- gopls instructions (sha256): c22be2d4ea39
- Getting-started skill: 69e128252caf
- Machine policy (sha256): 47433b0aa1a2018a
- carlos-gopls is carlos with gopls over MCP, set up as gopls's own docs set it up: `gopls mcp` runs behind the bench's goplsmcp wrapper, which starts it again in the app's module once go.mod appears (Claude Code's MCP never lets gopls find a module made after it starts), and the arm's system prompt ends with `gopls mcp -instructions` after the environment notes. The prompt itself is carlos's, word for word.

## groomer

| | carlos | carlos-gopls |
|---|---|---|
| runs (with agent failures) | 3 (0) | 3 (0) |
| build cost ($) | 8.01 (7.55–9.19) | 7.17 (6.06–7.96) |
| build turns | 117 (104–142) | 117 (100–125) |
| build minutes | 25.7 (20.9–29.8) | 20.1 (15.8–21.0) |
| build context at first write (k) | 123.8 (113.2–130.9) | 119.3 (112.9–123.8) |
| build deploy and live-check messages | 7 (7–8) | 6 (6–8) |
| build failed builds and test runs | 12 (4–20) | 10 (9–12) |
| build gopls calls: go_diagnostics | 0 | 2 (1–2) |
| build gopls calls: go_package_api | 0 | 0 (0–1) |
| build gopls calls: go_vulncheck | 0 | 1 |
| build gopls calls: go_workspace | 0 | 1 (1–2) |
| build messages: framework-read | 26 (5–28) | 2 (1–12) |
| build messages: app-read | 7 (5–15) | 7 (6–14) |
| build messages: scaffold | 2 (2–4) | 3 |
| build messages: write | 28 (23–31) | 29 (19–37) |
| build messages: build-test | 17 (12–32) | 15 (12–24) |
| build messages: gates | 4 | 5 (5–7) |
| build messages: screenshots | 4 (1–4) | 3 (2–4) |
| build messages: deploy | 6 (6–7) | 6 (5–7) |
| build messages: live-check | 1 | 1 (0–1) |
| build messages: other | 19 (15–26) | 26 (22–37) |
| build home: security headers present | 5 | 5 |
| build home: total over the wire (kB) | 20.5 (19.4–20.7) | 20.1 (19.6–20.2) |
| build home: CSS over the wire (kB) | 10.3 (9.4–10.6) | 10.2 (9.4–10.4) |
| build home: JS over the wire (kB) | 6.8 (6.8–6.8) | 6.8 |
| build home: axe serious or critical | 0 | 0 |
| build home: Lighthouse performance | 100 | 100 |
| build home: Lighthouse accessibility | 100 | 100 |
| build home: Lighthouse best-practices | 96 | 96 |
| build book: total over the wire (kB) | 19.5 (19.2–19.7) | 19.7 (19.6–20.2) |
| build book: CSS over the wire (kB) | 9.3 (9.3–9.6) | 9.7 (9.6–10.5) |
| build book: JS over the wire (kB) | 6.8 (6.8–6.8) | 6.8 (6.8–6.8) |
| build book: axe serious or critical | 0 (0–1) | 0 |
| build book: Lighthouse performance | 100 | 100 |
| build book: Lighthouse accessibility | 100 (96–100) | 100 |
| build book: Lighthouse best-practices | 96 | 96 |
| build signin: total over the wire (kB) | 31.1 (30.7–31.2) | 30.9 (30.7–31.2) |
| build signin: CSS over the wire (kB) | 12.1 (11.8–12.3) | 12.2 (11.9–12.2) |
| build signin: JS over the wire (kB) | 15.5 (15.4–15.5) | 15.4 (15.4–15.5) |
| build signin: axe serious or critical | 0 | 0 |
| build signin: Lighthouse performance | 100 | 100 |
| build signin: Lighthouse accessibility | 100 | 100 |
| build signin: Lighthouse best-practices | 96 | 96 |
| build owner: total over the wire (kB) | 20.6 (20.3–20.7) | 20.1 (19.8–20.1) |
| build owner: CSS over the wire (kB) | 10.8 (10.7–10.8) | 10.6 (10.5–10.7) |
| build owner: JS over the wire (kB) | 6.6 | 6.7 (6.6–6.7) |
| build owner: axe serious or critical | 0 | 0 |
| build owner: Lighthouse performance | – | – |
| build owner: Lighthouse accessibility | – | – |
| build owner: Lighthouse best-practices | – | – |
| follow-up cost ($) | 2.40 (1.89–2.74) | 2.09 (2.02–2.66) |
| follow-up turns | 28 (24–36) | 28 (25–36) |
| follow-up minutes | 5.6 (5.2–7) | 5.8 (5.3–7.8) |
| follow-up context at first write (k) | 279.2 (273.6–280.3) | 251.4 (240.0–280.9) |
| follow-up deploy and live-check messages | 1 | 1 |
| follow-up failed builds and test runs | 2 (1–2) | 0 (0–2) |
| follow-up gopls calls: go_diagnostics | 0 | 0 (0–1) |
| follow-up gopls calls: go_package_api | 0 | 0 |
| follow-up gopls calls: go_vulncheck | 0 | 0 |
| follow-up gopls calls: go_workspace | 0 | 0 |
| follow-up messages: framework-read | 0 (0–2) | 0 |
| follow-up messages: app-read | 2 (1–2) | 3 (1–5) |
| follow-up messages: scaffold | 0 | 0 |
| follow-up messages: write | 5 (5–7) | 4 (3–7) |
| follow-up messages: build-test | 3 (2–6) | 2 (1–6) |
| follow-up messages: gates | 1 (1–3) | 2 (1–3) |
| follow-up messages: screenshots | 7 (3–9) | 6 (3–7) |
| follow-up messages: deploy | 1 | 1 |
| follow-up messages: live-check | 0 | 0 |
| follow-up messages: other | 4 (3–6) | 5 (4–7) |
| follow-up home: security headers present | 5 | 5 |
| follow-up home: total over the wire (kB) | 24.4 (23.8–24.5) | 25.2 (23.6–25.4) |
| follow-up home: CSS over the wire (kB) | 11.2 (10.9–11.6) | 11.7 (10.8–12.9) |
| follow-up home: JS over the wire (kB) | 6.8 (6.8–6.8) | 6.8 (6.8–6.8) |
| follow-up home: axe serious or critical | 0 | 0 |
| follow-up home: Lighthouse performance | 100 | 100 |
| follow-up home: Lighthouse accessibility | 100 | 100 |
| follow-up home: Lighthouse best-practices | 100 | 100 |
| follow-up book: total over the wire (kB) | 21.8 (21.8–22.5) | 22.5 (22.3–23.0) |
| follow-up book: CSS over the wire (kB) | 11.1 (11.1–11.6) | 12.0 (11.4–12.1) |
| follow-up book: JS over the wire (kB) | 6.8 (6.8–6.8) | 6.8 (6.8–6.8) |
| follow-up book: axe serious or critical | 0 (0–1) | 0 |
| follow-up book: Lighthouse performance | 100 | 100 |
| follow-up book: Lighthouse accessibility | 100 (96–100) | 100 |
| follow-up book: Lighthouse best-practices | 100 | 100 |
| follow-up signin: total over the wire (kB) | 33.3 (33.3–34.1) | 33.3 (33.2–34.5) |
| follow-up signin: CSS over the wire (kB) | 13.7 (13.6–14.4) | 13.8 (13.7–14.5) |
| follow-up signin: JS over the wire (kB) | 15.4 (15.4–15.5) | 15.5 (15.4–15.5) |
| follow-up signin: axe serious or critical | 0 | 0 |
| follow-up signin: Lighthouse performance | 100 | 100 |
| follow-up signin: Lighthouse accessibility | 100 | 100 |
| follow-up signin: Lighthouse best-practices | 100 | 100 |
| follow-up owner: total over the wire (kB) | 22.8 (22.7–23.6) | 22.7 (22.4–23.9) |
| follow-up owner: CSS over the wire (kB) | 12.5 (12.1–13.1) | 12.3 (12.1–13.0) |
| follow-up owner: JS over the wire (kB) | 6.6 (6.6–6.7) | 6.7 (6.6–6.7) |
| follow-up owner: axe serious or critical | 0 | 0 |
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
- carlos-gopls 1: build measurement: signin: requests still open after 30s; weight counts what finished
- carlos-gopls 1: follow-up measurement: signin: requests still open after 30s; weight counts what finished
- carlos-gopls 2: build measurement: signin: requests still open after 30s; weight counts what finished
- carlos-gopls 2: follow-up measurement: signin: requests still open after 30s; weight counts what finished
- carlos-gopls 3: build measurement: signin: requests still open after 30s; weight counts what finished
- carlos-gopls 3: follow-up measurement: signin: requests still open after 30s; weight counts what finished

## lending

| | carlos | carlos-gopls |
|---|---|---|
| runs (with agent failures) | 3 (0) | 3 (0) |
| build cost ($) | 7.59 (7.03–8.69) | 6.59 (6.48–8.36) |
| build turns | 118 (117–130) | 117 (110–130) |
| build minutes | 21.7 (21.3–34.2) | 27.4 (22.9–34.4) |
| build context at first write (k) | 127.7 (124.8–136.1) | 122.4 (110.9–135.4) |
| build deploy and live-check messages | 7 (6–8) | 8 (5–9) |
| build failed builds and test runs | 12 (9–15) | 12 (12–14) |
| build gopls calls: go_diagnostics | 0 | 1 (1–2) |
| build gopls calls: go_package_api | 0 | 1 (0–1) |
| build gopls calls: go_vulncheck | 0 | 1 (0–1) |
| build gopls calls: go_workspace | 0 | 1 (1–2) |
| build messages: framework-read | 28 (26–28) | 14 (8–16) |
| build messages: app-read | 10 (5–11) | 9 (7–12) |
| build messages: scaffold | 3 (2–3) | 2 (2–3) |
| build messages: write | 25 (15–31) | 26 (21–26) |
| build messages: build-test | 21 (15–22) | 18 (12–22) |
| build messages: gates | 4 (4–5) | 4 (3–9) |
| build messages: screenshots | 0 (0–1) | 4 (1–7) |
| build messages: deploy | 6 (5–7) | 7 (4–8) |
| build messages: live-check | 1 | 1 |
| build messages: other | 18 (14–20) | 25 (24–28) |
| build home: security headers present | 5 | 5 |
| build home: total over the wire (kB) | 30.6 (30.1–30.7) | 30.3 (30.2–30.8) |
| build home: CSS over the wire (kB) | 11.8 (11.2–11.8) | 11.4 (11.4–11.9) |
| build home: JS over the wire (kB) | 15.4 (15.4–15.4) | 15.4 (15.4–15.5) |
| build home: axe serious or critical | 0 | 0 |
| build home: Lighthouse performance | 100 | 100 |
| build home: Lighthouse accessibility | 100 | 100 |
| build home: Lighthouse best-practices | 96 | 96 |
| build signin: total over the wire (kB) | 30.5 (30.1–30.6) | 30.3 (30.1–30.8) |
| build signin: CSS over the wire (kB) | 11.7 (11.3–11.8) | 11.4 (11.4–11.9) |
| build signin: JS over the wire (kB) | 15.5 (15.4–15.5) | 15.4 (15.4–15.5) |
| build signin: axe serious or critical | 0 | 0 |
| build signin: Lighthouse performance | 100 | 100 |
| build signin: Lighthouse accessibility | 100 | 100 |
| build signin: Lighthouse best-practices | 96 | 96 |
| build borrow: total over the wire (kB) | 19.9 (19.2–20.0) | 19.3 (19.0–20.1) |
| build borrow: CSS over the wire (kB) | 10.1 (9.5–10.2) | 10.0 (9.8–10.6) |
| build borrow: JS over the wire (kB) | 6.6 | 6.6 |
| build borrow: axe serious or critical | 0 | 0 |
| build borrow: Lighthouse performance | – | – |
| build borrow: Lighthouse accessibility | – | – |
| build borrow: Lighthouse best-practices | – | – |
| build overdue: total over the wire (kB) | 20.4 (19.8–20.5) | 19.9 (19.4–19.9) |
| build overdue: CSS over the wire (kB) | 10.0 (9.8–10.8) | 10.1 (10.1–10.6) |
| build overdue: JS over the wire (kB) | 6.7 (6.6–6.7) | 6.6 (6.6–6.7) |
| build overdue: axe serious or critical | 0 | 0 |
| build overdue: Lighthouse performance | – | – |
| build overdue: Lighthouse accessibility | – | – |
| build overdue: Lighthouse best-practices | – | – |
| follow-up cost ($) | 2.77 (2.31–4.56) | 3.23 (2.65–3.92) |
| follow-up turns | 40 (29–48) | 42 (37–47) |
| follow-up minutes | 6.0 (5.3–18.4) | 7.6 (6.0–7.7) |
| follow-up context at first write (k) | 281.9 (257.9–293.9) | 252.2 (249.1–289.0) |
| follow-up deploy and live-check messages | 2 (1–3) | 1 (1–2) |
| follow-up failed builds and test runs | 5 (2–9) | 2 (1–3) |
| follow-up gopls calls: go_diagnostics | 0 | 0 (0–1) |
| follow-up gopls calls: go_package_api | 0 | 0 |
| follow-up gopls calls: go_vulncheck | 0 | 0 |
| follow-up gopls calls: go_workspace | 0 | 0 |
| follow-up messages: framework-read | 3 (1–5) | 4 (0–7) |
| follow-up messages: app-read | 5 (2–8) | 9 (0–9) |
| follow-up messages: scaffold | 0 | 0 |
| follow-up messages: write | 7 (5–10) | 7 (4–7) |
| follow-up messages: build-test | 10 (8–16) | 9 (5–13) |
| follow-up messages: gates | 2 (2–3) | 1 (1–4) |
| follow-up messages: screenshots | 0 | 3 (0–4) |
| follow-up messages: deploy | 1 | 1 |
| follow-up messages: live-check | 1 (0–2) | 0 (0–1) |
| follow-up messages: other | 4 (3–5) | 8 (5–8) |
| follow-up home: security headers present | 5 | 5 |
| follow-up home: total over the wire (kB) | 32.3 (31.9–32.4) | 32.2 (32.0–32.3) |
| follow-up home: CSS over the wire (kB) | 13.2 (12.9–13.4) | 12.9 (12.9–13.4) |
| follow-up home: JS over the wire (kB) | 15.4 | 15.4 (15.4–15.5) |
| follow-up home: axe serious or critical | 0 | 0 |
| follow-up home: Lighthouse performance | 100 | 100 |
| follow-up home: Lighthouse accessibility | 100 | 100 |
| follow-up home: Lighthouse best-practices | 96 | 96 |
| follow-up signin: total over the wire (kB) | 32.3 (31.8–32.3) | 32.1 (32.0–32.3) |
| follow-up signin: CSS over the wire (kB) | 13.3 (13.0–13.4) | 12.9 (12.9–13.3) |
| follow-up signin: JS over the wire (kB) | 15.4 (15.4–15.5) | 15.5 (15.4–15.5) |
| follow-up signin: axe serious or critical | 0 | 0 |
| follow-up signin: Lighthouse performance | 100 | 100 |
| follow-up signin: Lighthouse accessibility | 100 | 100 |
| follow-up signin: Lighthouse best-practices | 96 | 96 |
| follow-up borrow: total over the wire (kB) | 25.6 (24.5–25.9) | 21.8 (21.0–22.2) |
| follow-up borrow: CSS over the wire (kB) | 14.9 (14.0–14.9) | 11.8 (10.9–11.9) |
| follow-up borrow: JS over the wire (kB) | 6.6 (6.6–7.4) | 6.6 (6.6–6.7) |
| follow-up borrow: axe serious or critical | 0 | 0 |
| follow-up borrow: Lighthouse performance | – | – |
| follow-up borrow: Lighthouse accessibility | – | – |
| follow-up borrow: Lighthouse best-practices | – | – |
| follow-up overdue: total over the wire (kB) | 22.1 (21.7–22.5) | 21.3 (21.1–22.2) |
| follow-up overdue: CSS over the wire (kB) | 11.6 (11.6–12.4) | 11.6 (11.6–12.3) |
| follow-up overdue: JS over the wire (kB) | 6.7 (6.7–6.7) | 6.7 (6.6–6.9) |
| follow-up overdue: axe serious or critical | 0 | 0 |
| follow-up overdue: Lighthouse performance | – | – |
| follow-up overdue: Lighthouse accessibility | – | – |
| follow-up overdue: Lighthouse best-practices | – | – |

Failures and measurement notes:

- carlos 1: build measurement: home: requests still open after 30s; weight counts what finished
- carlos 1: build measurement: signin: requests still open after 30s; weight counts what finished
- carlos 1: follow-up measurement: home: requests still open after 30s; weight counts what finished
- carlos 1: follow-up measurement: signin: requests still open after 30s; weight counts what finished
- carlos 2: build measurement: home: requests still open after 30s; weight counts what finished
- carlos 2: build measurement: signin: requests still open after 30s; weight counts what finished
- carlos 2: follow-up measurement: home: requests still open after 30s; weight counts what finished
- carlos 2: follow-up measurement: signin: requests still open after 30s; weight counts what finished
- carlos 3: build measurement: home: requests still open after 30s; weight counts what finished
- carlos 3: build measurement: signin: requests still open after 30s; weight counts what finished
- carlos 3: follow-up measurement: home: requests still open after 30s; weight counts what finished
- carlos 3: follow-up measurement: signin: requests still open after 30s; weight counts what finished
- carlos-gopls 1: build measurement: home: requests still open after 30s; weight counts what finished
- carlos-gopls 1: build measurement: signin: requests still open after 30s; weight counts what finished
- carlos-gopls 1: follow-up measurement: home: requests still open after 30s; weight counts what finished
- carlos-gopls 1: follow-up measurement: signin: requests still open after 30s; weight counts what finished
- carlos-gopls 2: build measurement: home: requests still open after 30s; weight counts what finished
- carlos-gopls 2: build measurement: signin: requests still open after 30s; weight counts what finished
- carlos-gopls 2: follow-up measurement: home: requests still open after 30s; weight counts what finished
- carlos-gopls 2: follow-up measurement: signin: requests still open after 30s; weight counts what finished
- carlos-gopls 3: build measurement: home: requests still open after 30s; weight counts what finished
- carlos-gopls 3: build measurement: signin: requests still open after 30s; weight counts what finished
- carlos-gopls 3: follow-up measurement: home: requests still open after 30s; weight counts what finished
- carlos-gopls 3: follow-up measurement: signin: requests still open after 30s; weight counts what finished

## noticeboard

| | carlos | carlos-gopls |
|---|---|---|
| runs (with agent failures) | 3 (0) | 3 (0) |
| build cost ($) | 6.83 (6.04–7.69) | 7.33 (6.13–8.82) |
| build turns | 105 (98–110) | 107 (101–136) |
| build minutes | 22.9 (15.5–30.9) | 27.3 (21.4–29.2) |
| build context at first write (k) | 130.6 (118.5–135.0) | 131.2 (118.8–132.9) |
| build deploy and live-check messages | 8 (8–9) | 7 (5–9) |
| build failed builds and test runs | 12 (11–12) | 10 (9–12) |
| build gopls calls: go_diagnostics | 0 | 1 (1–2) |
| build gopls calls: go_package_api | 0 | 0 (0–1) |
| build gopls calls: go_symbol_references | 0 | 0 (0–2) |
| build gopls calls: go_vulncheck | 0 | 1 |
| build gopls calls: go_workspace | 0 | 1 (1–2) |
| build messages: framework-read | 19 (6–25) | 26 (8–26) |
| build messages: app-read | 8 (6–9) | 7 (7–11) |
| build messages: scaffold | 2 (2–3) | 2 (1–3) |
| build messages: write | 21 (19–22) | 18 (16–28) |
| build messages: build-test | 16 (13–22) | 14 (11–17) |
| build messages: gates | 7 | 6 (4–8) |
| build messages: screenshots | 7 (2–7) | 5 (2–10) |
| build messages: deploy | 7 (7–8) | 6 (4–7) |
| build messages: live-check | 1 (0–2) | 1 (1–2) |
| build messages: other | 9 (9–22) | 23 (9–42) |
| build home: security headers present | 5 | 5 |
| build home: total over the wire (kB) | 21.3 (20.8–21.6) | 21.0 (20.9–21.1) |
| build home: CSS over the wire (kB) | 10.6 (10.5–11.2) | 10.6 (10.6–10.7) |
| build home: JS over the wire (kB) | 6.8 | 6.8 (6.8–6.8) |
| build home: axe serious or critical | 0 | 0 |
| build home: Lighthouse performance | 100 | 100 |
| build home: Lighthouse accessibility | 100 | 100 |
| build home: Lighthouse best-practices | 96 | 96 |
| build search: total over the wire (kB) | 20.6 (20.5–21.1) | 20.7 (20.6–20.7) |
| build search: CSS over the wire (kB) | 10.6 (10.4–11.2) | 10.6 (10.6–10.7) |
| build search: JS over the wire (kB) | 6.8 (6.8–6.8) | 6.8 (6.8–6.8) |
| build search: axe serious or critical | 0 | 0 |
| build search: Lighthouse performance | 100 | 100 |
| build search: Lighthouse accessibility | 100 | 100 |
| build search: Lighthouse best-practices | 96 | 96 |
| build listing: total over the wire (kB) | 31.6 (22.2–31.7) | 31.3 (31.2–31.7) |
| build listing: CSS over the wire (kB) | 12.3 (11.8–12.5) | 11.9 (11.9–12.0) |
| build listing: JS over the wire (kB) | 15.4 (6.8–15.4) | 15.5 (15.4–15.9) |
| build listing: axe serious or critical | 0 | 0 |
| build listing: Lighthouse performance | 100 | 100 |
| build listing: Lighthouse accessibility | 100 | 100 |
| build listing: Lighthouse best-practices | 96 | 96 |
| build contact: total over the wire (kB) | 31.6 (22.3–31.8) | 31.2 (31.2–31.3) |
| build contact: CSS over the wire (kB) | 12.3 (11.9–12.6) | 12.0 (11.9–12.0) |
| build contact: JS over the wire (kB) | 15.4 (6.8–15.5) | 15.5 (15.4–15.5) |
| build contact: axe serious or critical | 0 | 0 |
| build contact: Lighthouse performance | 100 | 100 |
| build contact: Lighthouse accessibility | 100 | 100 |
| build contact: Lighthouse best-practices | 96 | 96 |
| follow-up cost ($) | 3.00 (2.67–3.37) | 2.53 (2.48–2.89) |
| follow-up turns | 39 (33–43) | 33 (29–34) |
| follow-up minutes | 7.5 (6.9–7.7) | 6.4 (6.4–6.5) |
| follow-up context at first write (k) | 259.4 (239.9–281.4) | 282.4 (238.5–300.8) |
| follow-up deploy and live-check messages | 1 | 1 |
| follow-up failed builds and test runs | 2 (1–5) | 2 (2–4) |
| follow-up gopls calls: go_diagnostics | 0 | 0 (0–1) |
| follow-up gopls calls: go_package_api | 0 | 0 |
| follow-up gopls calls: go_symbol_references | 0 | 0 |
| follow-up gopls calls: go_vulncheck | 0 | 0 |
| follow-up gopls calls: go_workspace | 0 | 0 |
| follow-up messages: framework-read | 0 (0–7) | 0 (0–4) |
| follow-up messages: app-read | 4 (2–6) | 2 (0–2) |
| follow-up messages: scaffold | 0 | 0 |
| follow-up messages: write | 4 (3–11) | 8 (6–9) |
| follow-up messages: build-test | 4 (3–12) | 4 (3–4) |
| follow-up messages: gates | 3 (2–3) | 3 (3–4) |
| follow-up messages: screenshots | 6 (0–6) | 6 |
| follow-up messages: deploy | 1 | 1 |
| follow-up messages: live-check | 0 | 0 |
| follow-up messages: other | 5 (4–7) | 3 (2–4) |
| follow-up home: security headers present | 5 | 5 |
| follow-up home: total over the wire (kB) | 24.0 (23.9–24.2) | 23.4 (23.3–25.4) |
| follow-up home: CSS over the wire (kB) | 11.7 (11.1–11.9) | 11.7 (11.0–13.6) |
| follow-up home: JS over the wire (kB) | 6.8 (6.8–6.8) | 6.8 (6.8–6.8) |
| follow-up home: axe serious or critical | 0 | 0 |
| follow-up home: Lighthouse performance | 100 | 100 |
| follow-up home: Lighthouse accessibility | 100 | 100 |
| follow-up home: Lighthouse best-practices | 100 (96–100) | 96 (96–100) |
| follow-up search: total over the wire (kB) | 22.7 (22.3–23.8) | 23.2 (23.2–23.3) |
| follow-up search: CSS over the wire (kB) | 12.3 (11.7–13.1) | 13.0 (12.5–13.1) |
| follow-up search: JS over the wire (kB) | 6.8 (6.8–6.8) | 6.8 |
| follow-up search: axe serious or critical | 0 | 0 |
| follow-up search: Lighthouse performance | 100 | 100 |
| follow-up search: Lighthouse accessibility | 100 | 100 |
| follow-up search: Lighthouse best-practices | 100 (96–100) | 96 (96–100) |
| follow-up listing: total over the wire (kB) | 33.7 (25.2–34.3) | 33.9 (33.7–34.0) |
| follow-up listing: CSS over the wire (kB) | 14.2 (13.8–14.5) | 14.2 (13.9–14.3) |
| follow-up listing: JS over the wire (kB) | 15.4 (6.8–15.5) | 15.4 (15.4–15.5) |
| follow-up listing: axe serious or critical | 0 | 0 |
| follow-up listing: Lighthouse performance | 100 | 100 |
| follow-up listing: Lighthouse accessibility | 100 | 100 |
| follow-up listing: Lighthouse best-practices | 100 (96–100) | 96 (96–100) |
| follow-up contact: total over the wire (kB) | 33.7 (25.3–34.3) | 33.9 (33.8–34.0) |
| follow-up contact: CSS over the wire (kB) | 14.2 (13.8–14.5) | 14.3 (13.9–14.4) |
| follow-up contact: JS over the wire (kB) | 15.4 (6.8–15.4) | 15.4 (15.4–15.5) |
| follow-up contact: axe serious or critical | 0 | 0 |
| follow-up contact: Lighthouse performance | 100 | 100 |
| follow-up contact: Lighthouse accessibility | 100 | 100 |
| follow-up contact: Lighthouse best-practices | 100 (96–100) | 96 (96–100) |

Failures and measurement notes:

- carlos 1: build measurement: listing: requests still open after 30s; weight counts what finished
- carlos 1: build measurement: contact: requests still open after 30s; weight counts what finished
- carlos 1: follow-up measurement: listing: requests still open after 30s; weight counts what finished
- carlos 1: follow-up measurement: contact: requests still open after 30s; weight counts what finished
- carlos 2: build measurement: listing: requests still open after 30s; weight counts what finished
- carlos 2: build measurement: contact: requests still open after 30s; weight counts what finished
- carlos 2: follow-up measurement: listing: requests still open after 30s; weight counts what finished
- carlos 2: follow-up measurement: contact: requests still open after 30s; weight counts what finished
- carlos-gopls 1: build measurement: listing: requests still open after 30s; weight counts what finished
- carlos-gopls 1: build measurement: contact: requests still open after 30s; weight counts what finished
- carlos-gopls 1: follow-up measurement: listing: requests still open after 30s; weight counts what finished
- carlos-gopls 1: follow-up measurement: contact: requests still open after 30s; weight counts what finished
- carlos-gopls 2: build measurement: listing: requests still open after 30s; weight counts what finished
- carlos-gopls 2: build measurement: contact: requests still open after 30s; weight counts what finished
- carlos-gopls 2: follow-up measurement: listing: requests still open after 30s; weight counts what finished
- carlos-gopls 2: follow-up measurement: contact: requests still open after 30s; weight counts what finished
- carlos-gopls 3: build measurement: listing: requests still open after 30s; weight counts what finished
- carlos-gopls 3: build measurement: contact: requests still open after 30s; weight counts what finished
- carlos-gopls 3: follow-up measurement: listing: requests still open after 30s; weight counts what finished
- carlos-gopls 3: follow-up measurement: contact: requests still open after 30s; weight counts what finished

## Decision

In [read-1-decision.md](read-1-decision.md), kept by hand: `bench report` and `bench rescore` write this file again and leave that one alone.
