+++
title = "Make the room live"
files = ["internal/chat/app.go", "internal/chat/live.go", "internal/chat/handlers.go", "internal/chat/templates/room.html"]
check = "With the room open in two windows, a message sent in one appears in the other without a reload, and the box you typed in clears."
+++

On a live page, parts of the page update when the data behind them
changes. Live-page support is already built into the app; you add the
part that is specific to yours.

- **A topic** is the name of something that changes: here `room/7`,
  for room 7's messages. Declare it in `app.go`, with who may follow
  it: anyone signed in.
- **A region** is a part of the page tied to a topic. In `room.html`,
  the message list becomes `region:messages`.
- **Notify** after the write. When `postMessageAt` saves a message, it
  tells the hub that the room's topic moved.

Every browser showing that room then fetches the region again through
the same handler, with the permissions of the person viewing it. Only
the topic name goes down the live stream, never the messages
themselves.

With `data-live-form`, the form is sent without a page load, and the
regions it names are swapped in from the response. The form is in a
region of its own, so the box is cleared once a message is sent. With
scripts turned off, everything still works as ordinary pages.
