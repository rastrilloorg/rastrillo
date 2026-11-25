package dotaction

import . "amadan.net/rastrillo/rastrillo/action"

// Callbacks holds an action, which takes a *Visit dot-imported from the
// action package, and an ordinary callback. The action is 16 lines
// long, one over an action's limit.
var Callbacks = struct {
	Show  func(*Visit)
	Count func(int) int
}{
	Show: func(visit *Visit) {
		_ = visit
		_ = visit
		_ = visit
		_ = visit
		_ = visit
		_ = visit
		_ = visit
		_ = visit
		_ = visit
		_ = visit
		_ = visit
		_ = visit
		_ = visit
		_ = visit
		_ = visit
		_ = visit
	},
	Count: func(n int) int { return n + 1 },
}
