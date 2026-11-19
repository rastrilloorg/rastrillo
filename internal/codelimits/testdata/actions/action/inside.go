package action

// Visit is declared here, so an unqualified *Visit is the real one.
type Visit struct{}

// Inside takes the unqualified Visit of the action package itself.
func Inside(visit *Visit) {
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
