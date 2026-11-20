// The icons package rastrillo new wrote before 0.38 (lucide, inline),
// byte for byte from 81862585: it has no sun, moon, monitor or
// chevron-left. TestAnOldAppsIconsDrawTheNewControls reads it.

//rastrillo:data a table of SVG strings, one per icon

// Package icons is this app's icon set, from rastrillo new
// --icons=lucide --icon-delivery=inline, and the app's to edit.
// Keys are Lucide slugs whatever the set: rastrillo's icon vocabulary.
package icons

import "html/template"

var icons = map[string]template.HTML{
	"alert-triangle": `<svg class="icon" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" viewBox="0 0 24 24"><path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z"/><path d="M12 9v4"/><path d="M12 17h.01"/></svg>`,
	"check":          `<svg class="icon" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" viewBox="0 0 24 24"><path d="M20 6 9 17l-5-5"/></svg>`,
	"check-circle":   `<svg class="icon" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/><path d="m9 12 2 2 4-4"/></svg>`,
	"chevron-down":   `<svg class="icon" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" viewBox="0 0 24 24"><path d="m6 9 6 6 6-6"/></svg>`,
	"help-circle":    `<svg class="icon" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/><path d="M12 17h.01"/></svg>`,
	"info":           `<svg class="icon" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/></svg>`,
	"kebab":          `<svg class="icon" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" viewBox="0 0 24 24"><circle cx="12" cy="12" r="1"/><circle cx="12" cy="5" r="1"/><circle cx="12" cy="19" r="1"/></svg>`,
	"menu":           `<svg class="icon" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" viewBox="0 0 24 24"><path d="M4 12h16"/><path d="M4 6h16"/><path d="M4 18h16"/></svg>`,
	"plus":           `<svg class="icon" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" viewBox="0 0 24 24"><path d="M5 12h14"/><path d="M12 5v14"/></svg>`,
	"search":         `<svg class="icon" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" viewBox="0 0 24 24"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/></svg>`,
	"x":              `<svg class="icon" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" viewBox="0 0 24 24"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>`,
	"x-circle":       `<svg class="icon" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/><path d="m15 9-6 6"/><path d="m9 9 6 6"/></svg>`,
}

// Icon renders one icon, aria-hidden beside its visible label: a control
// whose only label is an icon needs an aria-label. An unknown slug
// renders nothing, which rastrillo generate --check catches.
func Icon(slug string) template.HTML { return icons[slug] }

// Assets is the <head> markup this delivery needs. Call it even when it
// is empty, so changing the delivery later needs no template edit.
func Assets() template.HTML { return "" }
