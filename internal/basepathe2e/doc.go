// Package basepathe2e drives a freshly scaffolded app under /acme in a
// real Chromium, behind an in-process stand-in for the edge: a hub at
// the product origin's root, two tenants of the same app under /acme
// and /bob, and the tenant's own direct host.
//
// Unit tests prove each piece emits the base. Only a browser shows that
// nothing on a real page still asks the shared origin's root, where the
// hub would answer.
//
// The drive is build-tagged browser and runs as its own suite role
// (make browser.internal.basepathe2e). This file has no tag, so a plain
// go list ./... still names the package rather than finding a directory
// whose every file the build constraints exclude.
package basepathe2e
