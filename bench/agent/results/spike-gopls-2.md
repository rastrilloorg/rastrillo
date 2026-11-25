# Agent benchmark, round spike-gopls-2

- Date: 2026-11-22
- Model: claude-opus-5-5
- Claude Code: 2.1.321
- Rastrillo: 091ea1ded8c1 (v0.45.0-bench.g091ea1ded8c1)
- gopls: golang.org/x/tools/gopls v0.24.0
- gopls instructions (sha256): c22be2d4ea39
- Getting-started skill: 69e128252caf
- Machine policy (sha256): 47433b0aa1a2018a
- carlos-gopls is carlos with gopls over MCP, set up as gopls's own docs set it up: `gopls mcp` runs behind the bench's goplsmcp wrapper, which starts it again in the app's module once go.mod appears (Claude Code's MCP never lets gopls find a module made after it starts), and the arm's system prompt ends with `gopls mcp -instructions` after the environment notes. The prompt itself is carlos's, word for word.

## smoke

| | carlos-gopls |
|---|---|
| runs (with agent failures) | 1 (0) |
| build cost ($) | 0.90 |
| build turns | 39 |
| build minutes | 3.3 |
| build context at first write (k) | 51.9 |
| build deploy and live-check messages | 9 |
| build failed builds and test runs | 0 |
| build gopls calls: go_diagnostics | 1 |
| build gopls calls: go_vulncheck | 1 |
| build gopls calls: go_workspace | 1 |
| build messages: framework-read | 5 |
| build messages: app-read | 3 |
| build messages: scaffold | 3 |
| build messages: write | 2 |
| build messages: build-test | 4 |
| build messages: gates | 0 |
| build messages: screenshots | 0 |
| build messages: deploy | 9 |
| build messages: live-check | 0 |
| build messages: other | 7 |
| build home: security headers present | 5 |
| build home: total over the wire (kB) | 14.3 |
| build home: CSS over the wire (kB) | 6.1 |
| build home: JS over the wire (kB) | 6.0 |
| build home: axe serious or critical | 0 |
| build home: Lighthouse performance | 100 |
| build home: Lighthouse accessibility | 100 |
| build home: Lighthouse best-practices | 96 |
| follow-up cost ($) | 1.26 |
| follow-up turns | 43 |
| follow-up minutes | 5.4 |
| follow-up context at first write (k) | 69.7 |
| follow-up deploy and live-check messages | 1 |
| follow-up failed builds and test runs | 0 |
| follow-up gopls calls: go_diagnostics | 1 |
| follow-up gopls calls: go_vulncheck | 0 |
| follow-up gopls calls: go_workspace | 0 |
| follow-up messages: framework-read | 2 |
| follow-up messages: app-read | 3 |
| follow-up messages: scaffold | 0 |
| follow-up messages: write | 7 |
| follow-up messages: build-test | 2 |
| follow-up messages: gates | 0 |
| follow-up messages: screenshots | 8 |
| follow-up messages: deploy | 1 |
| follow-up messages: live-check | 0 |
| follow-up messages: other | 8 |
| follow-up home: security headers present | 5 |
| follow-up home: total over the wire (kB) | 13.6 |
| follow-up home: CSS over the wire (kB) | 6.8 |
| follow-up home: JS over the wire (kB) | 4.2 |
| follow-up home: axe serious or critical | 0 |
| follow-up home: Lighthouse performance | 100 |
| follow-up home: Lighthouse accessibility | 100 |
| follow-up home: Lighthouse best-practices | 100 |
