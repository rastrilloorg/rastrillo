// Package guardtest holds the test that every database entry point
// refuses under RASTRILLO_CHECK_CONFIG. It is its own package because
// the test imports db and the root package, which both import
// checkconfig; checkconfig itself stays standard-library only so any
// entry point can call it without a cycle.
package guardtest
