+++
title = "Rename and delete rooms"
files = ["internal/chat/rooms.go", "internal/chat/admin.go", "internal/chat/templates/admin_room.html", "internal/chat/templates/admin_room_delete.html", "internal/chat/render.go", "internal/chat/routes.go"]
check = "Choosing a room on the admin page opens it. Renaming works, and deleting asks first."
+++

Each room gets its own admin page, with a form to rename it and a link
to delete it.

Deleting has a page of its own that asks before anything happens. A
link never changes data; only the form on that page does. Its Cancel
link comes before the delete button, so deleting is always a choice.

`roomAt` reads the room's id from the URL. A room that doesn't
exist comes back as `op.NotFound`, which `visit.Refused` turns into
*Page not found*.
