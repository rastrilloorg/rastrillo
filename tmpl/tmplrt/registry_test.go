package tmplrt_test

import (
	"bytes"
	"testing"

	"amadan.net/rastrillo/rastrillo/tmpl/tmplrt"
)

func TestRegisterFirstWins(t *testing.T) {
	bind := func(text string) tmplrt.Bind {
		return func(map[string]any) (tmplrt.Exec, error) {
			return func(buffer *bytes.Buffer, _ any) error {
				buffer.WriteString(text)
				return nil
			}, nil
		}
	}
	tmplrt.Register("registry-test", bind("first"))
	tmplrt.Register("registry-test", bind("second"))
	got, ok := tmplrt.Lookup("registry-test")
	if !ok {
		t.Fatal("Lookup found nothing")
	}
	exec, _ := got(nil)
	var buffer bytes.Buffer
	_ = exec(&buffer, nil)
	if buffer.String() != "first" {
		t.Errorf("Lookup returned the %s registration, want the first",
			buffer.String())
	}
	if _, ok := tmplrt.Lookup("registry-test-absent"); ok {
		t.Error("Lookup found a fingerprint nobody registered")
	}
}

func TestRegisterOwner(t *testing.T) {
	if _, ok := tmplrt.Owner("example.com/registry-none"); ok {
		t.Error("Owner reports a package that never registered")
	}
	tmplrt.RegisterOwner("example.com/registry", []string{"b", "a"})
	tmplrt.RegisterOwner("example.com/registry-empty", nil)
	got, ok := tmplrt.Owner("example.com/registry")
	if !ok || len(got) != 2 {
		t.Errorf("Owner = %v, %v; want [b a], true", got, ok)
	}
	if _, ok := tmplrt.Owner("example.com/registry-empty"); !ok {
		t.Error("an owner with nothing unsupported is not registered")
	}
}
