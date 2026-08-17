package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/carlosframework/rastrillo/internal/generate"
)

// runGenerate implements `rastrillo generate [flags] [dir]`: the
// one-shot generator rastrillo dev's watch loop and CI both call
// underneath (design doc §11) — one code path, not two.
//
// --check is the framework's half of `carlos vet` (§11): verify and
// report, write nothing. It covers route collisions (§4), the action
// build-tag convention (friction log F9), and i18n catalog completeness
// (§10) today; the rest of §11's list arrives with the subsystems it
// checks.
//
// Flags come before the directory: FlagSet.Parse stops at the first
// non-flag argument, which is what keeps the older bare `rastrillo
// generate <dir>` form working unchanged.
func runGenerate(args []string) error {
	fset := flag.NewFlagSet("generate", flag.ContinueOnError)
	check := fset.Bool("check", false, "verify without writing (route collisions, action build tags, i18n catalog completeness)")
	defaultLocale := fset.String("default-locale", "en", "locale every other catalog is checked against (design doc §10)")
	if err := fset.Parse(args); err != nil {
		return err
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

	actionsDir := filepath.Join(dir, "actions")
	if _, err := os.Stat(actionsDir); os.IsNotExist(err) {
		return fmt.Errorf("no actions/ directory in %s", dir)
	}

	actions, _, err := generate.Discover(actionsDir)
	if err != nil {
		return fmt.Errorf("discover actions: %w", err)
	}

	specs, err := generate.LoadManifests(filepath.Join(dir, "manifest"))
	if err != nil {
		return fmt.Errorf("load manifests: %w", err)
	}
	manifestActions, skipped, err := generate.ManifestActions(module, actionsDir, specs)
	if err != nil {
		return fmt.Errorf("manifest actions: %w", err)
	}

	all := append(append([]generate.Action{}, actions...), actionsOf(manifestActions)...)
	sort.Slice(all, func(i, j int) bool { return all[i].Route < all[j].Route })

	tools, err := generate.CollectTools(actionsDir, actions)
	if err != nil {
		return fmt.Errorf("collect tools: %w", err)
	}

	collisions := generate.FindCollisions(all)
	if len(collisions) > 0 {
		fmt.Fprintln(os.Stderr, "rastrillo generate: route collisions —")
		for _, c := range collisions {
			fmt.Fprintf(os.Stderr, "  %s claimed by:\n", c.Route)
			for _, s := range c.Sources {
				if strings.HasPrefix(s, "manifest:") {
					fmt.Fprintf(os.Stderr, "    %s (generated screen)\n", s)
				} else {
					fmt.Fprintf(os.Stderr, "    actions/%s\n", s)
				}
			}
		}
		return fmt.Errorf("%d route collision(s); build fails loudly on purpose (design doc §4)", len(collisions))
	}

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

		missing, err := generate.MissingKeys(filepath.Join(dir, "locales"), *defaultLocale)
		if err != nil {
			return fmt.Errorf("i18n catalog check: %w", err)
		}
		if len(missing) > 0 {
			codes := make([]string, 0, len(missing))
			for code := range missing {
				codes = append(codes, code)
			}
			sort.Strings(codes)
			fmt.Fprintf(os.Stderr, "rastrillo generate: incomplete locale catalogs (default %q) —\n", *defaultLocale)
			for _, code := range codes {
				fmt.Fprintf(os.Stderr, "  locales/%s.toml is missing:\n", code)
				for _, key := range missing[code] {
					fmt.Fprintf(os.Stderr, "    %s\n", key)
				}
			}
			return fmt.Errorf("%d locale catalog(s) incomplete; silent fallback while iterating, loud failure before ship (design doc §10)", len(missing))
		}

		// The agent-gate check (§13's buildable half): a write tool with
		// no Confirm sentence is an action an agent could execute with
		// no consent sentence to show — refused before it ships, not
		// discovered after.
		if unconfirmed := generate.UnconfirmedWriteTools(tools); len(unconfirmed) > 0 {
			fmt.Fprintln(os.Stderr, "rastrillo generate: write tools missing a Confirm sentence —")
			for _, id := range unconfirmed {
				fmt.Fprintf(os.Stderr, "  %s\n", id)
			}
			fmt.Fprintln(os.Stderr, "every rastrillo.ToolWrite needs Confirm: the sentence a person sees before the write runs (design doc §8)")
			return fmt.Errorf("%d write tool(s) without consent sentences", len(unconfirmed))
		}

		fmt.Printf("rastrillo generate --check: %d route(s) (%d from manifests, %d taken over by hand), %d tool(s), actions tagged, locale catalogs complete\n",
			len(all), len(manifestActions), len(skipped), len(tools))
		return nil
	}

	genDir := filepath.Join(dir, "gen")
	if err := os.RemoveAll(filepath.Join(genDir, "actions")); err != nil {
		return fmt.Errorf("clear stale generated actions: %w", err)
	}
	for _, a := range actions {
		if err := generate.Rewrite(actionsDir, genDir, a); err != nil {
			return fmt.Errorf("rewrite %s: %w", a.SourcePath, err)
		}
	}
	for _, ma := range manifestActions {
		outDir := filepath.Join(genDir, "actions", filepath.FromSlash(ma.GenDir))
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return err
		}
		base := ma.SourcePath[strings.LastIndex(ma.SourcePath, "/")+1:] + ".go"
		if err := os.WriteFile(filepath.Join(outDir, base), ma.Content, 0o644); err != nil {
			return err
		}
	}

	manifestGenDir := filepath.Join(genDir, "manifest")
	if len(specs) > 0 {
		pkg, err := generate.ManifestPackage(module, specs)
		if err != nil {
			return fmt.Errorf("render gen/manifest: %w", err)
		}
		if err := os.MkdirAll(manifestGenDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(manifestGenDir, "manifest.go"), pkg, 0o644); err != nil {
			return err
		}
	} else if err := os.RemoveAll(manifestGenDir); err != nil {
		return fmt.Errorf("clear stale gen/manifest: %w", err)
	}

	router, err := generate.Router(module, all)
	if err != nil {
		return fmt.Errorf("render router.go: %w", err)
	}
	if err := os.WriteFile(filepath.Join(genDir, "router.go"), router, 0o644); err != nil {
		return err
	}

	toolsFile, err := generate.ToolsFile(tools)
	if err != nil {
		return fmt.Errorf("render tools.go: %w", err)
	}
	if err := os.WriteFile(filepath.Join(genDir, "tools.go"), toolsFile, 0o644); err != nil {
		return err
	}

	fmt.Printf("rastrillo generate: %d route(s) wired\n", len(all))
	for _, a := range all {
		if strings.HasPrefix(a.SourcePath, "manifest:") {
			fmt.Printf("  %-24s %s (generated screen)\n", a.Route, a.SourcePath)
		} else {
			fmt.Printf("  %-24s actions/%s\n", a.Route, a.SourcePath)
		}
	}
	for _, s := range skipped {
		fmt.Printf("  taken over by hand: actions/%s\n", s)
	}
	return nil
}

// actionsOf projects ManifestActions to their routing halves.
func actionsOf(mas []generate.ManifestAction) []generate.Action {
	out := make([]generate.Action, len(mas))
	for i, ma := range mas {
		out[i] = ma.Action
	}
	return out
}
