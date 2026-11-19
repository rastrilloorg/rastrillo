+++
title = "Ship it"
commands = ["make release VERSION=v0.1.0", "carlos ship -app chat -target linux-arm64 releases/chat-linux-arm64", "carlos env set --app chat CHAT_ADMINS=you@example.com"]
check = "Your app is up at its URL, and sign-in emails arrive."
+++

> This step needs a CARLOS account. If you don't have one, start at
> [carloku.com](https://carloku.com), or stop here: the app is finished.

`make release` builds one small binary for the server. `carlos ship`
uploads it; then promote it as the CARLOS documentation describes.

The sign-in links and the form checks use the address the app is served
at. On CARLOS it is the instance's own hostname, so there is nothing to
set unless you use a custom domain or a hub path. To serve it at your
own domain, set `CHAT_ORIGIN` to it, such as `https://chat.example.com`.
To send real email, set `CHAT_SMTP_HOST`, `CHAT_SMTP_PORT`,
`CHAT_SMTP_FROM`, `CHAT_SMTP_USER` and `CHAT_SMTP_PASS`.

Serve it on a route that is always on. While a room page is open, the
browser keeps a stream to the server open, which keeps a hibernating
instance awake.

The rest is in [Deploying](/docs/deploying).
