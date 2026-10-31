// Package appname refuses the app names rastrillo new would turn into
// an app that does not build, or fails its own make code, while the
// working directory is still clean.
package appname

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"amadan.net/rastrillo/rastrillo/internal/codelimits"
)

// clashMessage and tooWideMessage are user-facing: a change to either
// goes through the copy review.
const (
	clashMessage = "rastrillo new %[1]q: choose another name. The new " +
		"app's code already uses %[2]s as a name, so a package called " +
		"%[2]s would not build."
	tooWideMessage = "rastrillo new %q: choose a shorter name. It makes " +
		"line %d of %s %d columns wide, and the app's make code allows %d."
)

// taken is every name the scaffold's Go uses in cmd/<name> and the
// <pkg>test package, the two that import the app's package. There a
// variable of the same name shadows the package (main.go's database
// made database.New a method call on *db.DB), and an import or a
// declaration of it clashes with the package's own import.
// TestNewRefusesEveryNameTheScaffoldUses in internal/scaffoldcheck
// parses every scaffold and fails on a name missing here.
var taken = strings.Fields(`
	action address answer append askForLink assets background base
	basepath body bool boot box budget bytes catalog chi chromedp client
	closeAll code confirm confirmPattern context cookie csrf ctx database
	db dbtest err error exitOnError filepath form formPattern genlocales
	get getAs got group handler harness hashedStylesheet href html http
	httptest index io json len library link linkPattern logger mail
	mailer main method migrate mux name newApp newAppWithMail nil ok
	options origin originFrom os outbox page path perftest post postOver
	powtest rastrillo recorder regexp request response rig router routes
	schema send servingOptions sessions signedInTokenPage signIn
	signInOver signins signinStore skip slog started staticDir step
	string strings suite sync t target template TestAPIAnswersInJSON
	TestBareAssetNameStaysFresh TestBrowserWalk
	TestErrorPageRendersFrameworkPartial
	TestIndexLinksFingerprintedStylesheet TestIndexRenders testing
	TestMain testOrigin TestOverviewRenders TestPerfBoot TestPerfScreens
	TestSignInByEmailedLink TestTokenPageShowsANewTokenOnce
	TestVendoredAssetsMatchTheLibrary time title token tokenPattern ui
	url variable vendored vendoredIsMine vendoredTheme year
`)

// CheckPackage refuses the app called name if its package name, pkg,
// is one the scaffold already uses.
func CheckPackage(name, pkg string) error {
	if slices.Contains(taken, pkg) {
		//lint:ignore ST1005 clashMessage is approved copy, verbatim
		return fmt.Errorf(clashMessage, name, pkg)
	}
	return nil
}

// CheckColumns refuses the app called name if a line of its assembled
// files, keyed by path under name, is wider than make code allows. It
// measures the files themselves rather than adding up the name's
// length: a name's punctuation is kept in some lines and dropped from
// its package name in others, and the sum missed lines it overflowed.
func CheckColumns(name string, files map[string]string) error {
	paths := make([]string, 0, len(files))
	for path := range files {
		if strings.HasSuffix(path, ".go") {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	for _, path := range paths {
		relative, err := filepath.Rel(name, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		offences, err := codelimits.ColumnOffences(relative,
			[]byte(files[path]))
		if err != nil {
			return err
		}
		if len(offences) > 0 {
			//lint:ignore ST1005 tooWideMessage is approved copy, verbatim
			return fmt.Errorf(tooWideMessage, name, offences[0].Line,
				relative, offences[0].Size, codelimits.MaxColumns)
		}
	}
	return nil
}
