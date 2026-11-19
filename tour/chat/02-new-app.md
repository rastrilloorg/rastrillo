+++
title = "Create the app"
scaffold = true
commands = ["rastrillo new --auth=magic chat", "cd chat"]
check = "You'll find `cmd`, `internal`, `Makefile` and `AGENTS.md` in the new `chat` folder, among others."
+++

`rastrillo new` bootstraps your app. It builds, its tests pass
and it serves before you write a line. `--auth=magic` gives it sign-in by
emailed link, so people can sign in without a password.

The files you will edit live in `internal/chat/`:

- `models.go` holds the database models.
- `routes.go` lists every page the app serves.
- `handlers.go` holds what each page does.
- `render.go` draws pages from `templates/`.
- `app.go` builds the app when it starts.

Tests live in `internal/chattest/`. Everything in the app is yours to
edit from now on.
