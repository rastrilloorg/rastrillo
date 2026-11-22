package op

import (
	"encoding/base64"
	encodingjson "encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	json "github.com/go-json-experiment/json"
)

type schemaInput struct {
	Title string          `json:"title"`
	Note  Opt[string]     `json:"note"`
	Price Nullable[Money] `json:"price"`
	Count int             `json:"count,omitzero"`
	Tags  []string        `json:"tags"`
	When  time.Time       `json:"when"`
	Blob  []byte          `json:"blob"`
}

type schemaOutput struct {
	Parent *int64   `json:"parent"`
	Tags   []string `json:"tags"`
	Next   string   `json:"next,omitzero"`
}

type schemaBase struct {
	ID int64 `json:"id"`
}

type schemaShared struct {
	Name string `json:"name"`
}

type schemaMerged struct {
	schemaBase
	schemaShared `json:",inline"`
	Title        string `json:"title"`
}

// moneyMembers is Money's schema less its type, which a Nullable widens.
const moneyMembers = `"properties":{"amount":{"type":"integer"},
	"currency":{"type":"string"}},"required":["amount"],
	"additionalProperties":false`

func TestAnInputSchemaNamesEachMemberByItsTag(t *testing.T) {
	sameJSON(t, mustSchema(t, reflect.TypeFor[schemaInput](), input), `{
		"type":"object",
		"properties":{
			"title":{"type":"string"},
			"note":{"type":"string"},
			"price":{"type":["object","null"],`+moneyMembers+`},
			"count":{"type":"integer"},
			"tags":{"type":"array","items":{"type":"string"}},
			"when":{"type":"string","format":"date-time"},
			"blob":{"type":"string","contentEncoding":"base64"}
		},
		"required":["title","tags","when","blob"],
		"additionalProperties":false
	}`)
}

func TestAnOutputSchemaAllowsNullOnlyForPointers(t *testing.T) {
	sameJSON(t, mustSchema(t, reflect.TypeFor[schemaOutput](), output), `{
		"type":"object",
		"properties":{
			"parent":{"type":["integer","null"]},
			"tags":{"type":"array","items":{"type":"string"}},
			"next":{"type":"string"}
		},
		"required":["parent","tags"],
		"additionalProperties":false
	}`)
}

// TestSchemaMergesAnInlineMemberAndAnEmbeddedStruct also decodes and
// encodes the merged members flat, because the schema is only right if
// the codec puts them where it says.
func TestSchemaMergesAnInlineMemberAndAnEmbeddedStruct(t *testing.T) {
	want := `{
		"type":"object",
		"properties":{
			"id":{"type":"integer"},
			"name":{"type":"string"},
			"title":{"type":"string"}
		},
		"required":["id","name","title"],
		"additionalProperties":false
	}`
	for _, way := range []direction{input, output} {
		sameJSON(t, mustSchema(t, reflect.TypeFor[schemaMerged](), way), want)
	}
	const document = `{"id":1,"name":"a","title":"b"}`
	var merged schemaMerged
	if err := DecodeInput([]byte(document), &merged); err != nil {
		t.Fatalf("DecodeInput(%s) = %v", document, err)
	}
	encoded, err := json.Marshal(merged)
	if err != nil {
		t.Fatal(err)
	}
	sameJSON(t, encoded, document)
}

// schemaLabel writes and reads itself as text, which json carries as a
// string whatever the Go kind underneath.
type schemaLabel struct{ text string }

func (label schemaLabel) MarshalText() ([]byte, error) {
	return []byte(label.text), nil
}

func (label *schemaLabel) UnmarshalText(text []byte) error {
	label.text = string(text)
	return nil
}

type schemaByte byte

type schemaShapes struct {
	Digest [4]byte      `json:"digest"`
	Levels []schemaByte `json:"levels"`
	Label  schemaLabel  `json:"label"`
}

// TestSchemaFollowsTheCodecWhereTheGoKindMisleads covers the types json
// writes by other rules than their kind: a byte array is base64, as a
// byte slice is; a slice of a named byte type is an array of numbers;
// and a type with text methods is a string. Each schema is checked
// against what the encoder writes and what DecodeInput takes back.
func TestSchemaFollowsTheCodecWhereTheGoKindMisleads(t *testing.T) {
	want := `{
		"type":"object",
		"properties":{
			"digest":{"type":"string","contentEncoding":"base64"},
			"levels":{"type":"array","items":{"type":"integer"}},
			"label":{"type":"string"}
		},
		"required":["digest","levels","label"],
		"additionalProperties":false
	}`
	filled := schemaShapes{
		[4]byte{1, 2, 3, 4}, []schemaByte{5}, schemaLabel{"a"}}
	encoded, err := json.Marshal(filled)
	if err != nil {
		t.Fatal(err)
	}
	for _, way := range []direction{input, output} {
		described := mustSchema(t, reflect.TypeFor[schemaShapes](), way)
		sameJSON(t, described, want)
		if err := conforms(t, described, encoded); err != nil {
			t.Errorf("%s does not conform: %v", encoded, err)
		}
	}
	var decoded schemaShapes
	err = DecodeInput(encoded, &decoded)
	if err != nil || decoded.Label != filled.Label {
		t.Errorf("DecodeInput(%s) = %+v, %v", encoded, decoded, err)
	}
}

type schemaShadowed struct {
	*schemaBase
	schemaShared
	Name string `json:"name"`
}

// TestAnEmbeddedPointerAndAHiddenMemberAreDescribedAsJSONWritesThem: a
// nil embedded pointer leaves its members out, so they are not
// required, and an outer member hides an embedded one of the same name.
func TestAnEmbeddedPointerAndAHiddenMemberAreDescribedAsJSONWritesThem(
	t *testing.T,
) {
	described := mustSchema(t, reflect.TypeFor[schemaShadowed](), output)
	sameJSON(t, described, `{
		"type":"object",
		"properties":{"id":{"type":"integer"},"name":{"type":"string"}},
		"required":["name"],
		"additionalProperties":false
	}`)
	for _, value := range []schemaShadowed{
		{},
		{schemaBase: &schemaBase{ID: 1}, Name: "outer"},
	} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := conforms(t, described, encoded); err != nil {
			t.Errorf("%s does not conform: %v", encoded, err)
		}
	}
	_, err := schemaFor(reflect.TypeFor[schemaShadowed](), input)
	want := fmt.Sprintf(msgPointerInput, "schemaShadowed", "schemaBase")
	if err == nil || err.Error() != want {
		t.Errorf("input schema error = %v, want %q", err, want)
	}
}

// TestAnOutputNumberIsTheNumberItHolds: api.Encode writes an
// encoding/json Number as a number. What DecodeInput takes for one
// depends on the json v2 underneath, but a string is taken by all.
func TestAnOutputNumberIsTheNumberItHolds(t *testing.T) {
	type amount struct {
		Total encodingjson.Number `json:"total"`
	}
	sameJSON(t, mustSchema(t, reflect.TypeFor[amount](), output),
		`{"type":"object","properties":{"total":{"type":"number"}},`+
			`"required":["total"],"additionalProperties":false}`)
	const sent = `{"total":"12"}`
	described := mustSchema(t, reflect.TypeFor[amount](), input)
	if err := conforms(t, described, []byte(sent)); err != nil {
		t.Errorf("%s does not conform to %s: %v", sent, described, err)
	}
	var decoded amount
	if err := DecodeInput([]byte(sent), &decoded); err != nil {
		t.Errorf("DecodeInput(%s) = %v", sent, err)
	}
}

func TestSchemaOfNoneIsNull(t *testing.T) {
	for _, way := range []direction{input, output} {
		got := mustSchema(t, reflect.TypeFor[None](), way)
		if string(got) != `{"type":"null"}` {
			t.Errorf("schema of None = %s, want {\"type\":\"null\"}", got)
		}
	}
}

func TestSchemaRefusesWhatTheWireWouldContradict(t *testing.T) {
	type untagged struct{ Title string }
	type misnamed struct {
		IPAddress string `json:"ipaddress"`
	}
	type stringOption struct {
		N int `json:"n,string"`
	}
	type pointer struct {
		Parent *int `json:"parent"`
	}
	type optional struct {
		Note Opt[string] `json:"note"`
	}
	type channel struct {
		C chan int `json:"c"`
	}
	type function struct {
		F func() `json:"f"`
	}
	type complexNumber struct {
		Z complex128 `json:"z"`
	}
	type intKeys struct {
		M map[int]string `json:"m"`
	}
	type stringer struct {
		S fmt.Stringer `json:"s"`
	}
	type node struct {
		Children []node `json:"children"`
	}
	// The pinned json reads inline only on an embedded field; Go 1.26's
	// encoding/json/v2 reads it on any field. Meta would be "Meta" on one
	// wire and flat on the other, so it needs a name of its own.
	type namedInline struct {
		Meta schemaBase `json:",inline"`
	}
	// json v2 has no default for a Duration and refuses to encode one.
	type duration struct {
		Wait time.Duration `json:"wait"`
	}
	for _, refusal := range []struct {
		goType reflect.Type
		way    direction
		want   string
	}{
		{reflect.TypeFor[untagged](), input,
			fmt.Sprintf(msgUntagged, "untagged", "Title", "title")},
		{reflect.TypeFor[misnamed](), output, fmt.Sprintf(msgWireName,
			"misnamed", "IPAddress", "ipaddress", "ip_address")},
		{reflect.TypeFor[stringOption](), input,
			fmt.Sprintf(msgTagOption, "stringOption", "N", "string")},
		{reflect.TypeFor[pointer](), input,
			fmt.Sprintf(msgPointerInput, "pointer", "Parent")},
		{reflect.TypeFor[optional](), output,
			fmt.Sprintf(msgOptResult, "optional", "Note")},
		{reflect.TypeFor[channel](), output,
			fmt.Sprintf(msgUnsupported, "channel", "C", "chan int")},
		{reflect.TypeFor[function](), input,
			fmt.Sprintf(msgUnsupported, "function", "F", "func()")},
		{reflect.TypeFor[complexNumber](), output, fmt.Sprintf(
			msgUnsupported, "complexNumber", "Z", "complex128")},
		{reflect.TypeFor[intKeys](), input,
			fmt.Sprintf(msgUnsupported, "intKeys", "M", "map[int]string")},
		{reflect.TypeFor[stringer](), output,
			fmt.Sprintf(msgUnsupported, "stringer", "S", "fmt.Stringer")},
		{reflect.TypeFor[node](), output,
			fmt.Sprintf(msgCycle, reflect.TypeFor[node]())},
		{reflect.TypeFor[namedInline](), input,
			fmt.Sprintf(msgUntagged, "namedInline", "Meta", "meta")},
		{reflect.TypeFor[duration](), output, fmt.Sprintf(
			msgUnsupported, "duration", "Wait", "time.Duration")},
	} {
		t.Run(refusal.goType.Name(), func(t *testing.T) {
			described, err := schemaFor(refusal.goType, refusal.way)
			if err == nil {
				t.Fatalf("schemaFor = %s, want error %q",
					described, refusal.want)
			}
			if err.Error() != refusal.want {
				t.Errorf("error = %q\nwant    %q", err, refusal.want)
			}
		})
	}
}

func TestOutputSchemasDescribeWhatTheEncoderWrites(t *testing.T) {
	described := mustSchema(t, reflect.TypeFor[schemaOutput](), output)
	parent := int64(7)
	for _, value := range []schemaOutput{
		{},
		{Parent: &parent, Tags: []string{"a"}, Next: "n"},
	} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := conforms(t, described, encoded); err != nil {
			t.Errorf("%s does not conform: %v", encoded, err)
		}
	}
	// The encoder writes a nil slice as [], so a schema that let tags be
	// null would describe a document the encoder never writes.
	const neverWritten = `{"parent":null,"tags":null}`
	if conforms(t, described, []byte(neverWritten)) == nil {
		t.Errorf("%s conforms, but the encoder never writes it", neverWritten)
	}
}

func TestInputSchemasDescribeWhatDecodeInputTakes(t *testing.T) {
	described := mustSchema(t, reflect.TypeFor[schemaInput](), input)
	const required = `"tags":["a"],"when":"2026-11-22T10:00:00Z",` +
		`"blob":"aGk="`
	for _, sent := range []struct {
		document string
		accepted bool
	}{
		{`{"title":"a",` + required + `}`, true},
		{`{"title":null,` + required + `}`, false},
		{`{"title":"a","price":null,` + required + `}`, true},
		{`{"title":"a","price":{"currency":"EUR"},` + required + `}`, false},
		{`{"title":"a","note":"n","count":2,` + required + `}`, true},
		{`{"title":"a","count":1.5,` + required + `}`, false},
		{`{"title":"a","colour":1,` + required + `}`, false},
		{`{"title":"a","tags":"a","when":"2026-11-22T10:00:00Z",` +
			`"blob":"aGk="}`, false},
	} {
		schemaErr := conforms(t, described, []byte(sent.document))
		var decoded schemaInput
		decodeErr := DecodeInput([]byte(sent.document), &decoded)
		if (schemaErr == nil) != sent.accepted {
			t.Errorf("conforms(%s) = %v, want accepted %v",
				sent.document, schemaErr, sent.accepted)
		}
		if (decodeErr == nil) != sent.accepted {
			t.Errorf("DecodeInput(%s) = %v, want accepted %v",
				sent.document, decodeErr, sent.accepted)
		}
	}
	var decoded schemaInput
	err := DecodeInput([]byte(`{"title":"a",`+required+`}`), &decoded)
	if err != nil || string(decoded.Blob) != "hi" {
		t.Errorf("blob decoded to %q, %v; want \"hi\"", decoded.Blob, err)
	}
}

func mustSchema(t *testing.T, goType reflect.Type, way direction) []byte {
	t.Helper()
	described, err := schemaFor(goType, way)
	if err != nil {
		t.Fatalf("schemaFor(%s) = %v", goType, err)
	}
	return described
}

// sameJSON compares two documents as values, so the expected one can be
// written with whitespace and in any member order.
func sameJSON(t *testing.T, got []byte, want string) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("got %s: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("want %s: %v", want, err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Errorf("got  %s\nwant %s",
			got, strings.Join(strings.Fields(want), ""))
	}
}

// conforms checks document against the part of JSON Schema schemaFor
// writes: type, properties, required, additionalProperties, items and
// contentEncoding. It is the test's own reading of the schema, kept apart
// from schemaFor so the two can disagree.
func conforms(t *testing.T, schema, document []byte) error {
	t.Helper()
	var described map[string]any
	var value any
	if err := json.Unmarshal(schema, &described); err != nil {
		t.Fatalf("schema %s: %v", schema, err)
	}
	if err := json.Unmarshal(document, &value); err != nil {
		t.Fatalf("document %s: %v", document, err)
	}
	return conformsAt("", described, value)
}

func conformsAt(path string, described map[string]any, value any) error {
	if !typeAllows(described["type"], value) {
		return fmt.Errorf("%s: %v is not %v", path, value, described["type"])
	}
	if text, ok := value.(string); ok && described["contentEncoding"] != nil {
		if _, err := base64.StdEncoding.DecodeString(text); err != nil {
			return fmt.Errorf("%s: %v", path, err)
		}
	}
	switch value := value.(type) {
	case []any:
		items, _ := described["items"].(map[string]any)
		for index, item := range value {
			itemPath := fmt.Sprintf("%s/%d", path, index)
			if err := conformsAt(itemPath, items, item); err != nil {
				return err
			}
		}
	case map[string]any:
		required, _ := described["required"].([]any)
		for _, name := range required {
			if _, ok := value[name.(string)]; !ok {
				return fmt.Errorf("%s: %v is required", path, name)
			}
		}
		properties, _ := described["properties"].(map[string]any)
		for name, member := range value {
			memberSchema, known := properties[name].(map[string]any)
			if !known && described["additionalProperties"] == false {
				return fmt.Errorf("%s: %s is not allowed", path, name)
			}
			if !known {
				memberSchema, _ =
					described["additionalProperties"].(map[string]any)
			}
			err := conformsAt(path+"/"+name, memberSchema, member)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func typeAllows(declared, value any) bool {
	names, _ := declared.([]any)
	if name, ok := declared.(string); ok {
		names = []any{name}
	}
	if declared == nil {
		return true
	}
	kind := "object"
	switch value := value.(type) {
	case nil:
		kind = "null"
	case bool:
		kind = "boolean"
	case string:
		kind = "string"
	case float64:
		kind = "number"
		if value == math.Trunc(value) {
			kind = "integer"
		}
	case []any:
		kind = "array"
	}
	return slices.Contains(names, any(kind)) ||
		kind == "integer" && slices.Contains(names, any("number"))
}
