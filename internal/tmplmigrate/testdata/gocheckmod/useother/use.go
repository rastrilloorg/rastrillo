package useother

import "example.com/gocheck/otherui/ui"

type fake struct{}

func (fake) WithT(any) any { return nil }

func wire() {
	_ = ui.WithT(nil) // another package called ui: not reported
	{
		ui := fake{} // a local variable named ui: not reported
		_ = ui.WithT(nil)
	}
}
