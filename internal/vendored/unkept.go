package vendored

import (
	"maps"
	"slices"
)

// Unkept returns the hooks, among hooks, named by each style rule that
// a page carrying hooks matches with one of its selectors in whole but
// not in slice, each once, sorted: what the page would show unstyled
// on slice that whole would style. A selector counts only where Slice
// would keep it, so a hook whose rules all need a hook the page lacks
// is not unkept, and a hook named in the slice only inside :not() is no
// rule for it. A selector is the same only with the same block in the
// same groups, so one kept outside its @media is not kept.
func Unkept(whole, slice []byte, hooks map[string]bool) ([]string, error) {
	wanted, err := read(whole, 0)
	if err != nil {
		return nil, err
	}
	served, err := read(slice, 0)
	if err != nil {
		return nil, err
	}
	page := readShown(hooks)
	kept := map[string]bool{}
	for _, met := range metSelectors(served, page, "") {
		kept[met.place] = true
	}
	unkept := map[string]bool{}
	for _, met := range metSelectors(wanted, page, "") {
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

// A metSelector is a selector Slice keeps for a set of hooks, placed
// by the openings of the groups it sits in, itself and its rule's
// block, with the hooks its rule names.
type metSelector struct {
	place string
	named []string
}

// metSelectors returns the selectors of the style rules of rules, at
// any depth, that hooks meet, as keptText chooses them. A rule it
// cannot cut into selectors, or that nests rules, is placed whole, as
// keptText keeps it.
func metSelectors(rules []rule, page shown, within string) []metSelector {
	var met []metSelector
	for _, rule := range rules {
		if rule.opening != nil {
			inside := within + string(rule.opening)
			met = append(met, metSelectors(rule.rules, page, inside)...)
			continue
		}
		if rule.selector == nil {
			continue
		}
		selectors := splitAtCommas(rule.selector)
		_, entries, block, ok := cutSelectors(rule.text)
		if rule.nested || !ok || len(entries) != len(selectors) {
			if anySelectorMet(rule.selector, page) {
				met = append(met,
					metSelector{within + string(rule.text), rule.named})
			}
			continue
		}
		for index, selector := range selectors {
			if selectorMet(selector, page) {
				place := within + string(entries[index]) + string(block)
				met = append(met, metSelector{place, rule.named})
			}
		}
	}
	return met
}
