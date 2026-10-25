// Package generate's manifest orchestrator (this file) is the one
// entry point cmd/rastrillo calls to turn a moduleRoot's declared
// manifests into a generated tree: load, render the JSON artifact,
// run every emitter Tasks 5-9 built, run sqlc, and — in check-only
// mode — verify idempotency and route collisions without touching the
// tree at all. GenerateManifests is a complete no-op when the app
// declares no manifest resources at all (manifests are optional, per
// route — internal/manifest.Load's own doc comment): an app that
// hasn't adopted the manifest system sees no change in behavior.
package generate

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/generate/apigen"
	"amadan.net/rastrillo/rastrillo/internal/generate/bootgen"
	"amadan.net/rastrillo/rastrillo/internal/generate/opsgen"
	"amadan.net/rastrillo/rastrillo/internal/generate/packs"
	"amadan.net/rastrillo/rastrillo/internal/generate/paritygen"
	"amadan.net/rastrillo/rastrillo/internal/manifest"
)

// Generated is what one GenerateManifests run produced, for pruning.
type Generated struct {
	// Written is every file the run emitted, changed or not, named
	// under genDir even in check-only mode, where it wrote elsewhere.
	Written []string
	// ExclusiveStores are the store directories sqlc writes into this
	// run; sqlc output anywhere else is stale.
	ExclusiveStores []string
	// Appended is one op.generate.migration_appended line per statement
	// this run added to a released migrations.go. Empty in check-only
	// mode, which writes nothing the app keeps.
	Appended []string
	// Scoped is one line per resource that the previous generation had
	// unscoped and this one has user-scoped (scopechange.go), whichever
	// store it keeps. Empty in check-only mode, like Appended.
	Scoped []string
	// MoneySchemaAdded is one op.generate.money_schema_added line per
	// resource whose events carry money, when this run added the event
	// log's rename to gen/boot.go. Empty in check-only mode, whose
	// boot.go is written to a temp dir and so always looks new.
	MoneySchemaAdded []string
}

// GenerateManifests is the one entry cmd/rastrillo calls: load,
// artifact → gen/manifest.json, all emitters, sqlc, then the checks.
// check-only mode runs everything into a temp dir and diffs against
// gen/ (idempotency + collision without touching the tree).
func GenerateManifests(moduleRoot, genDir string, checkOnly bool) (Generated, error) {
	rs, err := manifest.Load(moduleRoot, filepath.Join(moduleRoot, "manifest"))
	if err != nil {
		return Generated{}, err
	}
	// Before the no-resource return: an ops/<name>/ with no resource is
	// refused whether the app has other resources or none, and only
	// --check nags about a missing build tag, as it does for actions.
	hand, err := opsgen.HandFiles(moduleRoot, rs)
	if err != nil {
		return Generated{}, err
	}
	if checkOnly {
		if err := opsgen.CheckTagged(moduleRoot, hand); err != nil {
			return Generated{}, err
		}
	}
	if len(rs) == 0 {
		return Generated{}, nil
	}
	var out Generated
	for _, r := range rs {
		if r.Store != rastrillo.Mergeable {
			out.ExclusiveStores = append(out.ExclusiveStores, filepath.Join(genDir, "store", r.Name))
		}
	}

	collisions, err := routeCollisions(filepath.Join(moduleRoot, "actions"), rs)
	if err != nil {
		return Generated{}, err
	}
	if len(collisions) > 0 {
		return Generated{}, fmt.Errorf("%s%d route collision(s); build fails loudly on purpose (design doc §4)",
			FormatCollisions(collisions), len(collisions))
	}

	// Read once, from the real tree, before choosing where to write:
	// check-only mode writes into an empty temp dir and must emit from
	// the same released statements generate does.
	h, err := ReadHistory(genDir, rs)
	if err != nil {
		return Generated{}, err
	}

	if !checkOnly {
		// Before the pipeline overwrites gen/manifest.json: the artifact
		// is what the last run recorded, and the comparison is only a
		// transition while it is still the old one.
		previous, err := previousGeneration(genDir)
		if err != nil {
			return Generated{}, err
		}
		out.Scoped = scopeNotes(previous, rs)
		out.Written, out.Appended, out.MoneySchemaAdded, err = emitPipeline(moduleRoot, genDir, rs, h, true)
		return out, err
	}

	tmp, err := os.MkdirTemp("", "rastrillo-manifest-check-")
	if err != nil {
		return Generated{}, err
	}
	defer os.RemoveAll(tmp)

	tmpPaths, _, _, err := emitPipeline(moduleRoot, tmp, rs, h, false)
	if err != nil {
		return Generated{}, err
	}
	for _, p := range tmpPaths {
		rel, err := filepath.Rel(tmp, p)
		if err != nil {
			return Generated{}, err
		}
		out.Written = append(out.Written, filepath.Join(genDir, rel))
	}
	return out, diffGenerated(tmp, genDir, tmpPaths)
}

// emitPipeline runs rs through every emitter into genDir: gen/manifest.json,
// EmitStore, (optionally) sqlc, then per-resource EmitTemplates/
// opsgen.Emit/EmitActions — detecting a computed-path collision between two
// DIFFERENT resources as it goes (Name/Route uniqueness is already
// enforced by manifest.Load, but genDirFor's sanitizeIdent can still
// collapse two distinct valid routes, e.g. "/foo-bar" and "/foo_bar",
// onto the same gen/actions/ leaf directory — the one case Load's own
// uniqueness checks cannot catch) — then EmitLocales. runSqlc is false
// for check-only's temp-dir dry run: sqlc's own compiled output
// (queries.sql.go, models.go, db.go) is not part of what these
// emitters themselves produce, so diffing it would flag stale/absent
// sqlc output as a false idempotency failure, and it would force
// network/tool access on every --check. Returns every WRITTEN path
// (never a skipped one — a skip means a hand file already owns that
// path, so there is nothing of the orchestrator's own to compare
// there) for the idempotency check to diff against a second run. A
// hand operation's copy under gen/ops (opsgen.Emit's skipped) is
// written all the same: the orchestrator wrote it, it may carry the
// generated header and so must count as this run's or prune removes
// it, and the diff is what catches a hand file edited since the last
// generate, when the app would otherwise run the old copy.
//
// h is the released migration statements, read from the app's real
// gen/ tree whatever genDir is; appended is EmitStore's, notices
// bootgen.Emit's.
func emitPipeline(moduleRoot, genDir string, rs []rastrillo.Resource, h History, runSqlc bool) (written, appended, notices []string, err error) {
	storePaths, appended, err := EmitStore(genDir, rs, h)
	if err != nil {
		return nil, nil, nil, err
	}
	written = append(written, storePaths...)

	// RunSqlc runs only when at least one exclusive resource exists: a
	// module of only-mergeable resources has no sqlc.yaml (EmitStore
	// skips it) and needs no sqlc tool at all.
	if runSqlc && hasExclusiveStore(rs) {
		if err := RunSqlc(moduleRoot); err != nil {
			return nil, nil, nil, err
		}
	}

	owner := map[string]string{} // computed gen path -> resource name
	claim := func(paths []string, resourceName string) error {
		for _, p := range paths {
			if prev, ok := owner[p]; ok && prev != resourceName {
				return fmt.Errorf("generated file collision: %s claimed by resources %q and %q", p, prev, resourceName)
			}
			owner[p] = resourceName
		}
		return nil
	}

	for _, r := range rs {
		tw, _, err := EmitTemplates(moduleRoot, genDir, r)
		if err != nil {
			return nil, nil, nil, err
		}
		if err := claim(tw, r.Name); err != nil {
			return nil, nil, nil, err
		}
		written = append(written, tw...)

		ow, hand, err := opsgen.Emit(moduleRoot, genDir, r)
		if err != nil {
			return nil, nil, nil, err
		}
		if err := claim(append(ow, hand...), r.Name); err != nil {
			return nil, nil, nil, err
		}
		written = append(written, ow...)
		written = append(written, hand...)

		dw, err := apigen.Emit(moduleRoot, genDir, r)
		if err != nil {
			return nil, nil, nil, err
		}
		if err := claim(dw, r.Name); err != nil {
			return nil, nil, nil, err
		}
		written = append(written, dw...)

		aw, _, err := EmitActions(moduleRoot, genDir, r)
		if err != nil {
			return nil, nil, nil, err
		}
		if err := claim(aw, r.Name); err != nil {
			return nil, nil, nil, err
		}
		written = append(written, aw...)
	}

	// After every resource's doors: the parity test calls both, and an
	// app with nothing exposed gets no package, so prune removes the
	// one an earlier run wrote.
	pw, err := paritygen.Emit(genDir, rs)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := claim(pw, "paritytest"); err != nil {
		return nil, nil, nil, err
	}
	written = append(written, pw...)

	if err := EmitLocales(genDir, "en", rs); err != nil {
		return nil, nil, nil, err
	}
	written = append(written,
		filepath.Join(genDir, "locales", "en.toml"),
		filepath.Join(genDir, "locales", "locales.go"))

	events, err := eventStores(moduleRoot, rs)
	if err != nil {
		return nil, nil, nil, err
	}
	boot, notices, err := bootgen.Emit(genDir, rs, sqlMoneyTables(rs), events)
	if err != nil {
		return nil, nil, nil, err
	}
	written = append(written, boot...)

	// The artifact goes last: it is what the next run reads as the
	// previous generation (scopechange.go), so a run that fails in
	// sqlc or a later emitter must leave the old one, or the retry
	// compares scoped with scoped and the warning is lost for good.
	// Nothing above reads it; the CLI's prune does, after this returns.
	manifestPath := filepath.Join(genDir, "manifest.json")
	if err := writeFileIfChanged(manifestPath, manifest.Artifact(rs)); err != nil {
		return nil, nil, nil, fmt.Errorf("manifest.json: %w", err)
	}
	return append(written, manifestPath), appended, notices, nil
}

// eventStores is Boot's replay check: every mergeable store, sorted by
// name, and which carry money, which decides the event-log upgrade.
// That a store carries money is declared by its manifest, never found
// in the data (spec §8).
func eventStores(moduleRoot string, rs []rastrillo.Resource) ([]bootgen.EventStore, error) {
	module, err := actionsModulePath(moduleRoot)
	if err != nil {
		return nil, err
	}
	var out []bootgen.EventStore
	for _, r := range rs {
		if r.Store != rastrillo.Mergeable {
			continue
		}
		alias, path := storeImport(r, module)
		out = append(out, bootgen.EventStore{Name: r.Name, Alias: alias, Path: path, Money: len(mergeableMoney(r)) > 0})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// sqlMoneyTables is Boot's SQL money upgrade: every exclusive store
// with money, sorted by name so a regeneration writes the same file,
// its money fields by the column names columns(r) gives the queries.
func sqlMoneyTables(rs []rastrillo.Resource) []bootgen.Table {
	sorted := append([]rastrillo.Resource(nil), rs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	var out []bootgen.Table
	for _, r := range sorted {
		if r.Store == rastrillo.Mergeable {
			continue
		}
		var fields []string
		for _, c := range columns(r) {
			if c.Kind == rastrillo.Money {
				fields = append(fields, c.SQL)
			}
		}
		if len(fields) > 0 {
			out = append(out, bootgen.Table{Name: r.Name, Fields: fields, Legacy: r.Legacy})
		}
	}
	return out
}

// hasExclusiveStore reports whether any resource uses the exclusive
// (sqlc) store — the only case sqlc.yaml exists and RunSqlc has
// anything to do.
func hasExclusiveStore(rs []rastrillo.Resource) bool {
	for _, r := range rs {
		if r.Store != rastrillo.Mergeable {
			return true
		}
	}
	return false
}

// manifestRoute is one resource action spec's computed identity: the
// exact actions/-relative path EmitActions would also use for that
// same file (relSource — the path a hand file must occupy for
// EmitActions' own file-level skip to apply, see its doc comment),
// its route, and the Route/PackageName/GenDir a router entry needs.
type manifestRoute struct {
	resourceName string
	relSource    string // e.g. "admin/notes/index.GET.go" — same shape Discover's Action.SourcePath uses
	route        string
	packageName  string
	genDir       string
}

// manifestRoutes enumerates every resource's up-to-seven action specs
// via the shared actionSpecs (so this list and EmitActions' own cannot
// drift apart) and computes each one's route/relSource/packageName/
// genDir. It does not consider hand files at all — ManifestActions and
// routeCollisions each apply the file-level-skip exemption on top of
// this raw list for their own purposes.
func manifestRoutes(rs []rastrillo.Resource) ([]manifestRoute, error) {
	var out []manifestRoute
	for _, r := range rs {
		for _, s := range actionSpecs(r) {
			route, err := routeFor(s.dir, s.name, s.method)
			if err != nil {
				return nil, fmt.Errorf("resource %q: %w", r.Name, err)
			}
			base := s.name + "." + s.method + ".go"
			relSource := s.dir + "/" + base
			out = append(out, manifestRoute{
				resourceName: r.Name,
				relSource:    relSource,
				route:        route,
				packageName:  packageNameFor(relSource),
				genDir:       genDirFor(s.dir, s.name, s.method),
			})
		}
	}
	return out, nil
}

// handRelSources returns the actions/-relative paths (Action.SourcePath's
// own shape) of every hand-written action file under actionsDir, or nil
// without error when actionsDir doesn't exist at all (a manifest-only
// app may have none).
func handRelSources(actionsDir string) (map[string]bool, error) {
	if _, err := os.Stat(actionsDir); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	hand, _, err := Discover(actionsDir)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(hand))
	for _, a := range hand {
		set[a.SourcePath] = true
	}
	return set, nil
}

// ManifestActions synthesizes the Action entries each resource's
// generated files claim, for every spec EXCEPT one whose exact
// conventional relSource is already occupied by a hand file — the
// design doc §4 file-level rule EmitActions itself implements ("a
// hand-written actions/<same path> file already present under appRoot
// skips generating that one file"). Excluding it here too matters, not
// just cosmetically: EmitActions never writes anything at that spec's
// GenDir, so a router entry pointing there would import a package that
// doesn't exist; the app's own hand action (already in Discover's own
// result) is what actually serves that route. cmd/rastrillo/generate.go
// folds the result into the same gen/router.go Router already builds
// for hand actions; routeCollisions folds it into the same
// route-collision check Discover already runs across hand actions.
// SourcePath is a label, not a real actions/ file — used only to name
// the resource in a collision message or a route listing.
func ManifestActions(actionsDir string, rs []rastrillo.Resource) ([]Action, error) {
	skip, err := handRelSources(actionsDir)
	if err != nil {
		return nil, err
	}
	routes, err := manifestRoutes(rs)
	if err != nil {
		return nil, err
	}

	var out []Action
	for _, mr := range routes {
		if skip[mr.relSource] {
			continue
		}
		out = append(out, Action{
			SourcePath:  fmt.Sprintf("manifest:%s (%s)", mr.resourceName, mr.relSource),
			Route:       mr.route,
			PackageName: mr.packageName,
			GenDir:      mr.genDir,
		})
	}
	return out, nil
}

// routeCollisions merges the app's hand-written actions/ discoveries
// (if actionsDir exists at all) with the routes each resource's own
// action emitter will produce — EXCLUDING any spec ManifestActions
// itself excludes (the file-level-skip case: same computed FILE path
// hand vs generated is an allowed override, design doc §4, not a
// collision) — and reports any route claimed by more than one
// remaining source. Reuses Discover's own Collision type and byRoute
// grouping, so a hand-vs-manifest collision prints exactly the same
// shape as a hand-vs-hand one.
func routeCollisions(actionsDir string, rs []rastrillo.Resource) ([]Collision, error) {
	var hand []Action
	if _, err := os.Stat(actionsDir); err == nil {
		var derr error
		hand, _, derr = Discover(actionsDir)
		if derr != nil {
			return nil, derr
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	manifestActions, err := ManifestActions(actionsDir, rs)
	if err != nil {
		return nil, err
	}

	byRoute := map[string][]string{}
	for _, a := range hand {
		byRoute[a.Route] = append(byRoute[a.Route], "actions/"+a.SourcePath)
	}
	for _, a := range manifestActions {
		byRoute[a.Route] = append(byRoute[a.Route], a.SourcePath)
	}

	var collisions []Collision
	for route, sources := range byRoute {
		if len(sources) > 1 {
			sort.Strings(sources)
			collisions = append(collisions, Collision{Route: route, Sources: sources})
		}
	}
	sort.Slice(collisions, func(i, j int) bool { return collisions[i].Route < collisions[j].Route })
	return collisions, nil
}

// FormatCollisions renders collisions the way design doc §4's "build
// fails loudly on any collision" is reported: one line naming the
// route, then every claiming source indented beneath it.
func FormatCollisions(collisions []Collision) string {
	var b strings.Builder
	b.WriteString("route collisions —\n")
	for _, c := range collisions {
		fmt.Fprintf(&b, "  %s claimed by:\n", c.Route)
		for _, s := range c.Sources {
			fmt.Fprintf(&b, "    %s\n", s)
		}
	}
	return b.String()
}

// diffGenerated byte-compares every path in tmpPaths (rooted at
// tmpDir) against its counterpart under genDir (the real, on-disk
// tree), reporting missing (expected but absent) and differing
// (present but hand-edited since generation) files. Extra-file
// detection is deliberately scoped to gen/locales/ only — the one
// subtree this orchestrator can safely walk in full: gen/store/<name>/
// also holds sqlc's own compiled output (queries.sql.go, models.go,
// db.go), and gen/actions/ is shared with hand-rewritten actions, so a
// full-tree walk of either would flag legitimate, unrelated files as
// "extra". See the task report for this scope note.
func diffGenerated(tmpDir, genDir string, tmpPaths []string) error {
	var missing, differing []string
	for _, tp := range tmpPaths {
		rel, err := filepath.Rel(tmpDir, tp)
		if err != nil {
			return err
		}
		want, err := os.ReadFile(tp)
		if err != nil {
			return err
		}
		got, err := os.ReadFile(filepath.Join(genDir, rel))
		if os.IsNotExist(err) {
			missing = append(missing, rel)
			continue
		}
		if err != nil {
			return err
		}
		if !bytes.Equal(want, got) {
			differing = append(differing, rel)
		}
	}

	extra, err := extraLocaleFiles(tmpDir, genDir)
	if err != nil {
		return err
	}

	if len(missing) == 0 && len(differing) == 0 && len(extra) == 0 {
		return nil
	}

	sort.Strings(missing)
	sort.Strings(differing)
	sort.Strings(extra)

	var b strings.Builder
	b.WriteString("generated tree is not idempotent —\n")
	if len(missing) > 0 {
		b.WriteString("  missing:\n")
		for _, f := range missing {
			fmt.Fprintf(&b, "    gen/%s\n", f)
		}
	}
	if len(extra) > 0 {
		b.WriteString("  extra:\n")
		for _, f := range extra {
			fmt.Fprintf(&b, "    gen/%s\n", f)
		}
	}
	if len(differing) > 0 {
		b.WriteString("  differing:\n")
		for _, f := range differing {
			fmt.Fprintf(&b, "    gen/%s\n", f)
		}
	}
	return errors.New(b.String())
}

// extraLocaleFiles returns the basenames present in genDir/locales but
// absent from tmpDir/locales. Within this pipeline only EmitLocales
// writes there, so a full listing comparison is safe; the packs files
// beside its output are internal/generate/packs', checked by its own
// Stale, and counting them here would fail every app's --check.
func extraLocaleFiles(tmpDir, genDir string) ([]string, error) {
	want, err := localeFileSet(filepath.Join(tmpDir, "locales"))
	if err != nil {
		return nil, err
	}
	got, err := localeFileSet(filepath.Join(genDir, "locales"))
	if err != nil {
		return nil, err
	}
	var extra []string
	for name := range got {
		if !want[name] && !packs.Owns(name) {
			extra = append(extra, filepath.Join("locales", name))
		}
	}
	return extra, nil
}

func localeFileSet(dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			out[e.Name()] = true
		}
	}
	return out, nil
}
