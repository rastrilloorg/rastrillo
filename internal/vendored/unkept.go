package vendored

import (
	"maps"
	"slices"
)

// Unkept returns the hooks, among hooks, named by each style rule that
// a page carrying hooks gets from whole but not from slice, each once,
// sorted: what the page would show unstyled on slice that whole would
// style. A rule counts only where Slice would keep it, so a hook whose
// rules all need a hook the page lacks is not unkept, and a hook named
// in the slice only inside :not() is no rule for it. A rule is the same
// rule only in the same groups, so one kept outside its @media is not
// kept.
func Unkept(whole, slice []byte, hooks map[string]bool) ([]string, error) {
	wanted, err := read(whole, 0)
	if err != nil {
		return nil, err
	}
	served, err := read(slice, 0)
	if err != nil {
		return nil, err
	}
	kept := map[string]bool{}
	for _, met := range metRules(served, hooks, "") {
		kept[met.place] = true
	}
	unkept := map[string]bool{}
	for _, met := range metRules(wanted, hooks, "") {
		if kept[met.place] {
			continue
		}
		for _, name := range met.named {
			if hooks[name] {
				unkept[name] = true
			}
		}
	}
	return slices.Sorted(maps.Keys(unkept)), nil
}

// A metRule is a style rule Slice keeps for a set of hooks, placed by
// the openings of the groups it sits in and its own text.
type metRule struct {
	place string
	named []string
}

// metRules returns the style rules of rules, at any depth, whose
// selectors hooks meet, as writeKept chooses them.
func metRules(rules []rule, hooks map[string]bool, within string) []metRule {
	var met []metRule
	for _, rule := range rules {
		switch {
		case rule.opening != nil:
			inside := within + string(rule.opening)
			met = append(met, metRules(rule.rules, hooks, inside)...)
		case rule.selector != nil && anySelectorMet(rule.selector, hooks):
			met = append(met,
				metRule{within + string(rule.text), rule.named})
		}
	}
	return met
}
