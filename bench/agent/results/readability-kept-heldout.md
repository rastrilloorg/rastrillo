# Readability counts, readability-kept-heldout

Counts over the code the agent wrote, app code only; its tests are counted apart, below. Each spread is median/90th percentile over functions.

| run | Rastrillo | files | code lines | funcs | cognitive | nesting | lines | far short names | duplicated | over limit | wide | long funcs | names | long files | comment share | one-impl interfaces |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 1/lending-carlos-1 | v0.30.0-bench.gb79c48634fea | 11 | 1309 | 96 | 1/4 | 1/2 | 11/22 | 0/0 | 0/0 | 0/0 | 2 | 1 | 1 | 0 | 0.14 | 0 |
| 1/lending-carlos-2 | v0.30.0-bench.gb79c48634fea | 13 | 1397 | 98 | 1/4 | 1/2 | 12/23 | 0/0 | 0/0 | 0/0 | 4 | 1 | 4 | 0 | 0.13 | 0 |
| 1/lending-carlos-3 | v0.30.0-bench.gb79c48634fea | 12 | 1248 | 79 | 2/6 | 1/2 | 13/27 | 0/0 | 0/0 | 0/1 | 4 | 3 | 8 | 0 | 0.14 | 0 |
| 1/noticeboard-carlos-1 | v0.30.0-bench.gb79c48634fea | 11 | 1031 | 73 | 1/4 | 1/1 | 11/23 | 0/0 | 0/0 | 0/0 | 3 | 1 | 4 | 0 | 0.15 | 0 |
| 1/noticeboard-carlos-2 | v0.30.0-bench.gb79c48634fea | 11 | 965 | 69 | 1/3 | 1/1 | 12/29 | 0/0 | 0/0 | 0/0 | 3 | 1 | 7 | 0 | 0.18 | 0 |
| 1/noticeboard-carlos-3 | v0.30.0-bench.gb79c48634fea | 11 | 1155 | 78 | 1/3 | 1/1 | 13/28 | 0/0 | 0/0 | 0/0 | 2 | 1 | 1 | 0 | 0.14 | 0 |
| accept-1/lending-carlos-1 | v0.30.0-bench.gf99c96d2b252 | 14 | 1213 | 96 | 1/5 | 1/2 | 12/26 | 0/0 | 0/0 | 0/0 | 3 | 0 | 0 | 0 | 0.14 | 0 |

## Tests

The test files the agent wrote, counted as the app code is.

| run | files | code lines | funcs | wide | long funcs | names | long files | comment share |
|---|---|---|---|---|---|---|---|---|
| 1/lending-carlos-1 | 7 | 637 | 46 | 6 | 0 | 12 | 0 | 0.08 |
| 1/lending-carlos-2 | 5 | 534 | 37 | 14 | 0 | 27 | 0 | 0.09 |
| 1/lending-carlos-3 | 5 | 451 | 30 | 53 | 0 | 27 | 0 | 0.09 |
| 1/noticeboard-carlos-1 | 6 | 592 | 34 | 9 | 0 | 10 | 0 | 0.07 |
| 1/noticeboard-carlos-2 | 7 | 609 | 40 | 7 | 0 | 7 | 0 | 0.06 |
| 1/noticeboard-carlos-3 | 5 | 483 | 31 | 6 | 0 | 8 | 0 | 0.07 |
| accept-1/lending-carlos-1 | 7 | 614 | 35 | 2 | 0 | 5 | 0 | 0.06 |

## Highest cognitive complexity

| run | function | cognitive | nesting | lines |
|---|---|---|---|---|
| accept-1/lending-carlos-1 | internal/lending/shelf.go `Desk.shelfData` | 12 | 2 | 32 |
| 1/noticeboard-carlos-3 | internal/noticeboard/replies.go `Replies.Send` | 10 | 1 | 38 |
| 1/lending-carlos-2 | cmd/lending/main.go `main` | 8 | 1 | 85 |
| 1/lending-carlos-1 | cmd/lending/main.go `main` | 8 | 1 | 83 |
| 1/lending-carlos-3 | internal/lending/library.go `Library.Borrow` | 8 | 1 | 35 |

## Lowest cognitive complexity

Ties go to the longer function.

| run | function | cognitive | nesting | lines |
|---|---|---|---|---|
| 1/noticeboard-carlos-3 | internal/noticeboard/seed.go `olderNotices` | 0 | 0 | 32 |
| 1/noticeboard-carlos-3 | internal/noticeboard/seed.go `recentNotices` | 0 | 0 | 31 |
| accept-1/lending-carlos-1 | internal/lending/handlers.go `Routes` | 0 | 0 | 27 |
| 1/noticeboard-carlos-2 | internal/noticeboard/app.go `app.routes` | 0 | 0 | 27 |
| 1/lending-carlos-3 | internal/lending/handlers.go `app.routes` | 0 | 0 | 27 |
