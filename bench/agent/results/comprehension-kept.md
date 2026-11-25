# Comprehension, kept

Read by `bench read` with gpt-6-astra at high effort, 3 reads of each question per app, budget 180000 tokens (input, cached included, plus output; a reading over it is wrong). Accuracy is right answers over all the brief's questions: an unsettled key's question is not read and counts as not right. Median tokens and commands are over right answers.

## Runs

| Round | Run | Rastrillo | Read | Accuracy | Median tokens | Median commands | Unsettled | Not built |
|---|---|---|---|---|---|---|---|---|
| 1 | groomer-carlos-1 | v0.30.0-bench.gb79c48634fea | 1 | 0.90 | 90768 | 3.0 | 0 | 0 |
| 1 | groomer-carlos-1 | v0.30.0-bench.gb79c48634fea | 2 | 1.00 | 78218 | 3.0 | 0 | 0 |
| 1 | groomer-carlos-1 | v0.30.0-bench.gb79c48634fea | 3 | 1.00 | 71300 | 2.5 | 0 | 0 |
| 1 | groomer-carlos-2 | v0.30.0-bench.gb79c48634fea | 1 | 1.00 | 89622 | 3.5 | 0 | 0 |
| 1 | groomer-carlos-2 | v0.30.0-bench.gb79c48634fea | 2 | 1.00 | 92109 | 3.0 | 0 | 0 |
| 1 | groomer-carlos-2 | v0.30.0-bench.gb79c48634fea | 3 | 1.00 | 64220 | 2.0 | 0 | 0 |
| 1 | groomer-carlos-3 | v0.30.0-bench.gb79c48634fea | 1 | 0.90 | 81342 | 3.0 | 0 | 0 |
| 1 | groomer-carlos-3 | v0.30.0-bench.gb79c48634fea | 2 | 0.90 | 95674 | 3.0 | 0 | 0 |
| 1 | groomer-carlos-3 | v0.30.0-bench.gb79c48634fea | 3 | 1.00 | 94800 | 3.5 | 0 | 0 |
| accept-1 | groomer-carlos-1 | v0.30.0-bench.gf99c96d2b252 | 1 | 1.00 | 89912 | 3.0 | 0 | 0 |
| accept-1 | groomer-carlos-1 | v0.30.0-bench.gf99c96d2b252 | 2 | 1.00 | 89018 | 3.5 | 0 | 0 |
| accept-1 | groomer-carlos-1 | v0.30.0-bench.gf99c96d2b252 | 3 | 0.90 | 68273 | 2.0 | 0 | 0 |
| accept-1 | groomer-carlos-2 | v0.30.0-bench.gf99c96d2b252 | 1 | 0.80 | 94502 | 3.0 | 1 | 0 |
| accept-1 | groomer-carlos-2 | v0.30.0-bench.gf99c96d2b252 | 2 | 0.90 | 98157 | 3.0 | 1 | 0 |
| accept-1 | groomer-carlos-2 | v0.30.0-bench.gf99c96d2b252 | 3 | 0.90 | 98648 | 3.0 | 1 | 0 |
| accept-1 | groomer-carlos-3 | v0.30.0-bench.gf99c96d2b252 | 1 | 0.90 | 93500 | 3.0 | 0 | 0 |
| accept-1 | groomer-carlos-3 | v0.30.0-bench.gf99c96d2b252 | 2 | 0.90 | 87788 | 3.0 | 0 | 0 |
| accept-1 | groomer-carlos-3 | v0.30.0-bench.gf99c96d2b252 | 3 | 1.00 | 84615 | 3.0 | 0 | 0 |
| cost-1 | groomer-carlos-1 | v0.30.0-bench.gc89896cd6ebd | 1 | 1.00 | 99252 | 3.0 | 0 | 0 |
| cost-1 | groomer-carlos-1 | v0.30.0-bench.gc89896cd6ebd | 2 | 1.00 | 94212 | 3.5 | 0 | 0 |
| cost-1 | groomer-carlos-1 | v0.30.0-bench.gc89896cd6ebd | 3 | 0.90 | 89864 | 3.0 | 0 | 0 |
| cost-1 | groomer-carlos-2 | v0.30.0-bench.gc89896cd6ebd | 1 | 0.90 | 94437 | 3.0 | 1 | 0 |
| cost-1 | groomer-carlos-2 | v0.30.0-bench.gc89896cd6ebd | 2 | 0.90 | 96873 | 3.0 | 1 | 0 |
| cost-1 | groomer-carlos-2 | v0.30.0-bench.gc89896cd6ebd | 3 | 0.80 | 94818 | 3.0 | 1 | 0 |
| cost-1 | groomer-carlos-3 | v0.30.0-bench.gc89896cd6ebd | 1 | 0.90 | 88719 | 3.0 | 0 | 0 |
| cost-1 | groomer-carlos-3 | v0.30.0-bench.gc89896cd6ebd | 2 | 0.90 | 87270 | 3.0 | 0 | 0 |
| cost-1 | groomer-carlos-3 | v0.30.0-bench.gc89896cd6ebd | 3 | 0.90 | 94719 | 3.0 | 0 | 0 |
| weight-1 | groomer-carlos-1 | v0.30.0-bench.ga386b11d1cc8 | 1 | 1.00 | 91584 | 3.0 | 0 | 0 |
| weight-1 | groomer-carlos-1 | v0.30.0-bench.ga386b11d1cc8 | 2 | 1.00 | 92200 | 3.0 | 0 | 0 |
| weight-1 | groomer-carlos-1 | v0.30.0-bench.ga386b11d1cc8 | 3 | 1.00 | 87180 | 3.0 | 0 | 0 |
| weight-1 | groomer-carlos-2 | v0.30.0-bench.ga386b11d1cc8 | 1 | 1.00 | 84282 | 3.0 | 0 | 0 |
| weight-1 | groomer-carlos-2 | v0.30.0-bench.ga386b11d1cc8 | 2 | 0.90 | 82016 | 3.0 | 0 | 0 |
| weight-1 | groomer-carlos-2 | v0.30.0-bench.ga386b11d1cc8 | 3 | 1.00 | 91576 | 3.0 | 0 | 0 |
| weight-1 | groomer-carlos-3 | v0.30.0-bench.ga386b11d1cc8 | 1 | 0.90 | 88363 | 3.0 | 0 | 0 |
| weight-1 | groomer-carlos-3 | v0.30.0-bench.ga386b11d1cc8 | 2 | 1.00 | 91900 | 4.0 | 0 | 0 |
| weight-1 | groomer-carlos-3 | v0.30.0-bench.ga386b11d1cc8 | 3 | 1.00 | 88634 | 3.0 | 0 | 0 |

## Answers right, by question

Right reads over reads; - where the key is unsettled.

| Question | 1 carlos-1 | 1 carlos-2 | 1 carlos-3 | accept-1 carlos-1 | accept-1 carlos-2 | accept-1 carlos-3 | cost-1 carlos-1 | cost-1 carlos-2 | cost-1 carlos-3 | weight-1 carlos-1 | weight-1 carlos-2 | weight-1 carlos-3 |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| empty-dog | 2/3 | 3/3 | 3/3 | 2/3 | 2/3 | 2/3 | 2/3 | 2/3 | 0/3 | 3/3 | 2/3 | 3/3 |
| empty-phone | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 |
| booked-status | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 |
| after-booking | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 |
| owner-signed-out | 3/3 | 3/3 | 1/3 | 3/3 | 3/3 | 2/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 2/3 |
| demo-username | 3/3 | 3/3 | 3/3 | 3/3 | - | 3/3 | 3/3 | - | 3/3 | 3/3 | 3/3 | 3/3 |
| signin-path | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 |
| dog-field | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 |
| same-time | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 |
| remove-service | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 |

## Effort between runs

Only runs of one brief on one Rastrillo version are compared, read against read, on the questions both answered right.

| Rastrillo | Runs | Read | Both right | Median tokens | Median commands |
|---|---|---|---|---|---|
| v0.30.0-bench.gb79c48634fea | 1/groomer-carlos-1 vs groomer-carlos-2 | 1 | 9 | 90768 vs 86359 | 3.0 vs 3.0 |
| v0.30.0-bench.gb79c48634fea | 1/groomer-carlos-1 vs groomer-carlos-2 | 2 | 10 | 78218 vs 92109 | 3.0 vs 3.0 |
| v0.30.0-bench.gb79c48634fea | 1/groomer-carlos-1 vs groomer-carlos-2 | 3 | 10 | 71300 vs 64220 | 2.5 vs 2.0 |
| v0.30.0-bench.gb79c48634fea | 1/groomer-carlos-1 vs groomer-carlos-3 | 1 | 8 | 76580 vs 71722 | 2.5 vs 2.5 |
| v0.30.0-bench.gb79c48634fea | 1/groomer-carlos-1 vs groomer-carlos-3 | 2 | 9 | 64858 vs 95674 | 3.0 vs 3.0 |
| v0.30.0-bench.gb79c48634fea | 1/groomer-carlos-1 vs groomer-carlos-3 | 3 | 10 | 71300 vs 94800 | 2.5 vs 3.5 |
| v0.30.0-bench.gb79c48634fea | 1/groomer-carlos-2 vs groomer-carlos-3 | 1 | 9 | 86359 vs 81342 | 3.0 vs 3.0 |
| v0.30.0-bench.gb79c48634fea | 1/groomer-carlos-2 vs groomer-carlos-3 | 2 | 9 | 90178 vs 95674 | 3.0 vs 3.0 |
| v0.30.0-bench.gb79c48634fea | 1/groomer-carlos-2 vs groomer-carlos-3 | 3 | 10 | 64220 vs 94800 | 2.0 vs 3.5 |
| v0.30.0-bench.gf99c96d2b252 | accept-1/groomer-carlos-1 vs groomer-carlos-2 | 1 | 8 | 89912 vs 94502 | 3.0 vs 3.0 |
| v0.30.0-bench.gf99c96d2b252 | accept-1/groomer-carlos-1 vs groomer-carlos-2 | 2 | 9 | 94557 vs 98157 | 4.0 vs 3.0 |
| v0.30.0-bench.gf99c96d2b252 | accept-1/groomer-carlos-1 vs groomer-carlos-2 | 3 | 8 | 69576 vs 97299 | 2.5 vs 3.0 |
| v0.30.0-bench.gf99c96d2b252 | accept-1/groomer-carlos-1 vs groomer-carlos-3 | 1 | 9 | 85154 vs 93500 | 3.0 vs 3.0 |
| v0.30.0-bench.gf99c96d2b252 | accept-1/groomer-carlos-1 vs groomer-carlos-3 | 2 | 9 | 83478 vs 87788 | 3.0 vs 3.0 |
| v0.30.0-bench.gf99c96d2b252 | accept-1/groomer-carlos-1 vs groomer-carlos-3 | 3 | 9 | 68273 vs 77786 | 2.0 vs 3.0 |
| v0.30.0-bench.gf99c96d2b252 | accept-1/groomer-carlos-2 vs groomer-carlos-3 | 1 | 7 | 90154 vs 93500 | 3.0 vs 3.0 |
| v0.30.0-bench.gf99c96d2b252 | accept-1/groomer-carlos-2 vs groomer-carlos-3 | 2 | 8 | 95688 vs 78588 | 3.0 vs 3.0 |
| v0.30.0-bench.gf99c96d2b252 | accept-1/groomer-carlos-2 vs groomer-carlos-3 | 3 | 9 | 98648 vs 91444 | 3.0 vs 3.0 |
| v0.30.0-bench.gc89896cd6ebd | cost-1/groomer-carlos-1 vs groomer-carlos-2 | 1 | 9 | 99944 vs 94437 | 3.0 vs 3.0 |
| v0.30.0-bench.gc89896cd6ebd | cost-1/groomer-carlos-1 vs groomer-carlos-2 | 2 | 9 | 94886 vs 96873 | 4.0 vs 3.0 |
| v0.30.0-bench.gc89896cd6ebd | cost-1/groomer-carlos-1 vs groomer-carlos-2 | 3 | 8 | 92043 vs 94818 | 3.0 vs 3.0 |
| v0.30.0-bench.gc89896cd6ebd | cost-1/groomer-carlos-1 vs groomer-carlos-3 | 1 | 9 | 98560 vs 88719 | 3.0 vs 3.0 |
| v0.30.0-bench.gc89896cd6ebd | cost-1/groomer-carlos-1 vs groomer-carlos-3 | 2 | 9 | 93538 vs 87270 | 3.0 vs 3.0 |
| v0.30.0-bench.gc89896cd6ebd | cost-1/groomer-carlos-1 vs groomer-carlos-3 | 3 | 9 | 89864 vs 94719 | 3.0 vs 3.0 |
| v0.30.0-bench.gc89896cd6ebd | cost-1/groomer-carlos-2 vs groomer-carlos-3 | 1 | 8 | 93883 vs 91304 | 3.0 vs 3.0 |
| v0.30.0-bench.gc89896cd6ebd | cost-1/groomer-carlos-2 vs groomer-carlos-3 | 2 | 8 | 81450 vs 89437 | 3.0 vs 3.0 |
| v0.30.0-bench.gc89896cd6ebd | cost-1/groomer-carlos-2 vs groomer-carlos-3 | 3 | 8 | 94818 vs 94970 | 3.0 vs 3.0 |
| v0.30.0-bench.ga386b11d1cc8 | weight-1/groomer-carlos-1 vs groomer-carlos-2 | 1 | 10 | 91584 vs 84282 | 3.0 vs 3.0 |
| v0.30.0-bench.ga386b11d1cc8 | weight-1/groomer-carlos-1 vs groomer-carlos-2 | 2 | 9 | 92117 vs 82016 | 3.0 vs 3.0 |
| v0.30.0-bench.ga386b11d1cc8 | weight-1/groomer-carlos-1 vs groomer-carlos-2 | 3 | 10 | 87180 vs 91576 | 3.0 vs 3.0 |
| v0.30.0-bench.ga386b11d1cc8 | weight-1/groomer-carlos-1 vs groomer-carlos-3 | 1 | 9 | 87293 vs 88363 | 3.0 vs 3.0 |
| v0.30.0-bench.ga386b11d1cc8 | weight-1/groomer-carlos-1 vs groomer-carlos-3 | 2 | 10 | 92200 vs 91900 | 3.0 vs 4.0 |
| v0.30.0-bench.ga386b11d1cc8 | weight-1/groomer-carlos-1 vs groomer-carlos-3 | 3 | 10 | 87180 vs 88634 | 3.0 vs 3.0 |
| v0.30.0-bench.ga386b11d1cc8 | weight-1/groomer-carlos-2 vs groomer-carlos-3 | 1 | 9 | 81512 vs 88363 | 3.0 vs 3.0 |
| v0.30.0-bench.ga386b11d1cc8 | weight-1/groomer-carlos-2 vs groomer-carlos-3 | 2 | 9 | 82016 vs 91809 | 3.0 vs 4.0 |
| v0.30.0-bench.ga386b11d1cc8 | weight-1/groomer-carlos-2 vs groomer-carlos-3 | 3 | 10 | 91576 vs 88634 | 3.0 vs 3.0 |

## Noise

For each measure, the median absolute difference between two reads of one app must be at most half the median absolute difference between runs of one brief (on one Rastrillo version). A measure that fails is inconclusive for every decision until it passes, and so is one whose runs do not differ at all. Accuracy here grades an answer over the budget on what it says, so token noise cannot pass for a difference in understanding. Effort differences are on the questions both reads, or both runs, got right.

### Between the reads of each app

| Round | Run | Accuracy | Tokens | Commands |
|---|---|---|---|---|
| 1 | groomer-carlos-1 | 0.00 (3) | 24671 (3) | 0.50 (3) |
| 1 | groomer-carlos-2 | 0.00 (3) | 25402 (3) | 1.00 (3) |
| 1 | groomer-carlos-3 | 0.00 (3) | 9014 (3) | 0.00 (3) |
| accept-1 | groomer-carlos-1 | 0.00 (3) | 15205 (3) | 1.00 (3) |
| accept-1 | groomer-carlos-2 | 0.00 (3) | 1186 (3) | 0.00 (3) |
| accept-1 | groomer-carlos-3 | 0.00 (3) | 14255 (3) | 0.00 (3) |
| cost-1 | groomer-carlos-1 | 0.00 (3) | 5040 (3) | 0.00 (3) |
| cost-1 | groomer-carlos-2 | 0.00 (3) | 2436 (3) | 0.00 (3) |
| cost-1 | groomer-carlos-3 | 0.00 (3) | 6000 (3) | 0.00 (3) |
| weight-1 | groomer-carlos-1 | 0.00 (3) | 4404 (3) | 0.00 (3) |
| weight-1 | groomer-carlos-2 | 0.00 (3) | 7293 (3) | 0.00 (3) |
| weight-1 | groomer-carlos-3 | 0.00 (3) | 3266 (3) | 1.00 (3) |

### Between the runs of each brief

| Brief | Accuracy | Tokens | Commands |
|---|---|---|---|
| groomer | 0.00 (36) | 5502 (36) | 0.00 (36) |

### The gate

| Measure | Between reads | Between runs | Pairs (reads, runs) | Verdict |
|---|---|---|---|---|
| accuracy | 0.00 | 0.00 | 36, 36 | cannot separate runs: inconclusive |
| tokens | 5659 | 5502 | 36, 36 | fails: inconclusive |
| commands | 0.00 | 0.00 | 36, 36 | cannot separate runs: inconclusive |

## Budget

95th percentile of tokens on the 354 answers right before any budget: 174249, so -budget 180000 (rounded up to the next 10,000). Right answers this run's budget of 180000 made wrong: 14.

## Checkpoint 2: comprehension (2026-11-22)

**Verdict: on groomer, accuracy, tokens and commands are all inconclusive for later decisions.** None of them tells these twelve apps apart more clearly than two reads of one app differ.

**What was read.** The twelve kept groomer apps, every question with a settled key, by Astra at high effort. Two reads first (236 readings, one of them the smoke read), then, because a measure failed the gate, a third read once, as the plan says (118 more). 354 readings in all, none failed, all read at `-budget 200000`. Every one of the 354 answers matches its key.

**The noise gate.** A measure passes when the median difference between two reads of one app is at most half the median difference between runs of one brief on one Rastrillo version. When runs do not differ at all, the verdict is "cannot separate runs", which is inconclusive, never a pass. Accuracy is judged without the budget (ruling R19): an answer over the budget is graded on what it says, so token noise, which fails its own gate, cannot pass for a difference in understanding. The accuracy reported in the tables still applies the budget.

The gates recorded in the JSON, three reads graded at `-budget 180000`:

| Measure | Between reads | Between runs | Verdict |
|---|---|---|---|
| accuracy | 0.00 | 0.00 | cannot separate runs |
| tokens | 5659 | 5502 | fails |
| commands | 0.00 | 0.00 | cannot separate runs |

How the gate went while reading, at the reading budget of 200,000:

| Measure | 2 reads: reads / runs | 2 reads | 3 reads: reads / runs | 3 reads |
|---|---|---|---|---|
| accuracy | 0.00 / 0.00 | cannot separate runs | 0.00 / 0.00 | cannot separate runs |
| tokens | 1942 / 4369 | passes | 4712 / 4723 | fails |
| commands | 0.25 / 0.00 | cannot separate runs | 0.00 / 0.00 | cannot separate runs |

Commands failing at two reads led to the single third read. At three reads, tokens fail: two reads of one app differ as much as two apps do. Every one of the 354 answers is right on its content, so accuracy cannot differ between runs. Commands do not differ between runs by median, though reads of one app do (six of the twelve apps differ by 0.5 to 1 command between reads). The apps are too alike, and Astra reads each in about three commands, so whether these measures separate anything has to be shown on briefs whose apps differ more.

**Settings chosen.** `-reads 3`. `-budget 180000`: the 95th percentile of tokens on the 354 right answers is 174,249, rounded up to the next 10,000. The report above is graded at 180,000 from the saved readings, with no new reads. At that budget 14 right answers are over (10 `empty-dog`, 4 `owner-signed-out`) and the reported accuracy counts them wrong.

**Accuracy and effort per run** at `-budget 180000`, the mean over three reads (each read's median over its right answers):

| Round | Run | Rastrillo | Accuracy | Median tokens | Median commands |
|---|---|---|---|---|---|
| 1 | groomer-carlos-1 | b79c48634fea | 0.97 | 80095 | 2.8 |
| 1 | groomer-carlos-2 | b79c48634fea | 1.00 | 81984 | 2.8 |
| 1 | groomer-carlos-3 | b79c48634fea | 0.93 | 90605 | 3.2 |
| accept-1 | groomer-carlos-1 | f99c96d2b252 | 0.97 | 82401 | 2.8 |
| accept-1 | groomer-carlos-2 | f99c96d2b252 | 0.87 | 97102 | 3.0 |
| accept-1 | groomer-carlos-3 | f99c96d2b252 | 0.93 | 88634 | 3.0 |
| cost-1 | groomer-carlos-1 | c89896cd6ebd | 0.97 | 94443 | 3.2 |
| cost-1 | groomer-carlos-2 | c89896cd6ebd | 0.87 | 95376 | 3.0 |
| cost-1 | groomer-carlos-3 | c89896cd6ebd | 0.90 | 90236 | 3.0 |
| weight-1 | groomer-carlos-1 | a386b11d1cc8 | 1.00 | 90322 | 3.0 |
| weight-1 | groomer-carlos-2 | a386b11d1cc8 | 0.97 | 85958 | 3.0 |
| weight-1 | groomer-carlos-3 | a386b11d1cc8 | 0.97 | 89632 | 3.3 |

accept-1/2 and cost-1/2 cannot score above 0.90: their `demo-username` key is unsettled, and the denominator stays at ten. Every other point lost is a reading over the budget.

**What it spent.** 354 Astra readings: 34.0 million tokens (input, cached included, plus output) and 1,110 commands, on the ChatGPT login, about 35 minutes of wall time at `-j 3`. Each model call costs about 30,000 tokens of fixed context, so a reading's tokens are mostly its number of steps.
