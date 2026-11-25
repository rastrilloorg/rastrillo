# Readability counts, readability-kept

Counts over the code the agent wrote, app code only; its tests are counted apart, below. Each spread is median/90th percentile over functions.

| run | Rastrillo | files | code lines | funcs | cognitive | nesting | lines | far short names | duplicated | over limit | wide | long funcs | names | long files | comment share | one-impl interfaces |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 1/groomer-carlos-1 | v0.30.0-bench.gb79c48634fea | 18 | 1815 | 134 | 1/4 | 1/2 | 12/23 | 0/0 | 0/0 | 0/0 | 0 | 0 | 0 | 0 | 0.14 | 0 |
| 1/groomer-carlos-2 | v0.30.0-bench.gb79c48634fea | 16 | 1604 | 115 | 1/4 | 1/1 | 13/24 | 0/0 | 0/0 | 0/0 | 0 | 1 | 1 | 0 | 0.12 | 0 |
| 1/groomer-carlos-3 | v0.30.0-bench.gb79c48634fea | 14 | 1495 | 100 | 1/5 | 1/2 | 12.5/28 | 0/0 | 0/0 | 0/0 | 5 | 3 | 8 | 0 | 0.16 | 0 |
| accept-1/groomer-carlos-1 | v0.30.0-bench.gf99c96d2b252 | 11 | 1310 | 96 | 1/5 | 1/2 | 13/24 | 0/0 | 0/0 | 0/0 | 5 | 0 | 2 | 0 | 0.14 | 0 |
| accept-1/groomer-carlos-2 | v0.30.0-bench.gf99c96d2b252 | 16 | 1504 | 101 | 1/5 | 1/2 | 12/26 | 0/0 | 0/0 | 0/0 | 5 | 2 | 6 | 0 | 0.16 | 0 |
| accept-1/groomer-carlos-3 | v0.30.0-bench.gf99c96d2b252 | 17 | 1579 | 114 | 1/5 | 1/2 | 12/23 | 0/0 | 0/0 | 0/0 | 2 | 1 | 2 | 0 | 0.15 | 0 |
| cost-1/groomer-carlos-1 | v0.30.0-bench.gc89896cd6ebd | 13 | 1281 | 84 | 1/5 | 1/2 | 12/26 | 0/0 | 0/0 | 0/0 | 5 | 2 | 4 | 0 | 0.17 | 0 |
| cost-1/groomer-carlos-2 | v0.30.0-bench.gc89896cd6ebd | 15 | 1486 | 91 | 2/6 | 1/2 | 13/24 | 0/0 | 0/0 | 0/0 | 4 | 2 | 6 | 0 | 0.15 | 0 |
| cost-1/groomer-carlos-3 | v0.30.0-bench.gc89896cd6ebd | 11 | 1197 | 89 | 1/5 | 1/2 | 12/22 | 0/0 | 0/0 | 0/0 | 4 | 1 | 5 | 0 | 0.13 | 0 |
| weight-1/groomer-carlos-1 | v0.30.0-bench.ga386b11d1cc8 | 22 | 1833 | 129 | 1/4 | 1/1 | 12/23 | 0/0 | 0/0 | 0/0 | 0 | 0 | 0 | 0 | 0.13 | 0 |
| weight-1/groomer-carlos-2 | v0.30.0-bench.ga386b11d1cc8 | 16 | 1514 | 109 | 1/5 | 1/2 | 11/27 | 0/0 | 0/0 | 0/0 | 2 | 1 | 1 | 0 | 0.12 | 0 |
| weight-1/groomer-carlos-3 | v0.30.0-bench.ga386b11d1cc8 | 20 | 1768 | 123 | 1/4 | 1/2 | 11/22 | 0/0 | 0/0 | 0/0 | 3 | 1 | 3 | 0 | 0.14 | 0 |

## Tests

The test files the agent wrote, counted as the app code is.

| run | files | code lines | funcs | wide | long funcs | names | long files | comment share |
|---|---|---|---|---|---|---|---|---|
| 1/groomer-carlos-1 | 9 | 786 | 47 | 0 | 0 | 0 | 0 | 0.09 |
| 1/groomer-carlos-2 | 5 | 556 | 39 | 2 | 0 | 7 | 0 | 0.07 |
| 1/groomer-carlos-3 | 6 | 535 | 28 | 5 | 0 | 7 | 0 | 0.10 |
| accept-1/groomer-carlos-1 | 5 | 482 | 26 | 5 | 0 | 4 | 0 | 0.05 |
| accept-1/groomer-carlos-2 | 6 | 522 | 24 | 2 | 0 | 6 | 0 | 0.08 |
| accept-1/groomer-carlos-3 | 4 | 482 | 39 | 6 | 0 | 3 | 0 | 0.11 |
| cost-1/groomer-carlos-1 | 6 | 472 | 29 | 5 | 0 | 3 | 0 | 0.11 |
| cost-1/groomer-carlos-2 | 4 | 501 | 30 | 4 | 0 | 4 | 0 | 0.06 |
| cost-1/groomer-carlos-3 | 6 | 501 | 28 | 5 | 0 | 4 | 0 | 0.10 |
| weight-1/groomer-carlos-1 | 9 | 766 | 49 | 0 | 0 | 0 | 0 | 0.08 |
| weight-1/groomer-carlos-2 | 6 | 567 | 30 | 7 | 0 | 8 | 0 | 0.09 |
| weight-1/groomer-carlos-3 | 8 | 634 | 38 | 2 | 0 | 3 | 0 | 0.05 |

## Highest cognitive complexity

| run | function | cognitive | nesting | lines |
|---|---|---|---|---|
| weight-1/groomer-carlos-2 | internal/groomer/seed.go `Salon.seedComingBookings` | 17 | 3 | 30 |
| accept-1/groomer-carlos-2 | cmd/groomer/main.go `main` | 12 | 1 | 108 |
| cost-1/groomer-carlos-2 | internal/groomer/owner_hours.go `weekFrom` | 12 | 3 | 26 |
| accept-1/groomer-carlos-2 | internal/groomer/app.go `Router` | 11 | 2 | 77 |
| 1/groomer-carlos-3 | internal/groomer/seed.go `seedSalon` | 11 | 2 | 32 |

## Lowest cognitive complexity

Ties go to the longer function.

| run | function | cognitive | nesting | lines |
|---|---|---|---|---|
| weight-1/groomer-carlos-2 | internal/groomer/routes.go `Routes` | 0 | 0 | 33 |
| 1/groomer-carlos-2 | internal/groomer/app.go `app.mount` | 0 | 0 | 26 |
| accept-1/groomer-carlos-3 | internal/groomer/routes.go `Routes` | 0 | 0 | 25 |
| cost-1/groomer-carlos-2 | internal/groomer/routes.go `Routes` | 0 | 0 | 24 |
| accept-1/groomer-carlos-3 | internal/groomer/seed.go `seedMenu` | 0 | 0 | 22 |

## Checkpoint 1: do the counts rank the functions as a reader would?

I read the ten functions above in the kept runs (read only).

Highest cognitive complexity:

- `Salon.seedComingBookings` (17): hard. Two nested loops, an index
  (`next`) that moves in only one of three branches, a second bound
  checked inside the loop after it was checked in the header, and three
  error returns. I had to trace `next` by hand. The hardest of the ten.
- `weekFrom` (12): hard in a different way. An inner loop runs the same
  body for `"opens_"` and `"closes_"` and then tests the name again to
  decide which field to set, so the reader keeps the loop and the branch
  in mind together. A clumsy shape for two fields.
- `seedSalon` (11): not hard. Three flat loops, each one create and one
  error check. Easy to follow, though a little long.
- `Router` (11, 77 lines) and `main` (12, 108 lines): sequences of guard
  clauses, each one `if x == nil { return err }`, with long comments
  between them. Nothing needs holding in the head at once; what costs
  the reader is the length and the comment weight, which cognitive
  complexity does not see. The `lines` count does.

Lowest cognitive complexity: `Routes` (three runs), `app.mount` and
`seedMenu`. All five are flat declarative lists, a route per line or a
service per entry, and are the easiest to follow in the set. One
reservation: `seedMenu` has string concatenation across lines that
makes it wordy, not hard.

Verdict: the bottom of the ranking is right, the top is half right. The
two I found hardest are first and third, and `seedSalon`, `Router` and
`main` are on the list for being long, not for being tangled. So
cognitive complexity finds the real knots but rates long straight-line
code the same as a knot, which is why `lines` and the over-limit counts
should be read next to it and not folded into it.

Two things about the numbers themselves. The spread is narrow: median
cognitive complexity is 1 in eleven of twelve runs and 2 in one, the 90th
percentile 4 to 6, so across runs the difference is mostly in the
offence counts (wide lines, one-letter names, long functions), which run
from none (round 1 run 1, weight-1 run 1) to 15 names. And duplication
is zero in every run at dupl's threshold of 100 nodes; the standalone
`dupl` agrees (its only clones are in generated files, which are not
authored), so the zero is a real finding, not a gap.

(Note added 2026-11-25: when this checkpoint was written, the table's
offence counts, files, code lines and comment share covered the tests
too. The table above is now the app's code alone, with the tests in
their own table, so the offences quoted here, up to 15 names, are app
and tests together; the app alone runs from none to 8 names, with the
same runs at none. The function counts this checkpoint judges did not
change.)
