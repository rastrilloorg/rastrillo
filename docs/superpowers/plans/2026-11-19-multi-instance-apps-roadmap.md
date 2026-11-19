# Multi-instance apps: the roadmap of plans

**Spec:** `docs/superpowers/specs/2026-11-19-multi-instance-apps-design.md`
(approved 2026-11-19). It continues the user-models roadmap
(`carlos/pegamento`, branch `design-user-models`), whose Plans 1, 2, 4
and 5 have landed.

The spec covers several repos and pieces, so it's built as a sequence
of plans. Each plan produces working, tested software on its own and
lands through its own repo's gate. Each one is written when the plans
it depends on land, so it argues from what actually shipped. Only
Plan 1 is written now.

| # | Plan | Repo | Depends on | Delivers |
|---|---|---|---|---|
| 0 | Scaffold shapes | rastrillo | none | `--scaffold bare\|single\|accounts`, with `home` and `hub` reserved. Already planned: `docs/superpowers/plans/2026-10-25-scaffold-shapes.md` on branch `scaffold-shapes` |
| 1 | Instance handoff and roster | rastrillo | none | `instance` (rooted or standalone, from the environment), `handoff` (enter and consume, UM Plan 3), `roster` (signed snapshots and the receiver), `handofftest` |
| 2 | idear's roster store | idear | 1 released | idear implements `roster.Store`. Its people pages are off while the instance is rooted |
| 3 | plaza, subdomains | plaza (new repo) | 1 released | Records, signup → provision → wait → handoff, sign-in, `/assert`, the switcher, people pages with roster delivery, `/fleet` |
| 4 | `--scaffold home` | rastrillo | 0, 3 | One binary for the root and its instances; `rastrillo new` requires `--scaffold`; end-to-end tests; `DEPLOY.md` |
| 5 | The skill | rastrillo, carlosframework/skills | 4 | The question in `SKILL.md`, `skill/instances.md`, the addons entry, the CARLOS chapter (UM Plans 8 and 9), and a fresh-agent test with skilleval |
| 6 | plaza, subdirectories, and `--scaffold hub` | plaza, rastrillo | 4, platform #186 | Hub signup with the path-map republish and fall-through check; the hub scaffold |
| 7 | Adoption | seamus, patrix | 4 (and 6 for subdirectories) | Each app on its own branch in its own repo |

Plans 0 and 1 have no dependencies on each other and can run in
parallel. #186 (path routes across boxes) is being designed on
`carlos/platform` branch `path-routes-across-boxes`.

**Live work needing scoped approval:** the end-to-end run in the
`demo` account at the end of Plan 4 (and again for Plan 6). Each of
those plans states the exact scope when it gets there.
