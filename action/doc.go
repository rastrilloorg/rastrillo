// Package action lets a request handler read like a sentence. A Visit
// holds the response, the request and the app's per-request context in
// one value, so an action takes a single argument and says what it does
// to it: read an id, render a page, flash a notice, redirect.
//
// It sits on top of view, flash and op and changes none of them. The
// handlers it serves are ordinary net/http handlers, so an action from
// this package and a generated action can share one router.
package action
