// Package contract writes and checks api/v1.json, the app's committed
// record of what /api/v1 serves (JSON API spec section 6): every
// exposed operation's method, path, access, argument fields and result
// fields. The API is generated from manifests, so a manifest edit can
// break it without anyone meaning to; generate compares the contract
// it would write with the committed one before it writes anything, and
// a breaking change fails the run with the file untouched, so the
// evidence of the break survives an ordinary regeneration.
//
// The file is derived from the same data the emitters read, never
// described a second time: routes and access from apigen.Doors, fields
// from shape and wirename as opsgen names them. A money field is
// recorded as the kind "money"; its wire shape is one object for every
// money field, so the kind is the whole of what can change.
package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/generate/apigen"
	"amadan.net/rastrillo/rastrillo/internal/generate/shape"
	"amadan.net/rastrillo/rastrillo/internal/wirename"
)

// Path is the contract's place under the app root. It is committed, and
// lives outside gen/ so prune never touches it.
const Path = "api/v1.json"

// version is the one this generator writes. A file with another is an
// error rather than a comparison, because the rules below are v1's.
const version = 1

// Document is the file. Operations are sorted by id, and each map is
// written with its keys sorted, so a regeneration writes the same bytes.
type Document struct {
	Version    int         `json:"version"`
	Operations []Operation `json:"operations"`
}

// Operation is one exposed route. Input is the arguments as the door
// decodes them, under their wire names; Result is the fields the door
// answers. A list's result is the array under the resource's name and
// the cursor; its items are the get operation's result.
type Operation struct {
	ID     string            `json:"id"`
	Method string            `json:"method"`
	Path   string            `json:"path"`
	Access string            `json:"access"`
	Input  map[string]Field  `json:"input"`
	Result map[string]string `json:"result"`
}

// Field is one argument: its kind, and whether a request without it is
// refused.
type Field struct {
	Kind     string `json:"kind"`
	Required bool   `json:"required"`
}

// The kinds beyond the manifest's own (text, textarea, money).
const (
	kindInteger = "integer"
	kindTime    = "time"
	kindCursor  = "cursor"
	kindArray   = "array"
)

// Contract renders the file for rs: one entry per operation of each
// exposed resource, in a fixed order whatever order rs is in.
func Contract(rs []rastrillo.Resource) ([]byte, error) {
	doc := Document{Version: version, Operations: []Operation{}}
	for _, r := range rs {
		if !r.Exposed() {
			continue
		}
		doc.Operations = append(doc.Operations, operations(r)...)
	}
	sort.Slice(doc.Operations, func(i, j int) bool { return doc.Operations[i].ID < doc.Operations[j].ID })
	return encode(doc), nil
}

// operations is r's five routes, each with the arguments its door
// decodes and the fields it answers. The shapes follow opsgen's
// descriptors: the list takes q (when the store binds a search), each
// declared filter, after and limit; get and delete take the id; create
// takes every form field with the manifest's required; update takes the
// id and every field optional, because it changes only what is sent.
func operations(r rastrillo.Resource) []Operation {
	row := map[string]string{"id": kindInteger, "created_at": kindTime, "updated_at": kindTime}
	for _, c := range shape.Columns(r) {
		row[wirename.Name(c.Name)] = string(c.Kind)
	}
	id := map[string]Field{"id": {Kind: kindInteger, Required: true}}

	var out []Operation
	for _, d := range apigen.Doors(r) {
		o := Operation{ID: r.Name + "." + d.Action, Method: d.Method, Path: d.Pattern, Access: d.Access.String()}
		switch d.Action {
		case "list":
			o.Input = map[string]Field{"after": {Kind: kindCursor}, "limit": {Kind: kindInteger}}
			if shape.SearchBind(r) {
				o.Input["q"] = Field{Kind: string(rastrillo.Text)}
			}
			for _, f := range shape.FilterVars(r) {
				o.Input[wirename.Name(f.Field)] = Field{Kind: string(rastrillo.Text)}
			}
			o.Result = map[string]string{r.Name: kindArray, "next_after": kindCursor}
		case "get":
			o.Input, o.Result = id, row
		case "delete":
			o.Input, o.Result = id, map[string]string{}
		case "create", "update":
			o.Input = map[string]Field{}
			if d.Action == "update" {
				o.Input["id"] = id["id"]
			}
			for _, f := range shape.FormFields(r) {
				o.Input[wirename.Name(f.Name)] = Field{Kind: string(f.Kind), Required: d.Action == "create" && f.Required}
			}
			o.Result = row
		}
		out = append(out, o)
	}
	return out
}

// encode writes doc by hand rather than with MarshalIndent, so that
// each field is one line: the file is read in review, where a break
// should be a one-line diff, and a field spread over four lines is
// not.
func encode(doc Document) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "{\n  \"version\": %d,\n  \"operations\": [", doc.Version)
	for i, o := range doc.Operations {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "\n    {\n      \"id\": %s,\n      \"method\": %s,\n      \"path\": %s,\n      \"access\": %s,\n", str(o.ID), str(o.Method), str(o.Path), str(o.Access))
		b.WriteString("      \"input\": {")
		for j, name := range sortedKeys(o.Input) {
			if j > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, "\n        %s: {\"kind\": %s, \"required\": %t}", str(name), str(o.Input[name].Kind), o.Input[name].Required)
		}
		if len(o.Input) > 0 {
			b.WriteString("\n      ")
		}
		b.WriteString("},\n      \"result\": {")
		for j, name := range sortedKeys(o.Result) {
			if j > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, "\n        %s: %s", str(name), str(o.Result[name]))
		}
		if len(o.Result) > 0 {
			b.WriteString("\n      ")
		}
		b.WriteString("}\n    }")
	}
	if len(doc.Operations) > 0 {
		b.WriteString("\n  ")
	}
	b.WriteString("]\n}\n")
	return b.Bytes()
}

// str is s as a JSON string literal.
func str(s string) string {
	out, _ := json.Marshal(s)
	return string(out)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// parse reads a contract. A version this generator does not write is
// refused rather than compared, since the comparison rules are v1's.
func parse(src []byte) (Document, error) {
	var doc Document
	if err := json.Unmarshal(src, &doc); err != nil {
		return doc, err
	}
	if doc.Version != version {
		return doc, fmt.Errorf("version %d is not one this rastrillo writes (it writes %d)", doc.Version, version)
	}
	return doc, nil
}

// BreakingChanges is what a client written against old loses in new,
// one line each, in a fixed order: a route removed or moved, read
// access that became write, an argument or result field removed or of
// another kind, an argument that became or arrived required. Nothing
// is listed for what is only added, or for an access that became read.
func BreakingChanges(old, new []byte) ([]string, error) {
	was, err := parse(old)
	if err != nil {
		return nil, err
	}
	now, err := parse(new)
	if err != nil {
		return nil, err
	}
	byID := map[string]Operation{}
	for _, o := range now.Operations {
		byID[o.ID] = o
	}
	sort.Slice(was.Operations, func(i, j int) bool { return was.Operations[i].ID < was.Operations[j].ID })

	var out []string
	for _, o := range was.Operations {
		n, ok := byID[o.ID]
		if !ok {
			out = append(out, fmt.Sprintf("%s: route removed (%s %s)", o.ID, o.Method, o.Path))
			continue
		}
		if o.Method != n.Method || o.Path != n.Path {
			out = append(out, fmt.Sprintf("%s: route changed from %s %s to %s %s", o.ID, o.Method, o.Path, n.Method, n.Path))
		}
		if o.Access == rastrillo.Read.String() && n.Access == rastrillo.Write.String() {
			out = append(out, fmt.Sprintf("%s: now needs write access, so a read-only token loses it", o.ID))
		}
		for _, name := range sortedKeys(o.Input) {
			f, ok := n.Input[name]
			switch {
			case !ok:
				out = append(out, fmt.Sprintf("%s: argument %s was removed", o.ID, name))
			case f.Kind != o.Input[name].Kind:
				out = append(out, fmt.Sprintf("%s: argument %s changed kind from %s to %s", o.ID, name, o.Input[name].Kind, f.Kind))
			case f.Required && !o.Input[name].Required:
				out = append(out, fmt.Sprintf("%s: argument %s is now required", o.ID, name))
			}
		}
		for _, name := range sortedKeys(n.Input) {
			if _, ok := o.Input[name]; !ok && n.Input[name].Required {
				out = append(out, fmt.Sprintf("%s: new argument %s is required, so a client that does not send it is refused", o.ID, name))
			}
		}
		for _, name := range sortedKeys(o.Result) {
			kind, ok := n.Result[name]
			switch {
			case !ok:
				out = append(out, fmt.Sprintf("%s: result field %s was removed", o.ID, name))
			case kind != o.Result[name]:
				out = append(out, fmt.Sprintf("%s: result field %s changed kind from %s to %s", o.ID, name, o.Result[name], kind))
			}
		}
	}
	return out, nil
}

// BreakingError is generate's refusal: the breaks, and the two ways
// out. The file named is left as it was.
type BreakingError struct {
	Changes []string
}

func (e *BreakingError) Error() string {
	return fmt.Sprintf("%d breaking change(s) to /api/v1:\n%s\n%s was left as it was. Undo the manifest edit, or run rastrillo generate -accept-breaking to rewrite it (JSON API spec section 6)",
		len(e.Changes), indent(e.Changes), Path)
}

func indent(lines []string) string {
	return "  " + strings.Join(lines, "\n  ")
}

// Write is generate's step: the contract for rs is compared with the
// committed file before anything is written. No file, or an additive
// change, writes it; a breaking change is a *BreakingError and the file
// is untouched, unless acceptBreaking, which writes anyway. The notes
// are for the operator, one line each, and say what happened to the
// file; nothing is returned when it is already current.
func Write(appRoot string, rs []rastrillo.Resource, acceptBreaking bool) (notes []string, err error) {
	path := filepath.Join(appRoot, Path)
	now, err := Contract(rs)
	if err != nil {
		return nil, err
	}
	was, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return []string{Path + ": written"}, write(path, now)
	case err != nil:
		return nil, err
	case bytes.Equal(was, now):
		return nil, nil
	}
	breaks, err := BreakingChanges(was, now)
	if err != nil && !acceptBreaking {
		return nil, fmt.Errorf("%s does not parse: %v. Fix it, or run rastrillo generate -accept-breaking to rewrite it", Path, err)
	}
	if len(breaks) > 0 && !acceptBreaking {
		return nil, &BreakingError{Changes: breaks}
	}
	if len(breaks) > 0 {
		notes = append([]string{fmt.Sprintf("%s: rewritten over %d breaking change(s):", Path, len(breaks))}, strings.Split(indent(breaks), "\n")...)
	} else {
		notes = []string{Path + ": updated"}
	}
	return notes, write(path, now)
}

// write creates the directory too: an app's first exposed resource is
// also its first api/ directory.
func write(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

// Check is --check's step, and writes nothing: the committed file must
// exist and be what Write would write. A stale file that is a break
// says so, with the breaks, because the operator will otherwise learn
// of them only from the generate the message sends them to.
func Check(appRoot string, rs []rastrillo.Resource) error {
	now, err := Contract(rs)
	if err != nil {
		return err
	}
	was, err := os.ReadFile(filepath.Join(appRoot, Path))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("%s is missing. Run rastrillo generate to write it", Path)
	case err != nil:
		return err
	case bytes.Equal(was, now):
		return nil
	}
	breaks, err := BreakingChanges(was, now)
	if err != nil {
		return fmt.Errorf("%s does not parse: %v. Delete it and run rastrillo generate", Path, err)
	}
	if len(breaks) > 0 {
		return fmt.Errorf("%s is out of date, and the change breaks /api/v1 clients:\n%s\nUndo the manifest edit, or run rastrillo generate -accept-breaking to rewrite it", Path, indent(breaks))
	}
	return fmt.Errorf("%s is out of date. Run rastrillo generate", Path)
}
