+++
title = "Create the app"
scaffold = true
commands = ["rastrillo new --scaffold accounts chat", "cd chat"]
check = "You'll find `cmd`, `internal`, `Makefile` and `AGENTS.md` in the new `chat` folder, among others."
+++

`rastrillo new` bootstraps your app. It builds, its tests pass
and it serves before you write a line. `--scaffold accounts` gives it
sign-in: people get in with a link emailed to them, and can add a
passkey, an authenticator app or a passphrase later.

The files you will edit live in `internal/chat/`:

- `models.go` holds the database models.
- `routes.go` lists every page the app serves.
- `handlers.go` holds what each page does.
- `render.go` draws pages from `templates/`.
- `app.go` builds the app when it starts.

Tests live in `internal/chat/*_test.go`. Everything in the app is yours to
edit from now on.
