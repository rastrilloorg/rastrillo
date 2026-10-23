package opsgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/generate/scratch"
)

// handCreate is a hand-written create for the bookmarks fixture: the
// generated one with the title upper-cased, under a package name of
// the author's choosing, as spec §7 allows. It relies only on what
// types.go keeps: CreateInput, Bookmark, CreateOp, ValidateCreate and
// load.
const handCreate = `//go:build rastrillo_actions

package anything

import (
	"context"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/op"
	bookmarksstore "scratch/gen/store/bookmarks"
)

// Create stores the title upper-cased.
func Create(ctx context.Context, rc *rastrillo.Ctx, in CreateInput) (Bookmark, error) {
	if err := op.Check(ctx, rc, CreateOp); err != nil {
		return Bookmark{}, err
	}
	if errs := ValidateCreate(in); len(errs) > 0 {
		return Bookmark{}, op.Invalid{Fields: errs}
	}
	id, err := bookmarksstore.New(rc.DB).CreateBookmark(ctx, bookmarksstore.CreateBookmarkParams{
		Owner: rc.Actor.Subject,
		Title: strings.ToUpper(strings.TrimSpace(in.Title.Value)),
		Link:  strings.TrimSpace(in.Link.Value),
		Notes: in.Notes.Value,
		Now:   time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return Bookmark{}, err
	}
	return load(ctx, rc, id)
}
`

// TestHandFilesReplaceGeneratedOperations drives the CLI over a scratch
// app, as TestRuntime does, because the copy, --check's diff and the
// prune are wired together in cmd/rastrillo: a test of Emit alone would
// pass while the command pruned the copy or diffed it as drift.
func TestHandFilesReplaceGeneratedOperations(t *testing.T) {
	app := scratch.App(t, map[string]string{"manifest/bookmarks.toml": bookmarksTOML})
	scratch.Generate(t, app)
	ops := filepath.Join(app, "ops")
	genPath := filepath.Join(app, "gen", "ops", "bookmarks", "create.go")
	generated := readFile(t, genPath)

	t.Run("a tagged hand file replaces the generated one", func(t *testing.T) {
		defer os.RemoveAll(ops)
		scratch.Write(t, app, "ops/bookmarks/create.go", handCreate)
		scratch.Generate(t, app)
		got := readFile(t, genPath)
		if !strings.Contains(got, "strings.ToUpper") {
			t.Fatalf("gen/ops/bookmarks/create.go is not the hand file:\n%s", got)
		}
		if strings.Contains(got, "//go:build") {
			t.Error("the copy keeps the build constraint, so the compiler skips it too")
		}
		if !strings.HasPrefix(got, "package ops_bookmarks\n") {
			t.Errorf("the copy's package clause must be the generated package's, got:\n%s", got)
		}
		// The page doors call ValidateCreate, which stays in types.go
		// whatever replaces create.go; the build is what proves it.
		if out, err := scratch.GoBuild(app); err != nil {
			t.Fatalf("the app with a hand operation does not build:\n%s", out)
		}
		if err := scratch.Check(t, app); err != nil {
			t.Errorf("--check must treat the copy as the hand file's, not as drift: %v", err)
		}
	})

	t.Run("removing the hand file restores the generated one", func(t *testing.T) {
		scratch.Write(t, app, "ops/bookmarks/create.go", handCreate)
		scratch.Generate(t, app)
		if err := os.RemoveAll(ops); err != nil {
			t.Fatal(err)
		}
		scratch.Generate(t, app)
		if got := readFile(t, genPath); got != generated {
			t.Errorf("after the hand file is removed, create.go must be the generated one again, got:\n%s", got)
		}
	})

	t.Run("a copy of the generated file survives pruning", func(t *testing.T) {
		// An author starts from the generated file, header and all;
		// prune must not take the copy for an earlier run's leftover.
		defer os.RemoveAll(ops)
		scratch.Write(t, app, "ops/bookmarks/create.go", "//go:build rastrillo_actions\n\n"+generated)
		scratch.Generate(t, app)
		if _, err := os.Stat(genPath); err != nil {
			t.Fatalf("the copy was pruned: %v", err)
		}
		if err := scratch.Check(t, app); err != nil {
			t.Errorf("--check must not report the copy as stale: %v", err)
		}
	})

	t.Run("anything but the five operation files is refused", func(t *testing.T) {
		for _, c := range []struct{ rel, want string }{
			{"ops/bookmarks/types.go", "ops/bookmarks/types.go: only the five operation files can be replaced; types are the API contract"},
			{"ops/bookmarks/helpers.go", "ops/bookmarks/helpers.go: only the five operation files can be replaced"},
			{"ops/nope/create.go", `ops/nope: no manifest resource is named "nope"`},
			{"ops/stray.go", "ops/stray.go: a hand-written operation lives in ops/<resource>/<operation>.go"},
		} {
			scratch.Write(t, app, c.rel, handCreate)
			out, err := scratch.Run(app, scratch.CLI(t), "generate", app)
			if rmErr := os.RemoveAll(ops); rmErr != nil {
				t.Fatal(rmErr)
			}
			if err == nil {
				t.Errorf("%s: generate must refuse it", c.rel)
			} else if !strings.Contains(out, c.want) {
				t.Errorf("%s: the error must name the file and say what to do, got:\n%s", c.rel, out)
			}
		}
	})

	t.Run("a name without .go is refused, not ignored", func(t *testing.T) {
		// TrimSuffix(".go") alone let ops/bookmarks/create through
		// discovery; the copy then read create.go, found nothing, and
		// generated the default, so the author's file did nothing.
		for _, rel := range []string{"ops/bookmarks/create", "ops/bookmarks/create.go.bak"} {
			scratch.Write(t, app, rel, handCreate)
			out, err := scratch.Run(app, scratch.CLI(t), "generate", app)
			if rmErr := os.RemoveAll(ops); rmErr != nil {
				t.Fatal(rmErr)
			}
			if err == nil {
				t.Errorf("%s: generate must refuse it", rel)
			} else if !strings.Contains(out, rel+": only the five operation files can be replaced") {
				t.Errorf("%s: the error must name the file, got:\n%s", rel, out)
			}
		}
	})

	t.Run("a hand file edited after generate fails --check", func(t *testing.T) {
		// The copy under gen/ is what compiles; a --check that passed
		// with it stale would let the app ship the old operation.
		defer os.RemoveAll(ops)
		scratch.Write(t, app, "ops/bookmarks/create.go", handCreate)
		scratch.Generate(t, app)
		scratch.Write(t, app, "ops/bookmarks/create.go", strings.Replace(handCreate, "strings.ToUpper", "strings.ToLower", 1))
		err := scratch.Check(t, app)
		if err == nil {
			t.Fatal("--check must fail when gen/ops holds a stale copy of the hand file")
		}
		if !strings.Contains(err.Error(), "gen/ops/bookmarks/create.go") {
			t.Errorf("--check's error must name the stale copy, got:\n%v", err)
		}
		scratch.Generate(t, app)
		if err := scratch.Check(t, app); err != nil {
			t.Errorf("after a regenerate --check must pass again: %v", err)
		}
	})

	t.Run("an untagged hand file fails --check and names the fix", func(t *testing.T) {
		defer os.RemoveAll(ops)
		scratch.Write(t, app, "ops/bookmarks/create.go", strings.TrimPrefix(handCreate, "//go:build rastrillo_actions\n\n"))
		// generate itself tolerates the missing tag, as it does for an
		// action, so iterating stays smooth; --check is the gate.
		scratch.Generate(t, app)
		err := scratch.Check(t, app)
		if err == nil {
			t.Fatal("--check must fail on an untagged hand operation")
		}
		for _, want := range []string{"ops/bookmarks/create.go", "//go:build rastrillo_actions"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("--check's error must mention %q, got:\n%v", want, err)
			}
		}
	})
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
