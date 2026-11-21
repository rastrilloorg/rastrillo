package main

import (
	"flag"

	"amadan.net/rastrillo/rastrillo/internal/apppackage"
)

// loaderNeeds are the package-level names loaderTemplate reads off the
// app package. Finding the one package that declares all three tells
// the app apart from the helper and end-to-end packages an app such as
// Patrix keeps beside it in internal/.
var loaderNeeds = []string{"Schema", "BootSchema", "Models"}

// parseMigrationArgs registers -package, which every migration
// subcommand takes for the app detection cannot settle, and parses
// flags wherever they sit among args. flag alone stops at the first
// positional, which read `migration new rename_x -package blog` as a
// directory named -package and dropped any flag after [dir] unread.
func parseMigrationArgs(flags *flag.FlagSet, args []string) (
	positional []string, override string, err error) {
	flags.StringVar(&override, "package", "", "directory under "+
		"internal/ to use when package detection cannot choose one")
	for {
		if err := flags.Parse(args); err != nil {
			return nil, "", err
		}
		rest := flags.Args()
		consumed := len(args) - len(rest)
		if len(rest) == 0 || consumed > 0 && args[consumed-1] == "--" {
			return append(positional, rest...), override, nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// appPackage locates the app's package under internal/, as
// apppackage.Find describes, and its import path.
func appPackage(dir, override string) (importPath, pkgName string,
	err error) {
	module, err := modulePath(dir)
	if err != nil {
		return "", "", err
	}
	if pkgName, err = apppackage.Find(dir, override, loaderNeeds); err != nil {
		return "", "", err
	}
	return module + "/internal/" + pkgName, pkgName, nil
}
