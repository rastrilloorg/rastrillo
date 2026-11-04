+++
title = "Add rooms from an admin page"
files = ["internal/chat/rooms.go", "internal/chat/admin.go", "internal/chat/templates/admin_rooms.html", "internal/chat/render.go", "internal/chat/routes.go"]
check = "Signed in, http://localhost:8080/admin/rooms shows a form. Adding a room lists it, and adding the same name twice says it is taken."
+++

Somebody has to make the rooms. This step adds an admin page with the
rooms listed under a form to add one.

The app keeps the rules for rooms apart from the actions that answer
web requests:

- `rooms.go` holds the rules: a name of 1 to 40 characters, and no two
  rooms with the same name. These functions know nothing about HTTP, so
  a test can call them directly. A broken rule comes back as
  `op.Invalid`, naming the field and saying what is wrong.
- `admin.go` holds the actions. Each one reads the form, runs one rule,
  answers a refusal and says what success looks like. `RefusedShowing`
  shows the form again with the error beside the field.

The page itself is `templates/admin_rooms.html`. Add its name to the
list in `render.go`, and its two routes to `routes.go`, inside the
group of pages that need sign-in.
