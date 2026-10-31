package routelist

import "fmt"

// Every sentence a person reads from rastrillo routes is here, so the
// copy review reads one file and a test can compare a whole message.
// Each one says what is wrong and what to do about it.

// signatureToWrite is the shape of the one function rastrillo routes
// calls. App stands for the app's own type.
const signatureToWrite = "func Routes(routes *action.Routes, app *App)"

const noRoutesFunction = "No Routes function was found in the " +
	"packages under %s.\n" +
	"Add one to the package that registers your routes:\n\n" +
	"\t" + signatureToWrite + "\n\n" +
	"App is your app's type. rastrillo routes calls Routes with an " +
	"empty App, so Routes must only register routes."

const wrongShape = "The Routes function in %s cannot be called by " +
	"rastrillo routes.\n\n" +
	"It is:\n\n\t%s\n\n" +
	"Write it as:\n\n\t" + signatureToWrite + "\n\n" +
	"App can be any exported type in that package, or a pointer to " +
	"one. Routes returns nothing."

const noInternalDir = "%s has no internal directory, where " +
	"rastrillo routes looks for Routes. Run it in your app's " +
	"directory, the one with go.mod, or give that directory's path."

const severalRoutesFunctions = "More than one package has a Routes " +
	"function:\n\n%s\n" +
	"Choose one with --package, for example:\n\n" +
	"\trastrillo routes --package %s"

const chosenHasNone = "No Routes function was found in the package " +
	"%s.\n" +
	"Check the import path. rastrillo routes looks only in the " +
	"packages under internal/."

const inWorkspace = "rastrillo routes does not run in a Go workspace, " +
	"because Go may write go.work.sum. Your app is in the workspace " +
	"%s.\nRun rastrillo routes with GOWORK=off, or outside the workspace."

const goEnvFailed = "Go could not report its settings for %s. Fix " +
	"what Go reports, then try again:\n\n%s"

const noCleanupHere = "rastrillo routes cannot promise to stop and " +
	"clean up what it starts on this system, so it does not run here. " +
	"It runs on Linux and macOS."

const nestedInternal = "rastrillo routes cannot call the Routes " +
	"function in %s: the package is inside a second internal " +
	"directory, so the program that lists your routes cannot import " +
	"it. Move Routes to a package under your app's top-level " +
	"internal/ directory."

const goListFailed = "Go could not list the packages in %s. Fix what " +
	"Go reports, then try again:\n\n%s"

const unreadableFile = "%s could not be read as Go. Fix it, then try " +
	"again: %v"

const reservedDirectory = "Your app has %s, a name rastrillo routes " +
	"keeps for the program it builds to list your routes. Rename it, " +
	"then try again."

const buildFailed = "The app did not build, so its routes could not " +
	"be listed. Fix what Go reports, then try again:\n\n%s"

const runFailed = "The app stopped while it registered its routes " +
	"(%v). What it printed is above.\n" +
	"Routes is called with an empty %s, so it must not use anything " +
	"in it while it registers."

const noListingWritten = "The app exited before it wrote its routes. " +
	"Check that nothing in it calls os.Exit while it starts or while " +
	"Routes runs."

const unreadableListing = "The routes the app wrote could not be " +
	"read: %v. Run rastrillo routes again; if it fails the same way, " +
	"report it as a bug in rastrillo."

const stoppedBeforeListing = "Stopped before the routes were listed."

const stoppedBeforeChecking = "Stopped before every action was checked."

// Usage is what rastrillo routes says when its arguments do not parse.
const Usage = "usage: rastrillo routes [--package path] [--check] [dir]"

const noRoutesRegistered = "Routes registered no routes."

// tableNote ends every table, because a guard in the table is easy to
// read as the whole of who may do what.
const tableNote = "Guards are page middleware. An operation's " +
	"Authorize can still refuse a request that passes them."

const unmountedActions = "No route mounts these actions:\n\n%s\n" +
	"Mount each one in Routes, or delete it. An action mounted " +
	"through an interface value counts as not mounted: mount the " +
	"method on its type instead."

// problem is an error whose text is a whole message for a person, in
// sentences. An error meant to be wrapped starts in lower case with no
// full stop, and staticcheck holds errors.New and fmt.Errorf to that;
// these are never wrapped, only shown.
type problem string

func (message problem) Error() string { return string(message) }

func say(format string, values ...any) error {
	return problem(fmt.Sprintf(format, values...))
}
