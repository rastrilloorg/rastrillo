# Where agents waste turns

12 kept runs, build and follow-up phases. A mistake is a build, test or gate command whose output shows it failed. Turns to fix are the messages from the failure to the same check next passing. A check that passes is one with a pass line, or a silent one with no pipe, ';' or '||' to hide its exit status; anything else is unknown and never a fix. A passing go test fixes any Go mistake; a silent go build or go vet fixes only a compile error.

| Kind | Mistakes | Fixed | Never fixed | Median turns to fix |
|---|---|---|---|---|
| go-compile | 31 | 25 | 6 | 10 |
| template | 0 | 0 | 0 | - |
| name-missing | 5 | 4 | 1 | 2 |
| route | 0 | 0 | 0 | - |
| gate | 21 | 9 | 12 | 8 |
| other | 127 | 92 | 35 | 3 |

## Examples

### go-compile

- read-1/lending-carlos-1 build, message 59: `vet: internal/lending/harness_test.go:46:31: not enough arguments in call to lending.New`
- read-1/lending-carlos-1 build, message 88: `internal/lending/browser_test.go:81:10: cannot use chromedp.FullScreenshot(&picture, 80) (value of func type chromedp.Action[[]byte]) as chromedp.Action[chromed...`
- read-1/lending-carlos-1 followup, message 15: `internal/lending/browser_test.go:53:11: invalid operation: cannot call emulation.SetEmulatedMedia (variable of struct type cdp.Command[emulation.SetEmulatedMedi...`

### template


### name-missing

- read-1/lending-carlos-2 build, message 96: `rastrillo: 1 unknown icon slug(s)`
- read-1/lending-carlos-3 build, message 73: `rastrillo: 1 unknown icon slug(s)`
- read-1/lending-carlos-gopls-2 build, message 60: `rastrillo: 1 unknown icon slug(s)`

### route


### gate

- read-1/lending-carlos-1 build, message 73: `code`
- read-1/lending-carlos-2 build, message 94: `code`
- read-1/lending-carlos-2 build, message 95: `generate-check`

### other

- read-1/lending-carlos-1 build, message 27: `TestModelsMatchMigrations`
- read-1/lending-carlos-1 build, message 58: `rastrillo: compile templates: tmplgen: listing ./cmd/...: err: exit status 1: stderr: go: updates to go.mod needed; to update it:`
- read-1/lending-carlos-1 build, message 64: `FAIL`
