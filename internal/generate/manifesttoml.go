package generate

import (
	"fmt"
	"strconv"
	"strings"

	rastrillo "github.com/carlosframework/rastrillo"
)

// This file decodes the manifest TOML subset (design doc §3): top-level
// scalars, [list]/[form]/[delete] tables, arrays of strings, and arrays
// of inline tables. Hand-rolled like internal/catalog's flat decoder —
// the subset a manifest actually uses fits in a page, and the design
// doc's open question about picking a TOML dependency stays open
// rather than being answered by accident. This is a manifest decoder
// only; it is not, and must not grow into, a general TOML library.

// decodeManifestTOML lowers one manifest file into the identical typed
// value a .go manifest declares — one pipeline, two spellings.
func decodeManifestTOML(src, file string) (rastrillo.Resource, error) {
	var res rastrillo.Resource
	section := ""
	fail := func(line int, format string, args ...any) (rastrillo.Resource, error) {
		return rastrillo.Resource{}, fmt.Errorf("%s:%d: %s", file, line, fmt.Sprintf(format, args...))
	}

	for i, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return fail(i+1, "malformed section %q", line)
			}
			section = strings.TrimSpace(line[1 : len(line)-1])
			switch section {
			case "list", "form", "delete":
			default:
				return fail(i+1, "unknown section [%s] (want [list], [form] or [delete])", section)
			}
			continue
		}
		key, rawVal, ok := strings.Cut(line, "=")
		if !ok {
			return fail(i+1, "expected key = value, got %q", line)
		}
		key = strings.TrimSpace(key)
		rawVal = strings.TrimSpace(rawVal)
		val, rest, err := parseTOMLValue(rawVal)
		if err != nil {
			return fail(i+1, "%s: %v", key, err)
		}
		if rest = strings.TrimSpace(rest); rest != "" && !strings.HasPrefix(rest, "#") {
			return fail(i+1, "%s: trailing input %q", key, rest)
		}

		switch section + "." + key {
		case ".name":
			res.Name, err = wantString(val)
		case ".route":
			res.Route, err = wantString(val)
		case ".store":
			var s string
			if s, err = wantString(val); err == nil {
				switch s {
				case "exclusive", "":
					res.Store = rastrillo.Exclusive
				case "mergeable":
					res.Store = rastrillo.Mergeable
				default:
					err = fmt.Errorf("unknown store %q (want exclusive or mergeable)", s)
				}
			}
		case "list.columns":
			res.List.Columns, err = wantColumns(val)
		case "list.search":
			res.List.Search, err = wantBool(val)
		case "list.filter":
			res.List.Filter, err = wantStrings(val)
		case "form.basics":
			res.Form.Basics, err = wantFields(val)
		case "form.advanced":
			res.Form.Advanced, err = wantFields(val)
		case "delete.confirm":
			res.Delete.Confirm, err = wantString(val)
		default:
			err = fmt.Errorf("unknown key")
		}
		if err != nil {
			return fail(i+1, "%s: %v", key, err)
		}
	}
	return res, nil
}

// parseTOMLValue reads one value from the front of s: a quoted string,
// true/false, or an array of values (strings or inline tables).
func parseTOMLValue(s string) (any, string, error) {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasPrefix(s, `"`):
		return parseTOMLString(s)
	case strings.HasPrefix(s, "["):
		return parseTOMLArray(s)
	case strings.HasPrefix(s, "{"):
		return parseTOMLInlineTable(s)
	case s == "true" || strings.HasPrefix(s, "true"):
		return true, s[len("true"):], nil
	case s == "false" || strings.HasPrefix(s, "false"):
		return false, s[len("false"):], nil
	}
	return nil, "", fmt.Errorf("unsupported value %q (the manifest subset takes strings, booleans, arrays and inline tables)", s)
}

func parseTOMLString(s string) (any, string, error) {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			out, err := strconv.Unquote(s[:i+1])
			return out, s[i+1:], err
		}
	}
	return nil, "", fmt.Errorf("unterminated string")
}

func parseTOMLArray(s string) (any, string, error) {
	s = s[1:] // consume [
	var items []any
	for {
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, "", fmt.Errorf("unterminated array")
		}
		if s[0] == ']' {
			return items, s[1:], nil
		}
		v, rest, err := parseTOMLValue(s)
		if err != nil {
			return nil, "", err
		}
		items = append(items, v)
		s = strings.TrimSpace(rest)
		if strings.HasPrefix(s, ",") {
			s = s[1:]
		}
	}
}

func parseTOMLInlineTable(s string) (any, string, error) {
	s = s[1:] // consume {
	out := map[string]any{}
	for {
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, "", fmt.Errorf("unterminated inline table")
		}
		if s[0] == '}' {
			return out, s[1:], nil
		}
		eq := strings.Index(s, "=")
		if eq < 0 {
			return nil, "", fmt.Errorf("inline table wants key = value")
		}
		key := strings.TrimSpace(s[:eq])
		v, rest, err := parseTOMLValue(s[eq+1:])
		if err != nil {
			return nil, "", err
		}
		out[key] = v
		s = strings.TrimSpace(rest)
		if strings.HasPrefix(s, ",") {
			s = s[1:]
		}
	}
}

func wantString(v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("want a string")
	}
	return s, nil
}

func wantBool(v any) (bool, error) {
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("want true or false")
	}
	return b, nil
}

func wantStrings(v any) ([]string, error) {
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("want an array of strings")
	}
	var out []string
	for _, it := range items {
		s, err := wantString(it)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func wantKind(m map[string]any) (rastrillo.Kind, error) {
	raw, ok := m["kind"]
	if !ok {
		return rastrillo.Text, nil // the default, as in the Go form's zero value
	}
	s, err := wantString(raw)
	if err != nil {
		return 0, err
	}
	k, ok := rastrillo.KindByName(s)
	if !ok {
		return 0, fmt.Errorf("unknown kind %q", s)
	}
	return k, nil
}

func wantColumns(v any) ([]rastrillo.Column, error) {
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("want an array of inline tables")
	}
	var out []rastrillo.Column
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("want inline tables like { field = \"Name\", kind = \"text\" }")
		}
		var c rastrillo.Column
		var err error
		if c.Field, err = wantString(m["field"]); err != nil {
			return nil, fmt.Errorf("column needs a field")
		}
		if c.Kind, err = wantKind(m); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func wantFields(v any) ([]rastrillo.Field, error) {
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("want an array of inline tables")
	}
	var out []rastrillo.Field
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("want inline tables like { name = \"Price\", kind = \"money\" }")
		}
		var f rastrillo.Field
		var err error
		if f.Name, err = wantString(m["name"]); err != nil {
			return nil, fmt.Errorf("field needs a name")
		}
		if f.Kind, err = wantKind(m); err != nil {
			return nil, err
		}
		if raw, ok := m["required"]; ok {
			if f.Required, err = wantBool(raw); err != nil {
				return nil, err
			}
		}
		if raw, ok := m["derived"]; ok {
			if f.Derived, err = wantBool(raw); err != nil {
				return nil, err
			}
		}
		if raw, ok := m["options"]; ok {
			if f.Options, err = wantStrings(raw); err != nil {
				return nil, err
			}
		}
		out = append(out, f)
	}
	return out, nil
}
