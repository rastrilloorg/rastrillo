package ui

import _ "embed"

//go:embed transition.js
var transitionJS []byte

// TransitionJS returns transition.js for column, stage and topbar pages.
// Load it before styles so skipped incoming transitions are observed even
// when pagereveal has no transition. Like ShellJS, it is vendored once and
// app-owned from then on.
func TransitionJS() []byte { return transitionJS }
