+++
title = "Make the room live"
files = ["internal/chat/app.go", "internal/chat/live.go", "internal/chat/handlers.go", "internal/chat/templates/room.html"]
check = "With the room open in two windows, a message sent in one appears in the other without a reload, and the box you typed in clears."
+++

A live page updates parts of itself when the data behind them changes.
The app already has live-page support built in; you add the part that
is specific to your app.

- **A topic** names something that changes: here `room/7`, the topic
  name for room 7's messages. Declare it in `app.go`, with who may follow it: anyone
  signed in.
- **A region** is a part of the page that follows a topic. In
  `room.html`, the message list becomes `region:messages`.
- **Notify** after the write. When `postMessageAt` saves a message, it
  tells the hub that the room's topic moved.

Each page following that topic then requests its region again through
the same handler, with the permissions of the person viewing it. The
live stream sends only the topic name, never the messages themselves.

`data-live-form` lets the form send without a page load, then swap in
the regions it names from the response. The form sits in a region of its own, so a sent message clears
the box. With scripts turned off, everything still works as ordinary
pages.
