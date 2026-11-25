# Readability counts, readability-read-1

Counts over the code the agent wrote, app code only; its tests are counted apart, below. Each spread is median/90th percentile over functions.

| run | Rastrillo | files | code lines | funcs | cognitive | nesting | lines | far short names | duplicated | over limit | wide | long funcs | names | long files | comment share | one-impl interfaces |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| read-1/groomer-carlos-1 | v0.45.0-bench.g85cbb9f2e4ba | 15 | 1462 | 95 | 1/5 | 1/2 | 12/25 | 0/0 | 0/0 | 0/0 | 0 | 0 | 0 | 0 | 0.08 | 0 |
| read-1/groomer-carlos-2 | v0.45.0-bench.g85cbb9f2e4ba | 15 | 1405 | 101 | 1/5 | 1/2 | 11/23 | 0/0 | 0/0 | 0/0 | 0 | 0 | 0 | 0 | 0.08 | 0 |
| read-1/groomer-carlos-3 | v0.45.0-bench.g85cbb9f2e4ba | 14 | 1482 | 111 | 1/4 | 1/2 | 9/20 | 0/0 | 0/0 | 0/0 | 0 | 0 | 0 | 0 | 0.08 | 0 |
| read-1/groomer-carlos-gopls-1 | v0.45.0-bench.g85cbb9f2e4ba | 16 | 1375 | 100 | 1/4 | 1/2 | 12/19 | 0/0 | 0/0 | 0/0 | 0 | 0 | 0 | 0 | 0.09 | 0 |
| read-1/groomer-carlos-gopls-2 | v0.45.0-bench.g85cbb9f2e4ba | 20 | 1609 | 106 | 1/5 | 1/2 | 12.5/24 | 0/0 | 0/0 | 0/0 | 0 | 0 | 0 | 0 | 0.09 | 0 |
| read-1/groomer-carlos-gopls-3 | v0.45.0-bench.g85cbb9f2e4ba | 19 | 1646 | 117 | 1/5 | 1/2 | 11/24 | 0/0 | 0/0 | 0/0 | 0 | 0 | 0 | 0 | 0.08 | 0 |
| read-1/lending-carlos-1 | v0.45.0-bench.g85cbb9f2e4ba | 13 | 1218 | 82 | 1/5 | 1/2 | 12/22 | 0/0 | 0/0 | 0/0 | 0 | 0 | 0 | 0 | 0.08 | 0 |
| read-1/lending-carlos-2 | v0.45.0-bench.g85cbb9f2e4ba | 11 | 1104 | 80 | 1/4 | 1/1 | 11/20 | 0/0 | 0/0 | 0/0 | 0 | 0 | 0 | 0 | 0.09 | 0 |
| read-1/lending-carlos-3 | v0.45.0-bench.g85cbb9f2e4ba | 15 | 1347 | 97 | 2/4 | 1/2 | 11/22 | 0/0 | 0/0 | 0/0 | 0 | 0 | 0 | 0 | 0.08 | 0 |
| read-1/lending-carlos-gopls-1 | v0.45.0-bench.g85cbb9f2e4ba | 12 | 1123 | 80 | 2/3 | 1/2 | 12/21 | 0/0 | 0/0 | 0/0 | 0 | 0 | 0 | 0 | 0.08 | 0 |
| read-1/lending-carlos-gopls-2 | v0.45.0-bench.g85cbb9f2e4ba | 12 | 1277 | 88 | 1/5 | 1/2 | 12/25 | 0/0 | 0/0 | 0/0 | 0 | 0 | 0 | 0 | 0.08 | 0 |
| read-1/lending-carlos-gopls-3 | v0.45.0-bench.g85cbb9f2e4ba | 14 | 1354 | 91 | 1/5 | 1/2 | 12/26 | 0/0 | 0/0 | 0/0 | 0 | 0 | 0 | 0 | 0.08 | 0 |
| read-1/noticeboard-carlos-1 | v0.45.0-bench.g85cbb9f2e4ba | 13 | 933 | 59 | 1/4 | 1/1 | 13/22 | 0/0 | 0/0 | 0/0 | 0 | 0 | 0 | 0 | 0.08 | 0 |
| read-1/noticeboard-carlos-2 | v0.45.0-bench.g85cbb9f2e4ba | 13 | 1073 | 89 | 1/3 | 1/1 | 11/18 | 0/0 | 0/0 | 0/0 | 0 | 0 | 0 | 0 | 0.08 | 0 |
| read-1/noticeboard-carlos-3 | v0.45.0-bench.g85cbb9f2e4ba | 13 | 1146 | 72 | 1/3 | 1/1 | 13/23 | 0/0 | 0/0 | 0/0 | 0 | 0 | 0 | 0 | 0.07 | 0 |
| read-1/noticeboard-carlos-gopls-1 | v0.45.0-bench.g85cbb9f2e4ba | 15 | 1044 | 77 | 1/3 | 1/1 | 11/20 | 0/0 | 0/0 | 0/0 | 0 | 0 | 0 | 0 | 0.08 | 0 |
| read-1/noticeboard-carlos-gopls-2 | v0.45.0-bench.g85cbb9f2e4ba | 12 | 996 | 67 | 1/4 | 1/1 | 12/22 | 0/0 | 0/0 | 0/0 | 0 | 0 | 0 | 0 | 0.09 | 0 |
| read-1/noticeboard-carlos-gopls-3 | v0.45.0-bench.g85cbb9f2e4ba | 13 | 1228 | 79 | 1/3 | 1/1 | 12/22 | 0/0 | 0/0 | 0/0 | 0 | 0 | 0 | 0 | 0.07 | 0 |

## Tests

The test files the agent wrote, counted as the app code is.

| run | files | code lines | funcs | wide | long funcs | names | long files | comment share |
|---|---|---|---|---|---|---|---|---|
| read-1/groomer-carlos-1 | 6 | 485 | 25 | 0 | 0 | 0 | 0 | 0.04 |
| read-1/groomer-carlos-2 | 6 | 590 | 33 | 0 | 0 | 0 | 0 | 0.04 |
| read-1/groomer-carlos-3 | 6 | 674 | 38 | 0 | 0 | 0 | 0 | 0.04 |
| read-1/groomer-carlos-gopls-1 | 7 | 490 | 31 | 0 | 0 | 0 | 0 | 0.06 |
| read-1/groomer-carlos-gopls-2 | 7 | 668 | 40 | 0 | 0 | 0 | 0 | 0.03 |
| read-1/groomer-carlos-gopls-3 | 6 | 574 | 34 | 0 | 0 | 0 | 0 | 0.04 |
| read-1/lending-carlos-1 | 6 | 545 | 37 | 0 | 0 | 0 | 0 | 0.04 |
| read-1/lending-carlos-2 | 5 | 442 | 25 | 0 | 0 | 0 | 0 | 0.03 |
| read-1/lending-carlos-3 | 8 | 611 | 39 | 0 | 0 | 0 | 0 | 0.02 |
| read-1/lending-carlos-gopls-1 | 6 | 575 | 32 | 0 | 0 | 0 | 0 | 0.04 |
| read-1/lending-carlos-gopls-2 | 5 | 505 | 34 | 0 | 0 | 0 | 0 | 0.03 |
| read-1/lending-carlos-gopls-3 | 6 | 518 | 30 | 0 | 0 | 0 | 0 | 0.05 |
| read-1/noticeboard-carlos-1 | 7 | 434 | 29 | 0 | 0 | 0 | 0 | 0.04 |
| read-1/noticeboard-carlos-2 | 8 | 596 | 38 | 0 | 0 | 0 | 0 | 0.03 |
| read-1/noticeboard-carlos-3 | 6 | 449 | 27 | 0 | 0 | 0 | 0 | 0.03 |
| read-1/noticeboard-carlos-gopls-1 | 6 | 469 | 26 | 0 | 0 | 0 | 0 | 0.03 |
| read-1/noticeboard-carlos-gopls-2 | 6 | 483 | 26 | 0 | 0 | 0 | 0 | 0.04 |
| read-1/noticeboard-carlos-gopls-3 | 6 | 553 | 34 | 0 | 0 | 0 | 0 | 0.04 |

## Highest cognitive complexity

| run | function | cognitive | nesting | lines |
|---|---|---|---|---|
| read-1/lending-carlos-gopls-3 | internal/lending/home.go `shelfCategories` | 10 | 2 | 35 |
| read-1/groomer-carlos-gopls-3 | internal/groomer/seed.go `seedDemo` | 10 | 1 | 29 |
| read-1/groomer-carlos-gopls-2 | internal/groomer/seed.go `SeedDemo` | 10 | 1 | 28 |
| read-1/groomer-carlos-2 | internal/groomer/hours.go `Hours.Set` | 10 | 2 | 25 |
| read-1/groomer-carlos-1 | internal/groomer/salon.go `Salon.SetWeek` | 10 | 2 | 22 |

## Lowest cognitive complexity

Ties go to the longer function.

| run | function | cognitive | nesting | lines |
|---|---|---|---|---|
| read-1/lending-carlos-2 | internal/lending/routes.go `Routes` | 0 | 0 | 30 |
| read-1/noticeboard-carlos-gopls-2 | internal/noticeboard/routes.go `Routes` | 0 | 0 | 29 |
| read-1/noticeboard-carlos-2 | internal/noticeboard/routes.go `Routes` | 0 | 0 | 29 |
| read-1/groomer-carlos-gopls-2 | internal/groomer/routes.go `Routes` | 0 | 0 | 29 |
| read-1/groomer-carlos-2 | internal/groomer/routes.go `Routes` | 0 | 0 | 29 |
