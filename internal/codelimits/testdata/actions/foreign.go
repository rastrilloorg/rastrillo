package handlers

import "example.com/other/action"

// Foreign takes a Visit from a different package that is also named
// action, so it is an ordinary function with a limit of 40.
func Foreign(visit *action.Visit) {
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
}
