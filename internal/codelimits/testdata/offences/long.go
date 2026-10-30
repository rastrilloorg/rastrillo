package offences

import "amadan.net/rastrillo/rastrillo/action"

// LongFunction has a body of 41 lines, one over the limit.
func LongFunction() int {
	total := 0
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	return total
}

type handlers struct{}

// Show is an action with a body of 16 lines, one over the limit.
func (handlers *handlers) Show(visit *action.Visit) {
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

func register(handler func(*action.Visit)) {}

// Register passes an action literal of 16 lines to a call.
func Register() {
	register(func(visit *action.Visit) {
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
	})
}

// handler is a package-level function literal with a body of 41 lines.
var handler = func() int {
	total := 0
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	total++
	return total
}
