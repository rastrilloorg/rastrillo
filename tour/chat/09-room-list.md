+++
title = "List the rooms"
files = ["internal/chat/handlers.go", "internal/chat/templates/index.html", "internal/chat/templates/room.html", "internal/chat/render.go", "internal/chat/routes.go", "internal/chattest/index_test.go"]
commands = ["go test ./..."]
check = "The home page lists the rooms, and each opens a page with its name."
+++

Everyone who signs in should see the rooms. The home page now lists
them, and each links to a page of its own, where messages will go in the
next step.

`go test ./...` fails after this change, and that is the test doing its
job: the scaffold's test of the home page still looks for the old
*Hello* heading. Update it to look for the empty room list instead.
