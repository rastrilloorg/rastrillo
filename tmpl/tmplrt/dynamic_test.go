package tmplrt

import (
	"reflect"
	"testing"
)

// TestAnyMapFieldMatchesMapIndex: the map lookup for map[string]any
// gives what reflect's MapIndex gives, for a present, a nil and a missing
// key under each missingkey option: the same value, an interface value,
// and not addressable.
func TestAnyMapFieldMatchesMapIndex(t *testing.T) {
	data := map[string]any{
		"s": "x", "n": nil, "p": &struct{ A int }{1}, "i": 3,
	}
	rv := reflect.ValueOf(data)
	for _, missing := range []MissingKey{
		MissingKeyDefault, MissingKeyZero, MissingKeyError,
	} {
		for _, key := range []string{"s", "n", "p", "i", "absent"} {
			got, gotErr := anyMapField(data, key, missing)
			want, wantErr := mapField(rv, key, missing)
			if (gotErr != nil) != (wantErr != nil) {
				t.Errorf("%d %q: error %v, MapIndex's %v",
					missing, key, gotErr, wantErr)
				continue
			}
			if got.IsValid() != want.IsValid() {
				t.Errorf("%d %q: valid %v, MapIndex's %v",
					missing, key, got.IsValid(), want.IsValid())
				continue
			}
			if !got.IsValid() {
				continue
			}
			if got.Kind() != want.Kind() || got.Type() != want.Type() ||
				got.CanAddr() != want.CanAddr() {
				t.Errorf("%d %q: %v %v addressable %v, MapIndex's %v %v %v",
					missing, key, got.Kind(), got.Type(), got.CanAddr(),
					want.Kind(), want.Type(), want.CanAddr())
			}
			if got.Interface() != want.Interface() {
				t.Errorf("%d %q: %v, MapIndex's %v",
					missing, key, got.Interface(), want.Interface())
			}
		}
	}
	// Field takes the map path, through a pointer too.
	for _, recv := range []any{data, &data} {
		value, err := Field(reflect.ValueOf(recv), "s")
		if err != nil || value.Kind() != reflect.Interface ||
			value.Elem().String() != "x" {
			t.Errorf("Field(%T, s) = %v, %v", recv, value, err)
		}
	}
}
