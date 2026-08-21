package form

// Errors maps a field name to its validation error message — the shape
// a generated action's handler builds while checking submitted values
// and a formView's Errors field renders back beside each input.
type Errors map[string]string

// Any reports whether e holds at least one field error.
func (e Errors) Any() bool {
	return len(e) > 0
}
