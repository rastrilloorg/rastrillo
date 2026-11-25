package groomer

func Render(name string) string {
	if name == "" {
		return "anonymous"
	}
	return name
}
