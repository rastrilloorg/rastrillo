// Package routelist reads an app's route table without serving the
// app. It finds the app's Routes function, builds a small program that
// calls it with a recording table and an empty dependency, and reads
// back what was registered. The program is placed inside the app's
// module through the go command's overlay, so nothing is written into
// the app, and go.mod and go.sum are left as they were.
package routelist
