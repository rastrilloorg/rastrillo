+++
title = "Only admins"
files = ["internal/chat/admin.go", "internal/chat/routes.go"]
commands = ["CHAT_ADMINS=you@example.com rastrillo dev"]
check = "Signed in as the address you listed, the admin page works. Signed in as any other address, it answers Page not found."
+++

Right now anyone who signs in can add rooms. Admins are the people whose
addresses are listed in `CHAT_ADMINS`, separated by commas.

`requireAdmin` is a guard: it runs before the admin pages, and lets the
request through only for an admin. Everyone else gets *Page not found*,
not *Forbidden*, so the admin pages don't reveal that they exist.

In `routes.go`, move the admin routes into a group of their own that
carries the guard. Then restart `rastrillo dev` with `CHAT_ADMINS` set
to the address you sign in with.
