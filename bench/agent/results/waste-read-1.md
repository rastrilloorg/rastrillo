# Where agents waste turns

6 kept runs, build and follow-up phases. A mistake is a build, test or gate command whose output shows it failed. Turns to fix are the messages from the failure to the same check next passing. A check that passes is one with a pass line, or a silent one with no pipe, ';' or '||' to hide its exit status; anything else is unknown and never a fix. A passing go test fixes any Go mistake; a silent go build or go vet fixes only a compile error.

| Kind | Mistakes | Fixed | Never fixed | Median turns to fix |
|---|---|---|---|---|
| go-compile | 16 | 14 | 2 | 8 |
| template | 0 | 0 | 0 | - |
| name-missing | 0 | 0 | 0 | - |
| route | 0 | 0 | 0 | - |
| gate | 14 | 9 | 5 | 4 |
| other | 46 | 33 | 13 | 2 |

## Examples

### go-compile

- read-1/groomer-carlos-1 build, message 78: `internal/groomer/browser_test.go:49:55: undefined: chromedp.ByQuery`
- read-1/groomer-carlos-2 build, message 22: `internal/groomer/migrations.go:10:12: pattern migrations/*.sql: no matching files found`
- read-1/groomer-carlos-2 build, message 57: `vet: internal/groomer/harness_test.go:46:31: not enough arguments in call to groomer.New`

### template


### name-missing


### route


### gate

- read-1/groomer-carlos-1 build, message 75: `code`
- read-1/groomer-carlos-1 followup, message 19: `code`
- read-1/groomer-carlos-1 followup, message 20: `fmt-check`

### other

- read-1/groomer-carlos-1 build, message 72: `TestPerfScreens`
- read-1/groomer-carlos-1 build, message 73: `TestPerfScreens`
- read-1/groomer-carlos-2 build, message 60: `FAIL`
