package app

import "amadan.net/rastrillo/rastrillo/action"

// Tags and Tickets each have a Show, and only one is mounted, so a
// check that compared names would miss Tickets'.
type Tags struct{}

// Show is mounted under /admin.
func (tags *Tags) Show(visit *action.Visit) {}

type Tickets struct{}

// Show is mounted by no route.
func (tickets *Tickets) Show(visit *action.Visit) {}

// Report has a value receiver, so its method value is named without
// the pointer: app.Report.Show.
type Report struct{}

// Show is mounted at /report.
func (report Report) Show(visit *action.Visit) {}
