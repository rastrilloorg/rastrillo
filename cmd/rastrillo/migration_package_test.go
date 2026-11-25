package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// twoApps writes a module whose internal/ holds blog and wiki, both
// declaring everything the loader needs, so detection alone refuses;
// internal/apppackage's own tests cover detection itself.
func twoApps(t *testing.T) string {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module demo\n")
	for _, name := range []string{"blog", "wiki"} {
		mustWrite(t, filepath.Join(dir, "internal", name, "app.go"),
			"package "+name+"\n\nvar Schema, BootSchema, Models = 1, 2, 3\n")
	}
	return dir
}

func TestAppPackageFindsTheScaffoldWithNoFlag(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(cwd) })
	os.Chdir(dir)
	if err := runNew([]string{"stubapp"}); err != nil {
		t.Fatal(err)
	}
	importPath, pkg, err := appPackage(filepath.Join(dir, "stubapp"), "",
		migrationNeeds)
	if err != nil || pkg != "stubapp" ||
		!strings.HasSuffix(importPath, "/internal/stubapp") {
		t.Errorf("found %s (%s), %v; want stubapp", pkg, importPath, err)
	}
}

// migration new needs no compile, so it shows the flag reaching the
// directory it writes into, and that it may follow the name.
func TestMigrationNewWritesIntoThePackageTheFlagNames(t *testing.T) {
	dir := twoApps(t)
	err := runMigration([]string{"new", "rename_title", dir, "-package",
		"wiki"})
	if err != nil {
		t.Fatal(err)
	}
	written := filepath.Join(dir, "internal", "wiki", "migrations",
		"0001_rename_title.sql")
	if _, err := os.Stat(written); err != nil {
		t.Errorf("no 0001_rename_title.sql under internal/wiki: %v", err)
	}
}

// Every migration subcommand takes -package, before or after the
// directory, and refuses a name that is not there before compiling.
func TestEveryMigrationSubcommandTakesThePackageFlag(t *testing.T) {
	dir := twoApps(t)
	for _, args := range [][]string{
		{"check", "-package", "nosuch", dir},
		{"check", dir, "-package", "nosuch"},
		{"generate", "-package", "nosuch", dir},
		{"generate", dir, "--allow-destructive", "-package", "nosuch"},
		{"new", "rename_x", "-package", "nosuch", dir},
		{"new", "rename_x", dir, "-package", "nosuch"},
		{"status", "--db", "x.db", "-package", "nosuch", dir},
		{"status", dir, "-package", "nosuch", "--db", "x.db"},
		{"baseline", "--db", "x.db", "-package", "nosuch", dir},
		{"baseline", dir, "--db", "x.db", "--through", "app/0001_init",
			"-package", "nosuch"},
	} {
		err := runMigration(args)
		if err == nil || !strings.Contains(err.Error(), "nosuch") {
			t.Errorf("%v: err = %v, want a refusal naming nosuch", args, err)
		}
	}
}

func TestParseMigrationArgsTakesFlagsAnywhere(t *testing.T) {
	for _, test := range []struct {
		args, positional []string
		override         string
		destructive      bool
	}{
		{[]string{".", "--allow-destructive"}, []string{"."}, "", true},
		{[]string{"rename_x", ".", "-package", "blog"},
			[]string{"rename_x", "."}, "blog", false},
		{[]string{"--", "-package"}, []string{"-package"}, "", false},
		{[]string{"-package", "blog", "--", ".", "--allow-destructive"},
			[]string{".", "--allow-destructive"}, "blog", false},
	} {
		flags := flag.NewFlagSet("test", flag.ContinueOnError)
		destructive := flags.Bool("allow-destructive", false, "")
		positional, override, err := parseMigrationArgs(flags, test.args)
		if err != nil || strings.Join(positional, " ") !=
			strings.Join(test.positional, " ") || override != test.override ||
			*destructive != test.destructive {
			t.Errorf("%q: got %q, %q, %v, %v; want %q, %q, %v", test.args,
				positional, override, *destructive, err, test.positional,
				test.override, test.destructive)
		}
	}
}
func TestPackageFlagRefusesDotAndDotDot(t *testing.T) {
	for _, name := range []string{".", ".."} {
		dir := twoApps(t)
		err := runMigration([]string{"new", "rename_x", "-package", name,
			dir})
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%q",
			name)) {
			t.Errorf("-package %s: err = %v, want a refusal naming it",
				name, err)
		}
		filepath.WalkDir(dir, func(path string, entry fs.DirEntry,
			err error) error {
			if entry != nil && entry.Name() == "migrations" {
				t.Errorf("-package %s wrote %s", name, path)
			}
			return err
		})
	}
}

// The real loader, and dev's drift check on top of it, find the app
// among helper packages, including a test-only one declaring all three.
func TestLoaderFindsTheAppAmongHelperPackages(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a fixture app")
	}
	appDir := newFixtureApp(t, fixtureModelsGoWithArchived,
		map[string]string{"0001_init.sql": genesisSQL(t, &Note{})},
		map[string]string{
			"internal/harnesstest/harness.go": "package harnesstest\n\n" +
				"var Schema = 1\n",
			"internal/e2etest/flow_test.go": "package e2etest\n\n" +
				"var Schema, BootSchema, Models = 1, 2, 3\n",
		})
	payload, err := loadPayload(appDir, "")
	if err != nil || len(payload.Changes) == 0 {
		t.Fatalf("loadPayload: %d changes, %v; want the Archived column",
			len(payload.Changes), err)
	}
	if message := computeDrift(appDir); !strings.Contains(message,
		"disagree") {
		t.Errorf("computeDrift = %q, want the drift warning", message)
	}
}
