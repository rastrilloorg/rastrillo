package generate

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/generate/opsgen"
	"amadan.net/rastrillo/rastrillo/internal/manifest"
	"amadan.net/rastrillo/rastrillo/op"
)

var (
	operationLine = regexp.MustCompile(
		`(\w+Op)\s+= rastrillo\.Operation\{ID: "([^"]+)".*` +
			`Access: rastrillo\.(Read|Write)\}`)
	flagLine = regexp.MustCompile(`(?s)Operation:\s+(\w+Op),.*?` +
		`Destructive:\s+(true|false),\s+Idempotent:\s+(true|false),`)
)

// generatedDescriptors reads the descriptors out of the types.go that
// opsgen emits for r in the app at root, not out of opsgen's own
// tables, so the flags tested are the ones that reach the app.
func generatedDescriptors(
	t *testing.T, root string, resource rastrillo.Resource,
) []op.Descriptor {
	t.Helper()
	genDir := t.TempDir()
	if _, _, err := opsgen.Emit(root, genDir, resource); err != nil {
		t.Fatal(err)
	}
	typesPath := filepath.Join(genDir, "ops", resource.Name, "types.go")
	src, err := os.ReadFile(typesPath)
	if err != nil {
		t.Fatal(err)
	}
	flags := flagLine.FindAllStringSubmatch(string(src), -1)
	var descriptors []op.Descriptor
	for _, found := range operationLine.FindAllStringSubmatch(string(src), -1) {
		descriptor := op.Descriptor{Operation: rastrillo.Operation{
			ID: found[2], Access: rastrillo.Read,
		}}
		if found[3] == "Write" {
			descriptor.Access = rastrillo.Write
		}
		for _, flag := range flags {
			if flag[1] == found[1] {
				descriptor.Destructive = flag[2] == "true"
				descriptor.Idempotent = flag[3] == "true"
			}
		}
		descriptors = append(descriptors, descriptor)
	}
	return descriptors
}

// TestGeneratedDescriptorsPassFence: every descriptor the generator
// writes for the example apps passes the check mcp.New runs at boot, so
// a generated delete or update cannot reach an agent looking safe. The
// descriptors are read back from the emitted text, five per resource.
func TestGeneratedDescriptorsPassFence(t *testing.T) {
	for _, example := range []string{"blog", "helloworld", "notes", "tickets"} {
		t.Run(example, func(t *testing.T) {
			root := filepath.Join("..", "..", "examples", example)
			manifests := filepath.Join(root, "manifest")
			resources, err := manifest.Load(root, manifests)
			if err != nil {
				t.Fatal(err)
			}
			for _, resource := range resources {
				descriptors := generatedDescriptors(t, root, resource)
				if len(descriptors) != 5 {
					t.Fatalf("%s: %d descriptors read back; want 5",
						resource.Name, len(descriptors))
				}
				for _, descriptor := range descriptors {
					if err := op.CheckFlags(descriptor); err != nil {
						t.Errorf("%s: %v", example, err)
					}
				}
			}
		})
	}
}
