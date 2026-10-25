package generate

import (
	"os"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/generate/scratch"
)

// The scratch-app helpers live in the scratch package so the emitters
// in their own packages can run the same apps; the names here keep this
// package's tests as they were.

func TestMain(m *testing.M) {
	code := m.Run()
	scratch.Cleanup()
	os.Exit(code)
}

func scratchEnv() []string { return scratch.Env() }

func rastrilloCLI(t *testing.T) string { return scratch.CLI(t) }

func scratchApp(t *testing.T, files map[string]string) string { return scratch.App(t, files) }

func resourceTOML(name, store string) string { return scratch.ResourceTOML(name, store) }

func writeFile(t *testing.T, app, rel, content string) { scratch.Write(t, app, rel, content) }

func writeMain(t *testing.T, app, wiring string) { scratch.WriteMain(t, app, wiring) }

func runIn(app string, name string, args ...string) (string, error) {
	return scratch.Run(app, name, args...)
}

func runInWith(app string, env []string, name string, args ...string) (string, error) {
	return scratch.RunWith(app, env, name, args...)
}

func mustGenerate(t *testing.T, app string) { scratch.Generate(t, app) }

func checkGenerate(t *testing.T, app string) error { return scratch.Check(t, app) }

func goBuild(app string) (string, error) { return scratch.GoBuild(app) }

func goTest(app, pkg string) (string, error) { return scratch.GoTest(app, pkg) }
