package tmplescape

// GoVersion is the Go release this package and internal/htmlctx were
// forked from. TestForkMatchesToolchain fails when the toolchain's minor
// release differs, because a new html/template may escape differently
// and the fork would not follow.
const GoVersion = "go1.26.10"
