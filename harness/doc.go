// Package harness drives a rastrillo app in a real Chromium with a CDP
// virtual authenticator attached — the browser rig behind
// `go test -tags browser ./...`.
//
// Every other file in this package is build-tagged `browser`. The
// README promises that chromedp stays out of the ordinary
// build graph (`go list -deps ./...` pulls none of it), and an
// untagged package importing chromedp would make that sentence false
// the day it landed. This doc file is what keeps a plain
// `go build ./...` and `go vet ./...` seeing a valid package; the rig
// itself exists only under the tag.
//
// The shape (spec: docs/superpowers/specs/2026-08-23-harness-design.md):
// New binds a localhost listener first — the origin chicken-and-egg a
// passkey app forces — hands the app its origin, then launches
// Chromium with a virtual authenticator; watchers make every console
// error, failed request and 4xx/5xx a test failure; Screen gates each
// screen behind the junk scan, the bug class that renders perfectly
// and says nothing.
package harness
