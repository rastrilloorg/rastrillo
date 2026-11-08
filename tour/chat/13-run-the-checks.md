+++
title = "Run the whole gate"
files = ["internal/chattest/perf_test.go"]
commands = ["make ci"]
check = "`make ci` finishes without an error."
+++

`make ci` is every check the app has, in one command: vet, formatting,
static analysis, known vulnerabilities, leaked secrets, size and code
limits, the route table, the tests, the performance lane, and whether
the migrations and generated files are up to date. CI runs the same
command, so passing here means passing there.

The first run fails in the performance lane. That lane times every
page, and the room and admin pages can't be timed without a room in
the database and an admin signed in. Give the performance test what it
needs: `CHAT_ADMINS` for its signed-in user, a room in the database,
and a real URL for each page with an `{id}` in it.
