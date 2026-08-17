package screens

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	rastrillo "github.com/carlosframework/rastrillo"
	"github.com/carlosframework/rastrillo/blobs"
)

// maxUploadMemory bounds multipart parsing's in-memory buffer; larger
// files spill to temp files, which is fine — the bytes stream into the
// blob store either way.
const maxUploadMemory = 8 << 20

// fieldError is a per-field validation failure — a 400 with the field
// named, never a 500.
type fieldError struct {
	Field string
	Msg   string
}

func (e fieldError) Error() string { return e.Field + ": " + e.Msg }

// parseForm reads exactly the given fields from the request — the
// scoping mechanism behind the Basics/Advanced invariant. Blob fields
// need a multipart form and a blob store; a missing file on an existing
// row means "keep the current blob" (the field simply isn't in the
// returned map).
func parseForm(r *http.Request, fields []rastrillo.Field, store blobs.Store, existing map[string]any) (map[string]any, error) {
	hasBlob := false
	for _, f := range fields {
		if f.Kind == rastrillo.Blob {
			hasBlob = true
		}
	}
	if hasBlob && strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(maxUploadMemory); err != nil {
			return nil, fieldError{"form", "could not parse upload"}
		}
	}

	vals := map[string]any{}
	for _, f := range storedFields(fields) {
		name := rastrillo.SnakeCase(f.Name)
		switch f.Kind {
		case rastrillo.Blob:
			ref, ok, err := parseBlob(r, name, store)
			if err != nil {
				return nil, err
			}
			if ok {
				vals[f.Name] = ref
			} else if f.Required && existing == nil {
				return nil, fieldError{f.Name, "a file is required"}
			}
		case rastrillo.Bool:
			vals[f.Name] = r.FormValue(name) != ""
		case rastrillo.Money:
			cents, err := ParseMoney(r.FormValue(name))
			if err != nil {
				return nil, fieldError{f.Name, err.Error()}
			}
			vals[f.Name] = cents
		case rastrillo.Time:
			v := strings.TrimSpace(r.FormValue(name))
			if v != "" {
				if _, err := time.Parse(time.RFC3339, v); err != nil {
					return nil, fieldError{f.Name, "must be an RFC3339 timestamp like 2026-08-17T09:00:00Z"}
				}
			} else if f.Required {
				return nil, fieldError{f.Name, "is required"}
			}
			vals[f.Name] = v
		case rastrillo.Select:
			v := r.FormValue(name)
			if v == "" && !f.Required {
				vals[f.Name] = ""
				continue
			}
			ok := false
			for _, o := range f.Options {
				if v == o {
					ok = true
				}
			}
			if !ok {
				return nil, fieldError{f.Name, "must be one of: " + strings.Join(f.Options, ", ")}
			}
			vals[f.Name] = v
		default: // Text, LongText
			v := r.FormValue(name)
			if f.Required && strings.TrimSpace(v) == "" {
				return nil, fieldError{f.Name, "is required"}
			}
			vals[f.Name] = v
		}
	}
	return vals, nil
}

// parseBlob stores an uploaded file and returns its Ref as the row's
// JSON value. ok=false means no file was submitted.
func parseBlob(r *http.Request, name string, store blobs.Store) (string, bool, error) {
	file, header, err := r.FormFile(name)
	if errors.Is(err, http.ErrMissingFile) || errors.Is(err, http.ErrNotMultipart) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fieldError{name, "could not read upload"}
	}
	defer file.Close()
	if store == nil {
		return "", false, fmt.Errorf("screens: a Blob field needs Deps.Blobs — have Ctx.Scope implement screens.DepsProvider with a blobs.Store")
	}
	ct := header.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/octet-stream"
	}
	ref, err := store.Put(r.Context(), file, ct)
	if err != nil {
		return "", false, err
	}
	b, err := json.Marshal(ref)
	return string(b), true, err
}

// ParseMoney parses a decimal money string ("12", "12.3", "12.34",
// "-0.50") into integer cents. Never a float anywhere in the path —
// CARLOS's non-negotiable.
func ParseMoney(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" && frac == "" {
		return 0, errors.New("must be an amount like 12.34")
	}
	if whole == "" {
		whole = "0"
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, errors.New("must be an amount like 12.34")
	}
	var f int64
	switch len(frac) {
	case 0:
	case 1:
		f, err = strconv.ParseInt(frac, 10, 64)
		f *= 10
	case 2:
		f, err = strconv.ParseInt(frac, 10, 64)
	default:
		return 0, errors.New("at most two decimal places")
	}
	if err != nil {
		return 0, errors.New("must be an amount like 12.34")
	}
	cents := w*100 + f
	if neg {
		cents = -cents
	}
	return cents, nil
}

// FormatMoney renders cents as a plain decimal string ("1234" cents →
// "12.34"). Currency symbols are copy, not data — they belong in the
// label or the catalog.
func FormatMoney(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

// display renders a row value for a cell or a read-only field.
func display(f rastrillo.Field, v any) string {
	switch f.Kind {
	case rastrillo.Money:
		if n, ok := v.(int64); ok {
			return FormatMoney(n)
		}
	case rastrillo.Bool:
		if b, ok := v.(bool); ok {
			if b {
				return "yes"
			}
			return "no"
		}
	case rastrillo.Blob:
		if s, ok := v.(string); ok && s != "" {
			var ref blobs.Ref
			if json.Unmarshal([]byte(s), &ref) == nil {
				return fmt.Sprintf("%s (%d bytes)", ref.ContentType, ref.Size)
			}
		}
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case int64:
		return strconv.FormatInt(t, 10)
	case time.Time:
		if t.IsZero() {
			return ""
		}
		return t.UTC().Format("2006-01-02 15:04")
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}

// formValue renders a row value back into a form control's value
// attribute.
func formValue(f rastrillo.Field, v any) string {
	if f.Kind == rastrillo.Money {
		if n, ok := v.(int64); ok {
			return FormatMoney(n)
		}
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return display(f, v)
}

