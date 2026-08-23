// Package vectors emits the golden vectors that pin an app's JS
// derivation engine to its Go one. Any derivation over sealed content
// runs client-side, but the sidecar, operator tools and tests want
// the same derivation in Go — so the engine exists twice, and two
// engines drifting is the most dangerous E2EE bug class: a wrong
// answer with nothing looking broken. Kass solved it by hand
// (cmd/genvectors → vectors.json → a node:test suite that must
// reproduce every vector); this package is that discipline as
// framework machinery.
//
// A Set is built by the app's own cmd/genvectors (scaffolded by
// `rastrillo vectors -init`), written to test/vectors.json by
// `rastrillo vectors`, and consumed by the app's test/parity.test.mjs
// through the vendored helper this package embeds (see JS).
//
// Two treaty rules ride the file format implicitly, said out loud
// here: the KEY NAMES in fields are part of the contract — the JS
// suite consumes them by name, changing one means changing both sides
// in the same commit, and nothing mechanical checks the key sets
// agree — and time.Time values round-trip via RFC 3339 →
// new Date(v.now): put times in as time.Time, never pre-formatted
// strings.
//
// There is deliberately no hash-pin export. Eventlog hash-pins its
// merge vectors because they are a frozen external spec; app vectors
// change by design, and `rastrillo vectors -check`'s byte-compare
// catches regenerate-drift strictly better than a hash an author
// would just update. A vectors file that IS a frozen external spec
// keeps using eventlog's plain-test pattern in the app's own suite.
package vectors

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
)

// Set is an ordered collection of named vectors — ordered so the
// file reads in the order the rules were written, and two runs over
// the same cases write identical bytes.
type Set struct {
	cases []vcase
}

type vcase struct {
	name, why string
	fields    map[string]any
}

// New returns an empty Set.
func New() *Set { return &Set{} }

// Add appends one vector. name identifies it; why names the rule it
// pins — kass proved the worth of that: the JS test titles become
// "name — why". fields is the vector's payload, and its key names
// are the Go↔JS treaty ("name" and "why" are reserved for the
// vector's own envelope; WriteTo refuses them).
func (s *Set) Add(name, why string, fields map[string]any) {
	s.cases = append(s.cases, vcase{name: name, why: why, fields: fields})
}

// WriteTo emits the whole set as a JSON array, 2-space indent,
// trailing newline — test/vectors.json's exact contract. (The design
// doc sketched a bare error return; the canonical io.WriterTo shape
// is used instead so go vet's stdmethods check stays clean.)
//
// Normalisation is scoped honestly: TOP-LEVEL fields values only — a
// nil slice or nil map sitting directly in fields marshals as []/{},
// never null ("no bests has to look the same on both sides"). Inside
// app-typed values this method never walks: json tags, omitempty and
// custom marshalers make a generic deep walk a lie, so inner
// nil-vs-empty stays the app's own discipline, normalised by hand in
// its cmd/genvectors the way kass normalises its two output fields.
func (s *Set) WriteTo(w io.Writer) (int64, error) {
	var compact bytes.Buffer
	compact.WriteByte('[')
	for i, c := range s.cases {
		if i > 0 {
			compact.WriteByte(',')
		}
		if err := c.marshalInto(&compact); err != nil {
			return 0, err
		}
	}
	compact.WriteByte(']')

	var out bytes.Buffer
	if err := json.Indent(&out, compact.Bytes(), "", "  "); err != nil {
		return 0, err
	}
	out.WriteByte('\n')
	n, err := w.Write(out.Bytes())
	return int64(n), err
}

// marshalInto writes one case as a compact JSON object: name and why
// first (the file reads like the test titles it becomes), then the
// fields sorted by key, so regeneration is deterministic.
func (c vcase) marshalInto(buf *bytes.Buffer) error {
	keys := make([]string, 0, len(c.fields))
	for k := range c.fields {
		if k == "name" || k == "why" {
			return fmt.Errorf("vectors: case %q: field key %q is reserved for the vector envelope", c.name, k)
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	buf.WriteByte('{')
	if err := writeMember(buf, "name", c.name); err != nil {
		return err
	}
	buf.WriteByte(',')
	if err := writeMember(buf, "why", c.why); err != nil {
		return err
	}
	for _, k := range keys {
		buf.WriteByte(',')
		if err := writeMember(buf, k, normalizeTopLevel(c.fields[k])); err != nil {
			return fmt.Errorf("vectors: case %q, field %q: %w", c.name, k, err)
		}
	}
	buf.WriteByte('}')
	return nil
}

func writeMember(buf *bytes.Buffer, key string, val any) error {
	kb, err := json.Marshal(key)
	if err != nil {
		return err
	}
	vb, err := json.Marshal(val)
	if err != nil {
		return err
	}
	buf.Write(kb)
	buf.WriteByte(':')
	buf.Write(vb)
	return nil
}

// normalizeTopLevel is the whole of the library's normalisation: a
// nil slice becomes an empty one, a nil map an empty one — and
// nothing else, at no other depth.
func normalizeTopLevel(v any) any {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice:
		if rv.IsNil() {
			return []any{}
		}
	case reflect.Map:
		if rv.IsNil() {
			return map[string]any{}
		}
	}
	return v
}
