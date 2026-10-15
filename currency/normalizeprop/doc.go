// The normalising money reading's property tests live here, not in
// currency: they run the parser over about 600,000 generated inputs in
// every shipped locale, far past currency's 10 s budget, and cutting
// them to a sample would no longer prove the stated ambiguity shape
// exactly. This package holds them under a time budget of their own.

package normalizeprop
