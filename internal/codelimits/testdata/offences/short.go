package offences

var x = 1

const n = 2

// Sum declares a range variable and takes a parameter, both one letter.
func Sum(p int) int {
	total := 0
	for i := range p {
		total += i
	}
	return total
}
