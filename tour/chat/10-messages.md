+++
title = "Post messages"
files = ["internal/chat/models.go", "internal/chat/messages.go", "internal/chat/rooms.go", "internal/chat/handlers.go", "internal/chat/templates/room.html", "internal/chat/routes.go"]
commands = ["rastrillo migration generate"]
run = ["rastrillo migration generate"]
check = "In a room, a message you send appears in the list. An empty one is refused with a reason."
+++

A message belongs to a room and remembers who sent it. Add `Message` to
`models.go`, then generate its migration as you did for rooms.

`messages.go` holds the rules: 1 to 500 characters, in a room that
exists. The room page shows the newest 100 messages, with the oldest at
the top.

Deleting a room now has messages to deal with. `deleteRoom` removes
them in the same transaction as the room, so no message is left
pointing at a room that is gone.

Open the room in two windows and send a message from one. The other
window doesn't change until you reload it. The next step fixes that.
