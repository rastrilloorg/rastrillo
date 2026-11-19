package tour

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The chat tour, replayed on this checkout as a reader follows it: the
// app builds, vets and passes its tests after every step that changes
// it, and the finished app passes the checks make ci runs that need no
// network (the route table, migrations, generated files, code limits
// and the perf lane).
func TestEveryStepOfTheChatTourBuildsAndPasses(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds and builds an app for every step")
	}
	tour, err := Load("chat")
	if err != nil {
		t.Fatal(err)
	}
	toolchain := thisCheckout(t)
	app := ""
	played := func(step Step, directory string, changes []Change) error {
		app = directory
		if directory == "" || (len(changes) == 0 && !step.Scaffold) {
			return nil
		}
		for _, arguments := range [][]string{
			{"build", "./..."}, {"vet", "./..."}, {"test", "./..."},
		} {
			if err := toolchain.run(directory, "go", arguments...); err != nil {
				return err
			}
		}
		return nil
	}
	if err := tour.Replay(t.TempDir(), toolchain, played); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{
		"rastrillo routes --check", "rastrillo migration check",
		"rastrillo generate --check", "rastrillo budget code",
		"go test -tags perf ./internal/chattest/",
	} {
		if err := toolchain.command(app, command); err != nil {
			t.Errorf("the finished app: %v", err)
		}
	}
}

// thisCheckout runs the rastrillo this checkout builds, and points each
// new app at it: rastrillo new pins a released version, and this tour
// must pass on the code beside it.
func thisCheckout(t *testing.T) Toolchain {
	t.Helper()
	root := repositoryRoot()
	binary := filepath.Join(t.TempDir(), "rastrillo")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", binary,
		"./cmd/rastrillo")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build rastrillo: %v\n%s", err, output)
	}
	toolchain := Toolchain{Rastrillo: []string{binary}, Env: offline(t),
		NewEnv: []string{"GOPROXY=off", "GOMODCACHE=" + t.TempDir(),
			"GOFLAGS=-mod=mod -buildvcs=false -modcacherw"}}
	toolchain.Made = func(directory string) error {
		file, err := os.OpenFile(filepath.Join(directory, "go.mod"),
			os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, err = file.WriteString("\nreplace " +
			"amadan.net/rastrillo/rastrillo => " + root + "\n")
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		if err := toolchain.run(directory, "go", "mod", "tidy"); err != nil {
			return err
		}
		return toolchain.rastrillo(directory, "generate")
	}
	return toolchain
}

// offline resolves modules from this machine's module cache only, as
// internal/scaffoldcheck does, so a replay never waits on the network.
func offline(t *testing.T) []string {
	t.Helper()
	cache, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatal(err)
	}
	proxy := "file://" + strings.TrimSpace(string(cache)) + "/cache/download"
	return []string{"GOFLAGS=-mod=mod -buildvcs=false", "GOSUMDB=off",
		"GOPRIVATE=*", "GOPROXY=" + proxy, "GOWORK=off"}
}

func repositoryRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..")
}
