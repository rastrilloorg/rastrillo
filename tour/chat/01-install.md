+++
title = "Install Rastrillo"
commands = ["go install amadan.net/rastrillo/rastrillo/cmd/rastrillo@{{version}}"]
check = "Run `rastrillo` on its own to see a list of its commands."
+++

You need Go 1.26 or newer, and a terminal on Linux or macOS. On Windows,
work inside WSL.

Rastrillo is a Go module with a command-line tool, `rastrillo`, that makes
new apps, runs them while you work and checks them before you ship.
Install the tool at the version this tour was written against, so every
file you copy here matches what it makes.

If `rastrillo` is not found afterwards, add Go's bin directory to your
`PATH`: `export PATH="$PATH:$(go env GOPATH)/bin"`.
