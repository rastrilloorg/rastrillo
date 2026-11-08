+++
title = "Only admins"
files = ["internal/chat/admin.go", "internal/chat/routes.go"]
commands = ["CHAT_ADMINS=you@example.com rastrillo dev"]
check = "Signed in as the address you listed, you can use the admin page. Signed in as any other address, you get *Page not found*."
+++

Right now anyone who signs in can add rooms. Admins are the people whose
addresses are listed in `CHAT_ADMINS`, separated by commas.

`requireAdmin` is a guard: it runs before the admin pages, and lets the
request through only for an admin. Everyone else gets *Page not found*,
not *Forbidden*, so nobody else can tell the admin pages exist.

In `routes.go`, move the admin routes into a group of their own with the
guard on it. Then restart `rastrillo dev` with `CHAT_ADMINS` set to the
address you sign in with.
