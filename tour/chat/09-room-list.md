+++
title = "List the rooms"
files = ["internal/chat/handlers.go", "internal/chat/templates/index.html", "internal/chat/templates/room.html", "internal/chat/render.go", "internal/chat/routes.go", "internal/chattest/index_test.go"]
commands = ["go test ./..."]
check = "You see the rooms listed on the home page, and each one opens a page with its name."
+++

Everyone who signs in should see the rooms. List them on the home page,
each linking to a page of its own, where messages go in the next step.

`go test ./...` fails after this change, as it should: the scaffold's
test of the home page still checks for the old *Hello* heading. Update
it to check for the empty room list instead.
