// Package greeting is a sibling module the app reaches through a local
// replace, so listing must build with the app's own go.mod.
package greeting

// Hello is what the app's index would say.
func Hello() string { return "hello" }
