+++
title = "Add rooms from an admin page"
files = ["internal/chat/rooms.go", "internal/chat/admin.go", "internal/chat/templates/admin_rooms.html", "internal/chat/render.go", "internal/chat/routes.go"]
check = "Signed in, you see a form at http://localhost:8080/admin/rooms. A room you add appears in the list, and if you add the same name twice, you're told it is taken."
+++

Somebody has to make the rooms. This step adds an admin page with the
rooms listed under a form to add one.

The app keeps the rules for rooms apart from the actions that answer
web requests:

- `rooms.go` holds the rules: a name of 1 to 40 characters, and no two
  rooms with the same name. There is no HTTP code in it, so a test can
  call its functions directly. When a rule is broken, the function
  returns `op.Invalid` with the field's name and the reason.
- `admin.go` holds the actions. Each one reads the form, runs one rule,
  handles a refusal, and redirects on success. `RefusedShowing`
  redisplays the form with the error beside the field.

The page itself is `templates/admin_rooms.html`. Add its name to the
list in `render.go`, and its two routes to `routes.go`, inside the
group of pages that need sign-in.
