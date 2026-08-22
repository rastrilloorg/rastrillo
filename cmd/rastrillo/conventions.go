package main

import (
	"bytes"
	"fmt"
	"sort"
	"text/template"
)

// The UX convention profiles rastrillo can seed into a new app.
//
// A profile is a SEED, not a live binding: rastrillo new resolves it
// once and writes the resulting list into the app's AGENTS.md, and
// nothing ever re-reads the profile name again. Three things follow, all
// of them intended. An explicit flag that contradicts the profile wins,
// and the file records what actually happened, so the file never lies.
// Editing a line in that file is exactly as valid as having picked a
// different profile — which is what "override any line" has to mean to
// be real. And upgrading rastrillo cannot silently change a shipped
// app's UX; the cost is that an old app does not pick up conventions
// added since, which is the right way round.
//
// Conventions with a Component are enforced by something vendored; the
// rest are guidance an agent applies by hand. Keeping the two visibly
// distinct is the point: it is the honest gap between what rastrillo
// enforces and what it merely recommends.
type convention struct {
	Name      string
	Rule      string
	Component string // "" when nothing enforces it yet
}

type profile struct {
	Summary     string
	Conventions []convention
}

var profiles = map[string]profile{
	"considered": {
		Summary: "Accessible by construction, progressively enhanced, and never " +
			"dependent on JavaScript to be usable.\n\nThese are rastrillo's own " +
			"conventions, and the profile is named for what it does rather than " +
			"after anyone else's work. For wider reading on interface quality, " +
			"impeccable.style, the WAI-ARIA Authoring Practices and Inclusive " +
			"Components are all worth your time — none of them is endorsed by, " +
			"or authoritative for, this framework.",
		Conventions: []convention{
			{"Fields", "label, hint, help and error in one envelope, wired with aria-describedby", "ui/field, ui/field-select, ui/field-text"},
			{"Destructive actions", "their own confirm-page URL, never a modal", "ui/confirm-form"},
			{"State", "never colour alone — always a text label too", "ui/status-pill, ui/badge"},
			{"Dates", "relative, with the absolute date in a title attribute", ""},
			{"Errors", "server-rendered beside the field, in plain language", ""},
			{"Empty states", "say what the screen is for, not just that it is empty", "ui/empty-state"},
		},
	},
	"standard": {
		Summary: "Rastrillo's plain defaults. No added UX conventions beyond " +
			"what the shipped components already do.",
	},
}

func profileNames() []string {
	out := make([]string, 0, len(profiles))
	for name := range profiles {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// conventionsSection renders the "## UX conventions" block appended to
// the app's AGENTS.md.
//
// The icon choice is recorded whatever the profile: it is a fact about
// the app, not a convention the profile opted into.
func conventionsSection(profileName, iconSet, iconDelivery string) ([]byte, error) {
	p, ok := profiles[profileName]
	if !ok {
		return nil, fmt.Errorf("unknown UX profile %q (have %v)", profileName, profileNames())
	}
	data := struct {
		Profile      string
		Summary      string
		Conventions  []convention
		IconSet      string
		IconDelivery string
	}{profileName, p.Summary, p.Conventions, iconSet, iconDelivery}

	var buf bytes.Buffer
	if err := conventionsTmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var conventionsTmpl = template.Must(template.New("conventions").Parse(
	`
## UX conventions (seeded from profile: {{.Profile}})

{{.Summary}}

- [x] Icons — {{.IconSet}}, {{.IconDelivery}} — ` + "`internal/<app>/icons`" + `
{{- range .Conventions}}
{{if .Component}}- [x] {{.Name}} — {{.Rule}} — ` + "`{{.Component}}`" + `{{else}}- [ ] {{.Name}} — {{.Rule}}{{end}}
{{- end}}

` + "`[x]`" + ` is enforced by a vendored component: use it, do not hand-roll an
equivalent. ` + "`[ ]`" + ` is guidance to apply by hand. A convention absent from
this list is one rastrillo has no opinion about.

Edit any line. **This file is the source of truth, not the profile name.**
The profile only seeded this list at scaffold time; nothing re-reads it, so
changing a line here changes what the app does. A convention deleted from
this file is not a convention to reinstate.
`))

// claudeMDPointer is the whole of CLAUDE.md. The conventions live in
// AGENTS.md so they reach every agent rather than one, and nothing is
// duplicated here on purpose: two copies drift, and the drift is silent.
const claudeMDPointer = `@AGENTS.md

This project keeps its instructions in AGENTS.md so they apply to every
agent, not just Claude Code. Nothing is duplicated here on purpose: two
copies drift, and the drift is silent.
`
