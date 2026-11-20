package ui

import (
	"html/template"

	"amadan.net/rastrillo/rastrillo"
)

// iconOrFramework is an app's icon function with the framework's set
// behind it: the app's drawing when it has one, rastrillo.Icon's when
// it answers nothing. An app owns the icon package rastrillo new wrote,
// and nothing rewrites it on an upgrade, so one scaffolded before the
// scheme control's sun, moon and monitor, or the up link's
// chevron-left, would otherwise draw those controls blank. A slug the
// app does answer is always the app's, so its own set and its custom
// icons win; only the gaps are Lucide.
// TestAnOldAppsIconsDrawTheNewControls holds both halves.
func iconOrFramework(
	icon func(string) template.HTML,
) func(string) template.HTML {
	return func(slug string) template.HTML {
		if drawing := icon(slug); drawing != "" {
			return drawing
		}
		return rastrillo.Icon(slug)
	}
}

// appFunc is one of PagesConfig.Funcs as ui.Pages binds it: the app's
// icon with the framework's set behind it, anything else as given.
func appFunc(name string, function any) any {
	if icon, ok := function.(func(string) template.HTML); ok &&
		name == "icon" {
		return iconOrFramework(icon)
	}
	return function
}
