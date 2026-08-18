package generate

import (
	"go/build"
	"os"
	"path/filepath"
	"testing"
)

func writeAction(t *testing.T, dir, rel, pkg string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	src := "package " + pkg + "\n\nimport (\n\t\"net/http\"\n\n\t\"github.com/carlosframework/rastrillo\"\n)\n\nfunc Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {\n\tw.Write([]byte(\"ok\"))\n}\n"
	if err := os.WriteFile(full, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverRoutes(t *testing.T) {
	dir := t.TempDir()
	writeAction(t, dir, "index.GET.go", "actions")
	writeAction(t, dir, "orders/[id]/cancel.POST.go", "actions")
	writeAction(t, dir, "orders/[id]/edit.GET.go", "actions")
	// A nested index (not the root) contributes its directory as a
	// path segment same as any other name would — only the root
	// index.GET.go gets the {$} anchor treatment.
	writeAction(t, dir, "orders/index.GET.go", "actions")

	actions, collisions, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(collisions) != 0 {
		t.Fatalf("unexpected collisions: %v", collisions)
	}
	want := map[string]string{
		"index.GET.go":               "GET /{$}",
		"orders/[id]/cancel.POST.go": "POST /orders/{id}/cancel",
		"orders/[id]/edit.GET.go":    "GET /orders/{id}/edit",
		"orders/index.GET.go":        "GET /orders",
	}
	if len(actions) != len(want) {
		t.Fatalf("got %d actions, want %d: %+v", len(actions), len(want), actions)
	}
	for _, a := range actions {
		route, ok := want[a.SourcePath]
		if !ok {
			t.Fatalf("unexpected action %+v", a)
		}
		if a.Route != route {
			t.Errorf("%s: route = %q, want %q", a.SourcePath, a.Route, route)
		}
	}
	// The two actions/orders/[id]/*.go files must not collide on
	// package name even though they share a directory.
	if actions[1].PackageName == actions[2].PackageName {
		t.Errorf("package name collision: %s", actions[1].PackageName)
	}
}

func TestDiscoverCollision(t *testing.T) {
	dir := t.TempDir()
	// Two different source files claiming the same route (one via a
	// literal segment, one via a same-named bracket dir) must be
	// reported, not silently picked between.
	writeAction(t, dir, "widgets.GET.go", "actions")
	writeAction(t, dir, "widgets/index.GET.go", "actions")

	_, collisions, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(collisions) != 1 {
		t.Fatalf("got %d collisions, want 1: %+v", len(collisions), collisions)
	}
	if collisions[0].Route != "GET /widgets" {
		t.Errorf("collision route = %q, want %q", collisions[0].Route, "GET /widgets")
	}
}

func TestIgnoresUnderscoreFiles(t *testing.T) {
	dir := t.TempDir()
	writeAction(t, dir, "index.GET.go", "actions")
	writeAction(t, dir, "_middleware.go", "actions")

	actions, _, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 {
		t.Fatalf("got %d actions, want 1 (_middleware.go should be skipped): %+v", len(actions), actions)
	}
}

func TestRewriteProducesUniquePackages(t *testing.T) {
	actionsDir := t.TempDir()
	genDir := t.TempDir()
	writeAction(t, actionsDir, "orders/[id]/cancel.POST.go", "actions")

	actions, _, err := Discover(actionsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 {
		t.Fatalf("got %d actions, want 1", len(actions))
	}
	if err := Rewrite(actionsDir, genDir, actions[0]); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(genDir, "actions", filepath.FromSlash(actions[0].GenDir), "cancel.POST.go")
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("expected generated file at %s: %v", out, err)
	}
	if got := string(b); !contains(got, "package "+actions[0].PackageName) {
		t.Errorf("generated file missing rewritten package clause:\n%s", got)
	}
}

// Regression test for a real bug caught at build time: Router wrote
// `import "<app module>"` where it meant the framework's own import
// path, so a generated router.go for module "helloworld" tried to
// import "helloworld" (itself, with no package) instead of
// "github.com/carlosframework/rastrillo" for the *rastrillo.Ctx type
// its own signature references.
func TestRouterImportsFrameworkNotAppModule(t *testing.T) {
	actions := []Action{{
		Route:       "GET /{$}",
		PackageName: "act_index_get",
		GenDir:      "index_get",
	}}
	out, err := Router("helloworld", actions)
	if err != nil {
		t.Fatal(err)
	}
	src := string(out)
	if !contains(src, `"github.com/carlosframework/rastrillo"`) {
		t.Errorf("router.go doesn't import the framework:\n%s", src)
	}
	if contains(src, "\"helloworld\"\n") {
		t.Errorf("router.go wrongly self-imports the app's own module:\n%s", src)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// writeTaggedAction is writeAction with a build constraint above the
// package clause — the shape rastrillo new scaffolds (F9).
func writeTaggedAction(t *testing.T, dir, rel, constraint string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	src := constraint + "\n\npackage actions\n\nimport (\n\t\"net/http\"\n\n\t\"github.com/carlosframework/rastrillo\"\n)\n\nfunc Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {\n\tw.Write([]byte(\"ok\"))\n}\n"
	if err := os.WriteFile(full, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The gen/ copy is the form that must compile, so the constraint that
// keeps the go tool off the original must not travel with it.
func TestRewriteStripsTheBuildConstraint(t *testing.T) {
	for _, tc := range []struct {
		name       string
		constraint string
	}{
		{"go:build", "//go:build " + BuildTag},
		{"go:build with legacy line", "//go:build " + BuildTag + "\n// +build " + BuildTag},
		{"explainer comment in the same group", "// generator input, never compiled in place\n//go:build " + BuildTag},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actionsDir := t.TempDir()
			genDir := t.TempDir()
			writeTaggedAction(t, actionsDir, "orders/[id]/cancel.POST.go", tc.constraint)

			actions, _, err := Discover(actionsDir)
			if err != nil {
				t.Fatal(err)
			}
			if err := Rewrite(actionsDir, genDir, actions[0]); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(genDir, "actions", filepath.FromSlash(actions[0].GenDir), "cancel.POST.go")
			b, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			got := string(b)
			if contains(got, "//go:build") || contains(got, "+build") {
				t.Errorf("build constraint survived into the gen copy:\n%s", got)
			}
			if !contains(got, "package "+actions[0].PackageName) {
				t.Errorf("gen copy missing rewritten package clause:\n%s", got)
			}
			if !contains(got, "func Handle") {
				t.Errorf("gen copy lost the Handle function:\n%s", got)
			}
		})
	}
}

// An untagged file still rewrites — existing apps keep working; only
// --check nags (see UntaggedActions).
func TestRewriteToleratesAnUntaggedFile(t *testing.T) {
	actionsDir := t.TempDir()
	genDir := t.TempDir()
	writeAction(t, actionsDir, "index.GET.go", "actions")

	actions, _, err := Discover(actionsDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := Rewrite(actionsDir, genDir, actions[0]); err != nil {
		t.Fatal(err)
	}
}

func TestUntaggedActions(t *testing.T) {
	actionsDir := t.TempDir()
	writeTaggedAction(t, actionsDir, "index.GET.go", "//go:build "+BuildTag)
	writeAction(t, actionsDir, "posts/index.GET.go", "actions")

	actions, _, err := Discover(actionsDir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UntaggedActions(actionsDir, actions)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "posts/index.GET.go" {
		t.Errorf("UntaggedActions = %v, want [posts/index.GET.go]", got)
	}
}

func TestSkippedByDefaultBuild(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want bool
	}{
		{"tagged", "//go:build " + BuildTag + "\n\npackage actions\n", true},
		{"untagged", "package actions\n", false},
		// The check evaluates the constraint, not the tag name: any
		// never-satisfied constraint earns the ./... skip, and a
		// negated tag names BuildTag yet compiles in every default
		// build — the exact breakage --check exists to catch.
		{"different but excluding tag", "//go:build ignore\n\npackage actions\n", true},
		{"negated tag still compiles by default", "//go:build !" + BuildTag + "\n\npackage actions\n", false},
		{"satisfied-by-default expression", "//go:build " + BuildTag + " || " + build.Default.GOOS + "\n\npackage actions\n", false},
		{"legacy plus-build line", "// +build " + BuildTag + "\n\npackage actions\n", true},
		{"tag after package clause does not count", "package actions\n\n//go:build " + BuildTag + "\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := SkippedByDefaultBuild([]byte(tc.src)); got != tc.want {
				t.Errorf("SkippedByDefaultBuild = %v, want %v", got, tc.want)
			}
		})
	}
}
