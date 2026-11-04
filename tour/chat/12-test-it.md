+++
title = "Test two people chatting"
files = ["internal/chattest/chat_test.go"]
commands = ["go test ./..."]
check = "`go test ./...` passes, three new tests included."
+++

The tests use the app as people do: through its pages, over HTTP, with
the real sign-in.

- Ada, an admin, adds a room; Bob posts in it; Ada sees his message.
- An empty message is refused with a reason.
- Bob, who is not an admin, gets *Page not found* from the admin pages,
  and someone signed out is sent to sign in.

These tests set `CHAT_ADMINS` for the whole test run, so they don't run
in parallel with each other.
