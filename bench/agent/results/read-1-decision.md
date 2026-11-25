# Round read-1: the gopls decision

Kept by hand. `results/read-1.md` is written by `bench report` and `bench rescore` from `results/read-1.json`, and points here; neither command touches this file.

gopls is **not accepted**.

The bar (design, "Accepting gopls"; Global Constraints): `carlos-gopls` must be better than `carlos` on at least two of cost, turns, failed builds and comprehension effort, on the held-out briefs, with the two arms' ranges not overlapping, and not worse beyond the range on any other measure, readability counts included. An arm with more missing functionality than the other on a held-out brief cannot be accepted. Anything else on a measure is inconclusive and never a win.

Each cell is the median over three runs, with the range (lowest to highest) in brackets. Cost, turns and failed builds are build plus follow-up, since the scored app is the one left after both. `bench rescore -round read-1` was run first and changed nothing.

| Measure | Brief | carlos | carlos-gopls | carlos-gopls is |
|---|---|---|---|---|
| cost ($) | lending | 9.90 (9.79–13.25) | 9.82 (9.13–12.29) | inconclusive |
| cost ($) | noticeboard | 10.19 (9.04–10.35) | 9.86 (8.62–11.71) | inconclusive |
| turns | lending | 157 (147–178) | 159 (147–177) | inconclusive |
| turns | noticeboard | 143 (137–148) | 136 (134–170) | inconclusive |
| failed builds and test runs | lending | 14 (14–24) | 14 (13–17) | inconclusive |
| failed builds and test runs | noticeboard | 13 (13–17) | 12 (11–16) | inconclusive |
| comprehension effort | lending | not yet read | not yet read | inconclusive until read |
| comprehension effort | noticeboard | not yet read | not yet read | inconclusive until read |

Cost, turns and failed builds overlap on both held-out briefs, so at most one of the four measures, comprehension effort, could still come out better. Under the reading that the bar asks for two of the four measures (cost, turns, failed builds and comprehension effort, each judged on the held-out briefs), neither the comprehension result nor missing functionality can change the verdict: comprehension effort is one measure, and missing functionality can only stop an arm being accepted, never make it so. (`groomer`, the dev brief, is not part of the decision; it overlaps on all three as well.)

**Comprehension, missing functionality and the noise gate.** Not yet measured. `bench keys` on read-1 failed on every app: the bench token had reached its weekly limit (API error 429, "resets Nov 26, 8pm (Europe/Dublin)"). Without keys there are no reads, so missing functionality per arm and the noise gate's verdicts are still to come. Until the reads pass the gate, every comprehension measure counts as inconclusive.

**Readability counts** (`results/readability-read-1.md`), held-out briefs, per app, as median (range):

| Count | Brief | carlos | carlos-gopls | carlos-gopls is |
|---|---|---|---|---|
| cognitive complexity, 90th percentile | lending | 4 (4–5) | 5 (3–5) | inconclusive |
| cognitive complexity, 90th percentile | noticeboard | 3 (3–4) | 3 (3–4) | inconclusive |
| cognitive complexity, total | lending | 160 (129–179) | 174 (150–188) | inconclusive |
| cognitive complexity, total | noticeboard | 102 (95–124) | 107 (105–123) | inconclusive |
| highest cognitive complexity | lending | 8 (7–9) | 9 (7–10) | inconclusive |
| highest cognitive complexity | noticeboard | 6 (5–8) | 8 (7–8) | inconclusive |
| nesting, 90th percentile | lending | 2 (1–2) | 2 (2–2) | inconclusive |
| nesting, 90th percentile | noticeboard | 1 (1–1) | 1 (1–1) | the same |
| function lines, 90th percentile | lending | 22 (20–22) | 25 (21–26) | inconclusive |
| function lines, 90th percentile | noticeboard | 22 (18–23) | 22 (20–22) | inconclusive |
| far short names | lending | 2 (1–3) | 1 (1–4) | inconclusive |
| far short names | noticeboard | 1 (1–2) | 1 (0–3) | inconclusive |
| app code lines | lending | 1218 (1104–1347) | 1277 (1123–1354) | inconclusive |
| app code lines | noticeboard | 1073 (933–1146) | 1044 (996–1228) | inconclusive |
| test code lines | lending | 545 (442–611) | 518 (505–575) | inconclusive |
| test code lines | noticeboard | 449 (434–596) | 483 (469–553) | inconclusive |

Changed on 2026-11-25: this table had one row, "authored code lines", which counted the app's code and its tests together (lending 1763 (1546–1958) against 1782 (1698–1872), noticeboard 1595 (1367–1669) against 1513 (1479–1781)). `bench score` now totals them apart, so the table has app code lines and test code lines instead. Both still overlap on both briefs, so the verdict on them is unchanged. No other row changed: the function counts never included tests.

No app in either arm has a duplicated line, a line over a limit, a `codelimits` offence or an interface with exactly one implementation, so those are the same (0) in every run. carlos-gopls is worse beyond the range on no readability count.

**Taste** is untrusted for read-1 (ruling R31): neither Astra (46%) nor the tuned composite (69%) reached 80% agreement with Paul (`results/agreement-2026-11-22.md`), so no cross-arm pairs were judged and taste plays no part in this decision.

**What gopls did.** Agents called gopls 2 to 7 times a run, out of 128 to 178 turns, mostly `go_workspace`, `go_diagnostics` and `go_vulncheck`, one or two calls each. Go compile mistakes on the held-out briefs were 16 without gopls and 15 with it. Those figures come from `bench waste` run once per arm, `go run ./cmd/bench waste -rounds read-1 -briefs lending,noticeboard -arms carlos -out heldout-carlos` and the same with `-arms carlos-gopls -out heldout-carlos-gopls`, whose reports are `results/waste-heldout-carlos.md` and `results/waste-heldout-carlos-gopls.md`; `results/waste-heldout.md` is both arms together (31).

**Verdict:** not accepted. carlos-gopls is better than carlos on none of cost, turns and failed builds on either held-out brief, so, under the reading "two of the four measures", it cannot reach two of four, whatever comprehension effort shows. It is worse beyond the range on nothing measured so far.

**Was gopls loaded?** In 7 of the 9 gopls builds, gopls was restarted in the app's module and kept it, and every follow-up and mapping session started in the module. In two builds, groomer-carlos-gopls-1 (dev brief) and lending-carlos-gopls-2 (held-out), the agent moved its module from app/<name> up to app/ after gopls had loaded it, so for the rest of those builds gopls answered "no package metadata". The runner now rejects such a run, and the wrapper follows a moved module. The verdict does not depend on lending: noticeboard, whose three gopls builds were all valid, overlaps on cost, turns and failed builds on its own, so gopls cannot reach two of the four measures.
