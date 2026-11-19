package api

import (
	encodingjson "encoding/json"
	"fmt"
	"regexp"

	json "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

// jsonNumber is the grammar of a JSON number, which encoding/json's
// Number does not enforce on what it holds.
var jsonNumber = regexp.MustCompile(
	`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

// exactNumbers writes encoding/json's Number as the number it holds.
// json v2 would quote it, as a named string, and a hand-written result
// may carry its numbers that way to keep them past 2^53.
var exactNumbers = json.WithMarshalers(json.MarshalToFunc(
	func(encoder *jsontext.Encoder, number encodingjson.Number) error {
		if !jsonNumber.MatchString(string(number)) {
			return fmt.Errorf("api: json.Number %q is not a JSON number",
				string(number))
		}
		return encoder.WriteValue(jsontext.Value(number))
	}))

// Encode is value as Write sends it: json v2, with map keys sorted, <
// and & left as they are, and an encoding/json Number written as the
// number it holds. MCP encodes a tool result with it, so both doors
// write the same bytes for the same value.
func Encode(value any) ([]byte, error) {
	// Deterministic sorts map keys, so a fields map encodes the same way
	// every time and a client's test can compare bodies as strings.
	return json.Marshal(value, exactNumbers, json.Deterministic(true))
}
