+++
title = "Run it"
commands = ["rastrillo dev"]
check = "The page at `http://localhost:8080` says *Hello*, and after signing in it offers *Sign out*."
+++

`rastrillo dev` builds the app and serves it, then rebuilds and restarts
it every time you save a file. If a build fails, the last good build
keeps serving, so a typo never takes the app down. Leave it running in
its own terminal for the rest of the tour.

Open http://localhost:8080. To sign in, choose **Sign in** and give any
email address. The app has no mail server yet, so instead of sending
the link it prints it in the terminal running `rastrillo dev`. Copy the
link from there into your browser.
