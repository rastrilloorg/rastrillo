// Package app has its Routes behind a build tag, and a wrong one in a
// file for another platform. Without the tag the build has neither.
package app

// App is what the handlers would need.
type App struct{}
