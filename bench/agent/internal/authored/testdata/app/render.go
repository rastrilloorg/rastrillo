package groomer

// Render now has a comment.
func Render(name string) string {
	// A comment inside.
	if name == "" {
		return "anonymous"
	}
	return name
}
