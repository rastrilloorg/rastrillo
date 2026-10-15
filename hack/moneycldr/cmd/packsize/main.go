// Command packsize measures what each language pack adds to a binary:
// it builds a program that formats one amount with only the core
// linked, then the same program with one pack, the default twelve, and
// every pack, and prints the differences.
//
//	go run ./cmd/packsize
//
// The programs are written to .packsize/ inside this module, which the
// go tool's ./... patterns skip because of the leading dot, and removed
// afterwards.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var defaults = []string{"en", "ga", "zh", "es", "hi", "pt", "bn", "ru", "ja", "yue", "vi", "ar"}

const mod = "amadan.net/rastrillo/rastrillo"

func build(dir, name string, packs []string) int64 {
	var imports strings.Builder
	for _, p := range packs {
		fmt.Fprintf(&imports, "\t_ %q\n", mod+"/locales/"+p)
	}
	src := fmt.Sprintf("package main\n\nimport (\n\t\"fmt\"\n\n%s\n\t%q\n)\n\nfunc main() { fmt.Println(currency.Format(\"en\", 128450, \"USD\")) }\n",
		imports.String(), mod+"/currency")
	d := filepath.Join(dir, name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(d, "main.go"), []byte(src), 0o644); err != nil {
		panic(err)
	}
	out := filepath.Join(d, "bin")
	cmd := exec.Command("go", "build", "-o", out, "./"+filepath.ToSlash(d))
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		panic(err)
	}
	fi, err := os.Stat(out)
	if err != nil {
		panic(err)
	}
	return fi.Size()
}

func main() {
	dir := ".packsize"
	defer os.RemoveAll(dir)
	base := build(dir, "core", nil)
	fmt.Printf("core only: %d bytes\n", base)
	var sum int64
	for _, p := range defaults {
		d := build(dir, p, []string{p}) - base
		sum += d
		fmt.Printf("  +%-4s %7d bytes\n", p, d)
	}
	all := build(dir, "default12", defaults) - base
	modern := build(dir, "modern", []string{"modern"}) - base
	fmt.Printf("default twelve together: +%d bytes (sum of singles %d)\nevery pack (modern): +%d bytes\n", all, sum, modern)
}
