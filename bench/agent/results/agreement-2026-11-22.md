# agreement-2026-11-22

A layer is trusted when it picks Paul's side on at least 80% of the check pairs he gave a side, and at least 12 check pairs have one. Spares count as check pairs. "No difference" or "not the same job" against Paul's side is a disagreement; pairs Paul gave no side are not counted. A layer under 80% is revised once and checked on new pairs: each attempt is listed below.

## Attempt 1: results/pairs-2026-11-22.json, tuned weights

<!-- attempt results/pairs-2026-11-22.json results/calibration-2026-11-22.json tuned weights -->

Pairs: `results/pairs-2026-11-22.json`, seed 20261122. Paul's verdicts: `results/calibration-2026-11-22.json`. Astra's picks: `/home/paulca/amadan.net/rastrillo/rastrillo-readability-measurement/bench/agent/results/astra-picks-2026-11-22.json` (gpt-6-astra, high effort, instructions d8228569f633bb9b), made blind, before or without Paul's verdicts.

13 check pairs have a side.

### Paul's verdicts

| Set | Left | Right | No difference | Not the same job | Not judged |
|---|---|---|---|---|---|
| tune | 5 | 5 | 3 | 2 | 0 |
| check | 2 | 11 | 2 | 0 | 0 |
| spare | 0 | 0 | 0 | 0 | 10 |

### Tuned weights

Cognitive 1, Nesting 1, Over limit 1, Duplicated 1, Far short names 1. On the 10 tune pairs Paul gave a side, they agree on 7; even weights on 7. Each weight is 0, 1, 2 or 3; of weights that agree as often, the nearest even wins, then the one with fewer zeros, then the larger sum.

### Check pairs

| Layer | Agreed | Share | Trusted | Each run left out |
|---|---|---|---|---|
| Composite, tuned weights | 9 of 13 | 69% | no | 60% (6 of 10, without 1/groomer-carlos-1) to 88% (7 of 8, without cost-1/groomer-carlos-1) |
| Astra | 6 of 13 | 46% | no | 30% (3 of 10, without weight-1/groomer-carlos-2) to 62% (5 of 8, without 1/groomer-carlos-2) |
| Composite, even weights (for comparison) | 9 of 13 | 69% | - | 60% (6 of 10, without 1/groomer-carlos-1) to 88% (7 of 8, without cost-1/groomer-carlos-1) |

"Each run left out" is the lowest and highest agreement with the pairs showing any one run left out, each run in turn.

### Each judged pair

| Pair | Set | Left | Right | Paul | Composite | Astra |
|---|---|---|---|---|---|---|
| 1 | tune | weight-1/groomer-carlos-1 | 1/groomer-carlos-3 | same | same | right |
| 2 | tune | cost-1/groomer-carlos-3 | accept-1/groomer-carlos-1 | same | right | left |
| 3 | tune | weight-1/groomer-carlos-1 | weight-1/groomer-carlos-3 | left | left | left |
| 4 | tune | cost-1/groomer-carlos-2 | cost-1/groomer-carlos-3 | right | right | left |
| 5 | tune | weight-1/groomer-carlos-1 | cost-1/groomer-carlos-3 | right | right | right |
| 6 | tune | cost-1/groomer-carlos-3 | cost-1/groomer-carlos-2 | right | left | left |
| 7 | tune | 1/groomer-carlos-3 | cost-1/groomer-carlos-2 | left | left | left |
| 8 | tune | cost-1/groomer-carlos-2 | accept-1/groomer-carlos-1 | same | left | not-same-job |
| 9 | tune | weight-1/groomer-carlos-3 | cost-1/groomer-carlos-3 | right | left | left |
| 10 | tune | weight-1/groomer-carlos-3 | cost-1/groomer-carlos-3 | right | right | right |
| 11 | tune | weight-1/groomer-carlos-1 | cost-1/groomer-carlos-3 | not-same-job | left | not-same-job |
| 12 | tune | 1/groomer-carlos-3 | accept-1/groomer-carlos-1 | left | left | not-same-job |
| 13 | tune | weight-1/groomer-carlos-1 | 1/groomer-carlos-3 | left | right | left |
| 14 | tune | cost-1/groomer-carlos-3 | weight-1/groomer-carlos-1 | left | left | not-same-job |
| 15 | tune | cost-1/groomer-carlos-3 | weight-1/groomer-carlos-3 | not-same-job | right | not-same-job |
| 16 | check | cost-1/groomer-carlos-1 | accept-1/groomer-carlos-2 | right | same | left |
| 17 | check | 1/groomer-carlos-1 | accept-1/groomer-carlos-3 | right | right | left |
| 18 | check | accept-1/groomer-carlos-2 | weight-1/groomer-carlos-2 | right | right | right |
| 19 | check | cost-1/groomer-carlos-1 | 1/groomer-carlos-1 | right | right | right |
| 20 | check | 1/groomer-carlos-2 | accept-1/groomer-carlos-3 | right | left | left |
| 21 | check | accept-1/groomer-carlos-3 | 1/groomer-carlos-2 | right | right | right |
| 22 | check | 1/groomer-carlos-1 | weight-1/groomer-carlos-2 | right | right | right |
| 23 | check | 1/groomer-carlos-2 | accept-1/groomer-carlos-2 | left | left | not-same-job |
| 24 | check | cost-1/groomer-carlos-1 | accept-1/groomer-carlos-2 | left | right | right |
| 25 | check | cost-1/groomer-carlos-1 | accept-1/groomer-carlos-2 | same | left | not-same-job |
| 26 | check | accept-1/groomer-carlos-2 | 1/groomer-carlos-2 | right | right | not-same-job |
| 27 | check | 1/groomer-carlos-2 | cost-1/groomer-carlos-1 | right | left | left |
| 28 | check | 1/groomer-carlos-1 | 1/groomer-carlos-2 | same | left | left |
| 29 | check | accept-1/groomer-carlos-3 | weight-1/groomer-carlos-2 | right | right | right |
| 30 | check | accept-1/groomer-carlos-2 | cost-1/groomer-carlos-1 | right | right | right |
