+++
title = "Run the tests"
commands = ["go test ./..."]
check = "`go test ./...` finishes without a failure; packages with tests say `ok`."
+++

The app came with tests for the pages it already has, sign-in included.
Run them now, so you know what passing looks like before you change
anything.

Run them again after each step. When a test fails, it says what it
expected and what it got.
