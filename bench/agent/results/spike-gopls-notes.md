# gopls spike notes

## Spike 1: `spike-gopls` (2026-11-22)

The round's own report is `spike-gopls.md`.

Command, from `bench/agent`, detached with `setsid nohup`:

    go run ./cmd/bench run -round spike-gopls -briefs smoke -arms carlos-gopls -n 1 \
      -rastrillo 60340a3e -listen-ip 100.119.180.57 -gopls ~/go/bin/gopls

Spend: about $2.02 of the bench token ($1.12 build, $0.79 follow-up, $0.11 for the two mapping questions). One Carloku app was created, `smoke-gopls-rspike-gopls-1`. It deployed, and there were no agent failures.

### What the transcript shows

- **gopls connected and its tools were offered.** The init record lists `gopls` as `connected` and offers eight tools: `go_diagnostics`, `go_file_context`, `go_package_api`, `go_rename_symbol`, `go_search`, `go_symbol_references`, `go_vulncheck` and `go_workspace`.
- **The agent never called one.** The build made 30 Bash calls. The follow-up made 14 Bash calls and 9 Reads. Neither made a gopls call, so the transcript cannot answer whether `go_workspace` sees the module.
- **gopls gives the agent no instructions.** gopls v0.24.0's MCP server sets no `instructions` in its server info, and its discover reply carries none. The 4,787 characters that `gopls mcp -instructions` prints are meant for a person to paste into a CLAUDE.md. The design allows only what MCP delivers, so this arm's agent sees just the eight tool descriptions.

### The question, asked of gopls directly

To get an answer without a second paid run, `gopls mcp` was driven over stdio the way the arm starts it: in an empty directory, with the arm's environment (HOME, PATH and the rest of the clean list, plus the run's GOPROXY and GONOSUMDB). The spike's finished app was then copied in, so `go.mod` appeared after gopls had started. Every tool was then called. There were three variants:

| | `go_workspace` after `go.mod` appears | file tools (`go_file_context`, `go_diagnostics`, `go_package_api`) | `go_vulncheck` |
|---|---|---|---|
| client reports no roots | "not a Go workspace", later "an ad-hoc Go package, not in a Go module" | results | results |
| client reports its directory as a root | "uses Go modules ... module hello" | results | results |
| gopls started after `go.mod` exists | "uses Go modules ... module hello" | results | results |

gopls watches files only in the directories a client reports as roots. It asks for roots only in its handler for the classic MCP `initialized` notification (`internal/mcp/mcp.go`, `internal/cmd/mcp.go` at v0.24.0).

**Which case the arm is in:** claude was run once more with the arm's MCP config, gopls's `-rpc.trace`, and a deliberately invalid token, so no model was called and nothing was spent. Claude Code 2.1.321 speaks MCP protocol 2026-07-28 to gopls: `server/discover`, `subscriptions/listen`, then `tools/list`. It sends no `initialize` or `initialized`, so gopls never asks for roots, even though claude declares the roots capability. The trace holds no `roots/list`.

So, in the arm as built:

- **During the build**, gopls starts before `go.mod` exists and never watches the app. `go_workspace` does not report the app's module. The file-based tools still return results.
- **During the follow-up**, claude starts again with `--resume`, gopls starts after `go.mod` exists, and `go_workspace` reports the module.
- **`go_vulncheck` works.** It reached `https://vuln.go.dev` in every variant, because claude starts MCP servers outside the Bash sandbox. Allowing `vuln.go.dev` in the sandbox would change nothing for gopls.

### What was changed

Nothing. The design allows three fixes, and none of them makes `go_workspace` see a module created after start in this claude version:

- **Starting gopls later** would need gopls to start after the agent's scaffold, inside one claude process.
- **Passing an argument** isn't possible: `gopls mcp` has only `-listen`, `-logfile`, `-rpc.trace` and `-instructions`.
- **Allowing `vuln.go.dev`** isn't needed.

No deviation was recorded in the round header, and the spike was not repeated. Whether this gopls is measured as it is, or with a change the design does not list yet, is for the controller to decide.

## Spike 2: `spike-gopls-2` (2026-11-22), with the wrapper and the instructions

The round's own report is `spike-gopls-2.md`. Its header records the arm's definition: the goplsmcp wrapper, and `gopls mcp -instructions` (sha256 `c22be2d4ea39`) after the environment notes.

Command, from `bench/agent`, detached with `setsid nohup`:

    go run ./cmd/bench run -round spike-gopls-2 -briefs smoke -arms carlos-gopls -n 1 \
      -rastrillo 091ea1de -listen-ip 100.119.180.57 -gopls ~/go/bin/gopls

Spend: about $2.84 ($0.90 build, $1.26 follow-up, $0.68 for the two mapping questions). One Carloku app was created, `smoke-gopls-rspike-gopls-2-1`. It deployed, and there were no agent failures.

### Did the agent call gopls?

Yes, this time:

- **Build:** it loaded `go_workspace`, `go_vulncheck` and `go_diagnostics` with ToolSearch as its very first call, as the instructions ask. It then called:
  - `go_workspace` once (call #4);
  - `go_diagnostics` on `internal/hello/app.go` and `greet_test.go` ("No diagnostics");
  - `go_vulncheck` on the module, which reached vuln.go.dev and reported GO-2026-6859 in the standard library.
- **Follow-up:** one `go_diagnostics` ("No diagnostics").

All four calls returned results. The rest of the work was Bash: 33 calls in the build, and 27 Bash calls and 14 Reads in the follow-up.

### Did `go_workspace` see the module during the build?

It was never asked after the module existed. Its only call, #4, came before the agent scaffolded: `rastrillo new --scaffold=bare hello` ran at #11, into `app/hello`. So it answered "This is not a Go workspace", which was true at that moment. The agent did not call `go_workspace` again.

`rec/goplsmcp.log` shows the restart did happen:

    20:35:31 started gopls in .../app
    20:36:20 found .../app/hello/go.mod; restarting gopls once nothing is in flight
    20:36:20 restarted gopls in .../app/hello
    20:38:51 started gopls in .../app/hello    (mapping question, build)
    20:39:09 started gopls in .../app/hello    (follow-up)
    20:44:35 started gopls in .../app/hello    (mapping question, follow-up)

The two gopls calls after the restart (#22 and #23, both naming files in `app/hello`) were answered over the same Claude Code session, so the replay held up with MCP 2026-07-28. The module was in a subdirectory, which the wrapper's three-level search found.

The direct evidence that `go_workspace` reports a module after a restart comes from driving the wrapper around real gopls with the app copied in after start: before the copy, "not a Go workspace"; after it, "uses Go modules ... module hello". A `go_workspace` call after the restart in a real run is still unobserved.

Cleanup, printed and not run:

    carlos apps delete --account-id bdf --app smoke-gopls-rspike-gopls-2-1
