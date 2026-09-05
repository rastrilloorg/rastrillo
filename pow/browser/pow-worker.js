// The Worker that hosts the solver.
//
// Thin on purpose: everything that could disagree with the Go verifier
// lives in powcore.js, which the browser test imports and runs
// directly. A worker cannot be imported, so any logic left in here
// would be untested by the one gate that matters.
//
// It runs off the main thread because a few hundred thousand hashes on
// it freezes a phone for seconds — the page stops scrolling and stops
// answering taps at exactly the moment somebody is deciding whether to
// trust you.

import { solve } from "./powcore.js";

self.onmessage = (e) => {
  const { nonce, binding, difficulty } = e.data;
  const counter = solve(nonce, binding, difficulty, (tried) => {
    self.postMessage({ done: false, tried });
  });
  self.postMessage({ done: true, counter });
};
