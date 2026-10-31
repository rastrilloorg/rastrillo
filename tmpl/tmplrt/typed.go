package tmplrt

import (
	"bytes"
	"maps"
	"slices"
	"strconv"
)

// The functions below are for typed compiled code: code that knows each
// value's static Go type, and so can skip the reflection and the boxing
// into an any that the generic escapers and the dynamic helpers need.

// DecimalEscapers names, as html/template does, the escapers that leave
// the decimal form of an integer as it is. Given an integer of a type
// with no String, Error or Format method, which they print as fmt.Sprint
// does, each returns what IntString or UintString returns for it. The
// compiler writes such an integer with WriteInt or WriteUint, and skips
// the escaper. A test holds each name to that for every integer type.
var DecimalEscapers = []string{
	"_html_template_attrescaper",
	"_html_template_htmlescaper",
	"_html_template_jsstrescaper",
	"_html_template_nospaceescaper",
	"_html_template_rcdataescaper",
	"_html_template_urlescaper",
	"_html_template_urlfilter",
	"_html_template_urlnormalizer",
}

// IntString and UintString are number in decimal, as fmt.Sprint writes an
// integer.
func IntString(number int64) string { return strconv.FormatInt(number, 10) }

func UintString(number uint64) string { return strconv.FormatUint(number, 10) }

// WriteInt and WriteUint write number in decimal to buffer, without
// allocating when buffer has room.
func WriteInt(buffer *bytes.Buffer, number int64) {
	buffer.Write(strconv.AppendInt(buffer.AvailableBuffer(), number, 10))
}

func WriteUint(buffer *bytes.Buffer, number uint64) {
	buffer.Write(strconv.AppendUint(buffer.AvailableBuffer(), number, 10))
}

// Entry is a key of a map and its value.
type Entry[K, V any] struct {
	Key   K
	Value V
}

// orderedKey is a key type {{range}} orders by <, as Go does. Floats are
// left out: text/template puts NaN first.
type orderedKey interface {
	~string | ~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

// SortedMap is mapping's entries in the order {{range}} visits a map
// whose keys are strings or integers: by key, smallest first. Values are
// read before the range runs, as text/template reads them.
func SortedMap[M ~map[K]V, K orderedKey, V any](mapping M) []Entry[K, V] {
	keys := slices.Sorted(maps.Keys(mapping))
	entries := make([]Entry[K, V], len(keys))
	for index, key := range keys {
		entries[index] = Entry[K, V]{key, mapping[key]}
	}
	return entries
}
