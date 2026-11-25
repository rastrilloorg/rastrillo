# Where agents waste turns

6 kept runs, build and follow-up phases. A mistake is a build, test or gate command whose output shows it failed. Turns to fix are the messages from the failure to the same check next passing. A check that passes is one with a pass line, or a silent one with no pipe, ';' or '||' to hide its exit status; anything else is unknown and never a fix. A passing go test fixes any Go mistake; a silent go build or go vet fixes only a compile error.

| Kind | Mistakes | Fixed | Never fixed | Median turns to fix |
|---|---|---|---|---|
| go-compile | 15 | 12 | 3 | 10 |
| template | 0 | 0 | 0 | - |
| name-missing | 2 | 2 | 0 | 2.5 |
| route | 0 | 0 | 0 | - |
| gate | 11 | 6 | 5 | 7.5 |
| other | 58 | 40 | 18 | 3 |

## Examples

### go-compile

- read-1/lending-carlos-gopls-1 build, message 36: `vet: internal/lending/harness_test.go:46:31: not enough arguments in call to lending.New`
- read-1/lending-carlos-gopls-2 build, message 21: `internal/lending/migrations.go:10:12: pattern migrations/*.sql: no matching files found`
- read-1/lending-carlos-gopls-2 build, message 67: `internal/lending/browser_test.go:45:54: undefined: chromedp.ByQuery`

### template


### name-missing

- read-1/lending-carlos-gopls-2 build, message 60: `rastrillo: 1 unknown icon slug(s)`
- read-1/noticeboard-carlos-gopls-3 build, message 82: `rastrillo: 1 unknown icon slug(s)`

### route


### gate

- read-1/lending-carlos-gopls-1 build, message 54: `code`
- read-1/lending-carlos-gopls-1 followup, message 25: `code`
- read-1/lending-carlos-gopls-2 build, message 54: `fmt-check`

### other

- read-1/lending-carlos-gopls-1 build, message 50: `FAIL`
- read-1/lending-carlos-gopls-1 build, message 51: `TestMemberBorrowsAFreeItemAndSeesItInTheirLoans`
- read-1/lending-carlos-gopls-1 build, message 57: `TestPerfScreens`
