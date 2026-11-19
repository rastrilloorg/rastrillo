package allowed

// Opt holds a value that may be absent. Its fields are one letter, which
// the limits do not count.
type Opt[T any] struct {
	X, Y  int
	value T
}

// Pair takes two one-capital type parameters.
func Pair[K comparable, V any](key K, value V) (K, V) { return key, value }
