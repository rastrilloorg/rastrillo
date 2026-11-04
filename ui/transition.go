package ui

// TransitionJS returns Fiesta's transition.js for column, stage and
// topbar pages. Load it before styles so skipped incoming transitions are
// observed even when pagereveal has no transition. Like ShellJS, it is
// vendored once and app-owned from then on.
func TransitionJS() []byte { return fiestaScript("transition.js") }
