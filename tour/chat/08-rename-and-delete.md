+++
title = "Rename and delete rooms"
files = ["internal/chat/rooms.go", "internal/chat/admin.go", "internal/chat/templates/admin_room.html", "internal/chat/templates/admin_room_delete.html", "internal/chat/render.go", "internal/chat/routes.go"]
check = "Choose a room on the admin page to open it. You can rename it, and you're asked to confirm before it is deleted."
+++

Each room gets its own admin page, with a form to rename it and a link
to delete it.

Deleting has a page of its own, where you confirm before anything
happens. Following a link never changes data; only submitting the form
on that page does. Its Cancel link comes before the delete button, so
deleting is always a deliberate choice.

`roomAt` reads the room's id from the URL. For a room that doesn't
exist it returns `op.NotFound`, and `visit.Refused` turns that into
*Page not found*.
