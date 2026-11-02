package opsgen

// Effects is what a generated descriptor says about a call: whether it
// may change or remove what is already there, and whether calling it
// twice leaves the state a single call would. The descriptors and the
// API contract both read it, so the two cannot disagree. A replaced
// write body is a hand-written one the generator cannot read, so it
// gets the cautious flags; a read stays a read whatever replaces it.
func Effects(action string, replaced bool) (destructive, idempotent bool) {
	switch action {
	case "list", "get":
		return false, true
	case "create":
		return replaced, false
	case "delete":
		return true, !replaced
	}
	// An update overwrites fields and a repeat moves updated_at again.
	// Anything else is a write nobody described, so it is cautious too.
	return true, false
}
