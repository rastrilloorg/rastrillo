package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/generate"
	"amadan.net/rastrillo/rastrillo/internal/generate/apigen"
	"amadan.net/rastrillo/rastrillo/internal/generate/catalogs"
	"amadan.net/rastrillo/rastrillo/internal/generate/contract"
	"amadan.net/rastrillo/rastrillo/internal/generate/lint"
	"amadan.net/rastrillo/rastrillo/internal/generate/packs"
	"amadan.net/rastrillo/rastrillo/internal/generate/prune"
	"amadan.net/rastrillo/rastrillo/internal/manifest"
)

// defaultLocaleRemoved is Appendix C.2's op.generate.default_locale_removed.
const defaultLocaleRemoved = "--default-locale was removed: the default comes from default_locale in manifest/app.toml"

// runGenerate implements `rastrillo generate [flags] [dir]`: the
// one-shot generator rastrillo dev's watch loop and CI both call
// underneath (design doc §11) — one code path, not two.
//
// --check is the framework's half of `carlos vet` (§11): verify and
// report, write nothing. It covers route collisions (§4), the action
// build-tag convention (friction log F9), i18n catalog completeness
// (§10) and the parity lint over hand-written operations and door
// overrides (JSON API spec §7) today; the rest of §11's list arrives
// with the subsystems it checks.
//
// Flags come before the directory: FlagSet.Parse stops at the first
// non-flag argument, which is what keeps the older bare `rastrillo
// generate <dir>` form working unchanged.
func runGenerate(args []string) error {
	fset := flag.NewFlagSet("generate", flag.ContinueOnError)
	check := fset.Bool("check", false, "verify without writing (route collisions, action build tags, i18n catalog completeness, hand-written code on its operation)")
	acceptBreaking := fset.Bool("accept-breaking", false, "rewrite api/v1.json over a change that breaks /api/v1 clients; without it, generate refuses and leaves the file as it was")
	// Kept only to refuse: an undefined flag fails with Go's generic
	// "flag provided but not defined", which names no way out.
	fset.String("default-locale", "", defaultLocaleRemoved)
	if err := fset.Parse(args); err != nil {
		return err
	}
	removed := false
	fset.Visit(func(f *flag.Flag) { removed = removed || f.Name == "default-locale" })
	if removed {
		return errors.New(defaultLocaleRemoved)
	}

	dir := "."
	if rest := fset.Args(); len(rest) > 0 {
		dir = rest[0]
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	module, err := modulePath(dir)
	if err != nil {
		return err
	}
	app, err := manifest.LoadApp(filepath.Join(dir, "manifest"))
	if err != nil {
		return err
	}

	// actions/ is optional: a manifest-only app (design doc §9) may
	// declare every route through manifest/ and never have an actions/
	// directory at all. A missing dir means an empty hand-action set —
	// Discover itself would error walking a nonexistent path, so it's
	// skipped entirely rather than tolerated inside — everything
	// downstream (collision/skip machinery, ManifestActions, the router)
	// already treats "no hand actions" as the ordinary empty case.
	actionsDir := filepath.Join(dir, "actions")
	var actions []generate.Action
	var collisions []generate.Collision
	if _, err := os.Stat(actionsDir); err == nil {
		actions, collisions, err = generate.Discover(actionsDir)
		if err != nil {
			return fmt.Errorf("discover actions: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if len(collisions) > 0 {
		fmt.Fprintln(os.Stderr, "rastrillo generate: route collisions —")
		for _, c := range collisions {
			fmt.Fprintf(os.Stderr, "  %s claimed by:\n", c.Route)
			for _, s := range c.Sources {
				fmt.Fprintf(os.Stderr, "    actions/%s\n", s)
			}
		}
		return fmt.Errorf("%d route collision(s); build fails loudly on purpose (design doc §4)", len(collisions))
	}

	genDir := filepath.Join(dir, "gen")

	if *check {
		// Untagged action files don't break the app — plain generate
		// tolerates them, so iterating stays smooth — but they do break
		// every `go build ./...` in the tree (friction log F9), and the
		// raw go errors ("malformed import path", "Handle redeclared")
		// never mention the one-line fix. Fail here, where the message
		// can.
		untagged, err := generate.UntaggedActions(actionsDir, actions)
		if err != nil {
			return fmt.Errorf("build-tag check: %w", err)
		}
		if len(untagged) > 0 {
			fmt.Fprintln(os.Stderr, "rastrillo generate: action files missing the build constraint —")
			for _, s := range untagged {
				fmt.Fprintf(os.Stderr, "  actions/%s\n", s)
			}
			fmt.Fprintf(os.Stderr, "each needs `//go:build %s` (then a blank line) above its package clause,\n", generate.BuildTag)
			fmt.Fprintln(os.Stderr, "so `go build ./...` and friends skip generator input instead of failing on it")
			fmt.Fprintln(os.Stderr, "(a file that already carries a //go:build line needs it amended, never a second line)")
			return fmt.Errorf("%d action file(s) missing //go:build %s", len(untagged), generate.BuildTag)
		}

		// Packs first: a stale packs file is fixed by generate itself,
		// and a catalog the new locale needs is only worth naming after.
		if stale, err := packs.Stale(genDir, app); err != nil || stale != "" {
			if err == nil {
				err = errors.New(stale)
			}
			return err
		}
		problems, err := catalogs.Check(dir, app)
		if err != nil {
			return fmt.Errorf("i18n catalog check: %w", err)
		}
		if len(problems) > 0 {
			fmt.Fprintf(os.Stderr, "rastrillo generate: incomplete locale catalogs (default %q) —\n", app.DefaultLocale)
			for _, p := range problems {
				fmt.Fprintf(os.Stderr, "  %s\n", p)
			}
			return fmt.Errorf("%d locale catalog(s) incomplete; silent fallback while iterating, loud failure before ship (design doc §10)", len(problems))
		}

		// The committed API contract (JSON API spec §6), read-only here
		// like everything else: missing or stale fails. It runs before
		// the idempotency check below because after a refused generate
		// gen/ is always stale too, and "run generate" is advice the
		// break would refuse; naming the break sends the operator to
		// the manifest edit instead. Resources are loaded here, as the
		// write path does, because GenerateManifests keeps its own; the
		// parity lint further down reads the same load.
		manifests, rs, err := loadManifests(dir)
		if err != nil {
			return err
		}
		if bound, err := contractBinds(dir, manifests); err != nil {
			return err
		} else if bound {
			if err := contract.Check(dir, rs); err != nil {
				return err
			}
		}

		// Manifest resources (design doc §9): idempotency and
		// hand-vs-generated route collisions, all inside a temp dir —
		// a --check run never writes into gen/. A complete no-op when
		// the app declares no manifest resources at all.
		gm, err := generate.GenerateManifests(dir, genDir, true)
		if err != nil {
			return fmt.Errorf("manifest check: %w", err)
		}
		// cliWritten counts the hand actions' copies as this run's so
		// prune keeps them; whether each is current is its own question.
		stale, err := prune.Stale(genDir, append(gm.Written, cliWritten(genDir, actions)...), gm.ExclusiveStores)
		if err := errors.Join(err, prune.StaleError(stale), generate.StaleRewrites(actionsDir, genDir, actions)); err != nil {
			return err
		}

		// A template naming an icon nothing answers renders a hole in the
		// page. At run time that is deliberate — a typo must cost a
		// missing icon, not a crashed response — so this is where it gets
		// to be loud, the same posture as the catalog check above.
		unknownIcons, err := generate.UnknownIconSlugs(dir)
		if err != nil {
			return fmt.Errorf("icon slug check: %w", err)
		}
		if len(unknownIcons) > 0 {
			fmt.Fprintln(os.Stderr, "rastrillo generate: templates name icons nothing answers —")
			for _, ref := range unknownIcons {
				// Just the slug: it may have been written as {{icon "x"}}
				// or passed to a partial as "ActionIcon" "x", and quoting
				// back a form that is not on that line reads as a lie.
				fmt.Fprintf(os.Stderr, "  %s:%d  %q\n", ref.File, ref.Line, ref.Slug)
			}
			fmt.Fprintln(os.Stderr, "add the slug to the app's icons package, or fix the spelling")
			fmt.Fprintln(os.Stderr, "(at run time an unknown slug renders nothing on purpose; this is the pre-ship gate)")
			return fmt.Errorf("%d unknown icon slug(s)", len(unknownIcons))
		}

		// Hand-written code on its operation (JSON API spec §7): a hand
		// operation that skips op.Check is open to every caller, and a
		// hand action that overrides an exposed door without calling
		// its operation lets the page and the API drift apart. Neither
		// fails a build, so this is the only place they are refused.
		if manifests {
			findings, err := lint.Parity(dir, rs)
			if err != nil {
				return fmt.Errorf("parity check: %w", err)
			}
			if len(findings) > 0 {
				fmt.Fprintln(os.Stderr, "rastrillo generate: hand-written code is off its operation:")
				for _, f := range findings {
					fmt.Fprintf(os.Stderr, "  %s\n", f)
				}
				return fmt.Errorf("%d parity finding(s); the page and the API must do the same thing (JSON API spec §7)", len(findings))
			}
		}

		// Go↔JS parity vectors (vectors design §1.4): when the app has
		// a cmd/genvectors, the vectors check joins this gate —
		// regenerate-and-byte-compare plus the JS parity suite — so
		// there is one gate before ship, not two to remember. Absent
		// generator, absent gate: vectors are opt-in.
		if _, err := os.Stat(filepath.Join(dir, "cmd", "genvectors")); err == nil {
			if err := vectorsCheck(dir); err != nil {
				return fmt.Errorf("vectors check: %w", err)
			}
		} else if !os.IsNotExist(err) {
			return err
		}

		fmt.Printf("rastrillo generate --check: %d route(s), actions tagged, locale catalogs complete, icon slugs resolve, hand-written code on its operation\n", len(actions))
		return nil
	}

	// Manifest resources claim routes too (their own action files, not
	// discovered via Discover/Rewrite — see actions.go's package doc);
	// their Route/PackageName/GenDir fold into the same gen/router.go
	// Router builds for hand actions below, so a resource's generated
	// actions actually get served. GenerateManifests loads and
	// validates the manifest set itself; a manifest-less app (the
	// common case — every pre-manifest app has no manifest/ directory
	// at all) skips this Load entirely rather than paying for another
	// goEval `go run` on every generate.
	//
	// Loaded before the first write into gen/ because the API contract
	// is compared here (JSON API spec §6): a breaking change refuses
	// the whole run, and a run refused before it wrote anything leaves
	// nothing to revert but the manifest edit.
	var manifestActions []generate.Action
	booted := false // the router takes gen.Booted exactly when gen/boot.go exists
	manifests, rs, err := loadManifests(dir)
	if err != nil {
		return err
	}
	if bound, err := contractBinds(dir, manifests); err != nil {
		return err
	} else if bound {
		notes, err := contract.Write(dir, rs, *acceptBreaking)
		if err != nil {
			return err
		}
		for _, n := range notes {
			fmt.Println(n)
		}
	}
	if manifests {
		booted = len(rs) > 0
		for _, w := range manifest.IneligibleDefaults(rs) {
			fmt.Fprintln(os.Stderr, w)
		}
		manifestActions, err = generate.ManifestActions(actionsDir, rs)
		if err != nil {
			return err
		}
	}

	if err := os.RemoveAll(filepath.Join(genDir, "actions")); err != nil {
		return fmt.Errorf("clear stale generated actions: %w", err)
	}
	// gen/ops holds hand-written copies too (opsgen.Emit), and a copy
	// without the generated header is invisible to prune: once its
	// resource is gone it would outlive types.go and break the build.
	if err := os.RemoveAll(filepath.Join(genDir, "ops")); err != nil {
		return fmt.Errorf("clear stale generated operations: %w", err)
	}
	for _, a := range actions {
		if err := generate.Rewrite(actionsDir, genDir, a); err != nil {
			return fmt.Errorf("rewrite %s: %w", a.SourcePath, err)
		}
	}

	// Manifest resources (design doc §9): gen/manifest.json, the sqlc
	// store, generated templates/actions, and the locales fragment —
	// written straight into genDir. A complete no-op when the app
	// declares no manifest resources at all (manifests are optional,
	// per route).
	gm, err := generate.GenerateManifests(dir, genDir, false)
	if err != nil {
		return fmt.Errorf("generate manifests: %w", err)
	}
	for _, line := range append(append(gm.Appended, gm.Scoped...), gm.MoneySchemaAdded...) {
		fmt.Println(line)
	}
	if _, err := packs.Emit(genDir, app); err != nil {
		return err
	}

	allActions := append(append([]generate.Action(nil), actions...), manifestActions...)

	router, err := generate.Router(module, allActions, booted)
	if err != nil {
		return fmt.Errorf("render router.go: %w", err)
	}
	// genDir itself is only ever created as a side effect of Rewrite (a
	// hand action) or GenerateManifests' emitters (a manifest
	// resource) — an app with neither (no actions/ at all, or an empty
	// one, and no manifest/) reaches here with gen/ never having been
	// created.
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(genDir, "router.go"), router, 0o644); err != nil {
		return err
	}
	// Every app, manifests or not: a main.go that mounts
	// api.New(cfg, gen.APIRoutes()...) must keep compiling when the last
	// exposed resource is taken off the API, or when the app has no
	// manifest yet and is adding the API first.
	apiFile, err := apigen.File(module, rs)
	if err != nil {
		return fmt.Errorf("render api.go: %w", err)
	}
	if err := os.WriteFile(filepath.Join(genDir, "api.go"), apiFile, 0o644); err != nil {
		return err
	}

	// The tool registry used to be emitted here. An app upgrading from a
	// release that did still has gen/tools.go, which names a type the root
	// package no longer has and would fail its build; gen/ is wholly
	// generated, so remove it.
	if err := os.Remove(filepath.Join(genDir, "tools.go")); err != nil && !os.IsNotExist(err) {
		return err
	}

	pruned, err := prune.Prune(genDir, append(gm.Written, cliWritten(genDir, actions)...), gm.ExclusiveStores)
	for _, f := range pruned {
		fmt.Printf(prune.Pruned+"\n", f)
	}
	if err != nil {
		return err
	}

	fmt.Printf("rastrillo generate: %d route(s) wired\n", len(allActions))
	for _, a := range actions {
		fmt.Printf("  %-24s actions/%s\n", a.Route, a.SourcePath)
	}
	for _, a := range manifestActions {
		fmt.Printf("  %-24s %s\n", a.Route, a.SourcePath)
	}
	return nil
}

// loadManifests reports whether the app has a manifest/ directory and,
// if so, the resources it declares. A manifest-less app skips the load,
// which is another goEval `go run`, rather than paying for nothing.
func loadManifests(dir string) (bool, []rastrillo.Resource, error) {
	if _, err := os.Stat(filepath.Join(dir, "manifest")); os.IsNotExist(err) {
		return false, nil, nil
	} else if err != nil {
		return false, nil, err
	}
	rs, err := manifest.Load(dir, filepath.Join(dir, "manifest"))
	return true, rs, err
}

// contractBinds is whether api/v1.json is compared: with manifests, and
// also without them once a contract is committed. Keying on manifest/
// alone let deleting or renaming it skip the comparison, and the run
// then pruned every exposed resource's files with no word about the
// routes it took off /api/v1. No manifests is no resources, so every
// committed operation was removed: a break, refused like any other.
func contractBinds(dir string, manifests bool) (bool, error) {
	_, err := os.Stat(filepath.Join(dir, contract.Path))
	switch {
	case manifests || err == nil:
		return true, nil
	case os.IsNotExist(err):
		return false, nil
	}
	return false, err
}

// cliWritten is what runGenerate writes into genDir beside the manifest
// pipeline, changed or not, so pruning counts it as this run's.
func cliWritten(genDir string, actions []generate.Action) []string {
	out := append(packs.Paths(genDir), filepath.Join(genDir, "router.go"), filepath.Join(genDir, "api.go"), filepath.Join(genDir, "tools.go"))
	for _, a := range actions {
		out = append(out, generate.RewritePath(genDir, a))
	}
	return out
}
