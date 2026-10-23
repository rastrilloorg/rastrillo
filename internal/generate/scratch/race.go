//go:build race

package scratch

// raceDetector is whether this test binary runs under -race; the build
// tag is the only way to know, and GoTest passes it on.
const raceDetector = true
