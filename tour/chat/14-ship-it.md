+++
title = "Ship it"
commands = ["make release VERSION=v0.1.0", "carlos deploy --app chat --version v0.1.0 --env CHAT_ADMINS=you@example.com releases/chat-linux-arm64", "carlos email enable --app chat --env-prefix CHAT_SMTP"]
check = "Your app is up at its URL, and sign-in emails arrive."
+++

> This step needs a CARLOS account. If you don't have one, start at
> [carloku.com](https://carloku.com), or stop here: the app is finished.

`make release` builds one small binary for the server. `carlos deploy`
ships it, starts it and waits until it answers; the first deploy
creates the app, `--version` names the release, and `--env` sets
`CHAT_ADMINS` on it.

`carlos email enable` switches on real email, so sign-in links go out
from `noreply@` at the app's address. `--env-prefix CHAT_SMTP` delivers
the settings under the names the app reads.

The sign-in links and the form checks use the address the app is served
at. On CARLOS it is the instance's own hostname, so there is nothing to
set unless you use a custom domain or a hub path. To serve it at your
own domain, set `CHAT_ORIGIN` to it, such as `https://chat.example.com`.

Serve it on a route that is always on. While a room page is open, the
browser keeps a stream to the server open, which keeps a hibernating
instance awake.

The rest is in [Deploying](/docs/deploying).
