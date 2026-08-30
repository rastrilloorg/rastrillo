// The design-system gallery is generated and is not committed: it is
// 20 MB of machine output, rewritten whole every time a ui partial
// changes, and the website that publishes it builds it instead by
// running cmd/dsgen against a pinned version of this module.
//
// `go generate ./...` writes a copy into .design-system/, which is
// git-ignored. It is there to be looked at — open a page, diff two runs,
// see what a change to a partial did to every page that renders it — and
// nothing reads it back. Deleting it costs nothing.
//
//go:generate go run ./cmd/dsgen -out .design-system
package rastrillo
