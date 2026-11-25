package groomer

import "fmt"

func New() string {
	return fmt.Sprint("scaffold", "plus")
}

func Router() string {
	return "router"
}
