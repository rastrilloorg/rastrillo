# Where agents waste turns

12 kept runs, build and follow-up phases. A mistake is a build, test or gate command whose output shows it failed. Turns to fix are the messages from the failure to the same check next passing. A check that passes is one with a pass line, or a silent one with no pipe, ';' or '||' to hide its exit status; anything else is unknown and never a fix. A passing go test fixes any Go mistake; a silent go build or go vet fixes only a compile error.

| Kind | Mistakes | Fixed | Never fixed | Median turns to fix |
|---|---|---|---|---|
| go-compile | 26 | 19 | 7 | 5 |
| template | 4 | 3 | 1 | 2 |
| name-missing | 3 | 2 | 1 | 4 |
| route | 1 | 0 | 1 | - |
| gate | 29 | 21 | 8 | 3 |
| other | 37 | 32 | 5 | 3 |

## Examples

### go-compile

- 1/groomer-carlos-1 build, message 30: `internal/groomer/migrations.go:10:12: pattern migrations/*.sql: no matching files found`
- 1/groomer-carlos-1 build, message 103: `vet: internal/groomertest/api_test.go:76:25: undefined: groomer.Tokens`
- 1/groomer-carlos-2 build, message 48: `vet: internal/groomer/handlers.go:12:6: app redeclared in this block`

### template

- 1/groomer-carlos-1 build, message 77: `2026/10/30 22:35:12 ERROR render page page=book-time ref=7bjrmx err="template: book-time.html:3:57: executing \"content\" at <.Service.bookPath>: can't evaluate...`
- cost-1/groomer-carlos-1 build, message 75: `rastrillo: internal/groomer/rastrillo_templates_gen.go is out of date with the app's templates; run rastrillo generate`
- weight-1/groomer-carlos-1 build, message 88: `rastrillo: internal/groomer/rastrillo_templates_gen.go is out of date with the app's templates; run rastrillo generate`

### name-missing

- 1/groomer-carlos-2 followup, message 21: `rastrillo: 10 unknown icon slug(s)`
- accept-1/groomer-carlos-3 followup, message 50: `rastrillo: 16 unknown icon slug(s)`
- weight-1/groomer-carlos-2 followup, message 36: `rastrillo: 6 unknown icon slug(s)`

### route

- weight-1/groomer-carlos-1 build, message 89: `rastrillo: No route mounts these actions:`

### gate

- 1/groomer-carlos-1 build, message 98: `budget code`
- 1/groomer-carlos-1 build, message 101: `budget code`
- 1/groomer-carlos-1 build, message 105: `budget code`

### other

- 1/groomer-carlos-1 build, message 41: `FAIL	groomer/internal/groomer [setup failed]`
- 1/groomer-carlos-1 build, message 43: `FAIL	groomer/internal/groomer [setup failed]`
- 1/groomer-carlos-1 build, message 76: `FAIL`
