// The design-system tree (docs/design-system) is generated, not
// hand-edited. cmd/dsgen renders it; internal/designsystem's
// TestDesignSystemIsCurrent is the freshness gate: it fails the build if
// the committed tree drifts from what internal/designsystem.Render()
// produces, so run this after touching any ui partial, theme, or
// design-system page.
//
//go:generate go run ./cmd/dsgen -out docs/design-system
package rastrillo
