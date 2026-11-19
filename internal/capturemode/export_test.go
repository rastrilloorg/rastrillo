package capturemode

// reset forgets the names given out, as a new capture process starts
// with none.
func reset() {
	mu.Lock()
	defer mu.Unlock()
	names = map[string]string{}
}
