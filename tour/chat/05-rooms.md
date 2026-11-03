+++
title = "Give rooms a table"
files = ["internal/chat/models.go"]
commands = ["rastrillo migration generate"]
run = ["rastrillo migration generate"]
check = "`internal/chat/migrations/` has a new file, `0002_create_rooms.sql`, and the dev server's log says it applied a migration."
+++

Chat happens in rooms, so rooms come first. A room is a row in the
database, and a model describes it: add `Room` to `models.go`, and list
it in `Models`.

`Note` is the scaffold's example model. Leave it for now; the account
page uses it.

Models don't change the database by themselves. `rastrillo migration
generate` compares the models with the migrations already written, and
writes a new migration for the difference. The app applies it the next
time it starts, which `rastrillo dev` does as soon as the file appears.
