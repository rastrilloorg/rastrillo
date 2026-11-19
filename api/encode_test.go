package api

import (
	encodingjson "encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A hand-written result may carry an encoding/json Number; both doors
// write it as the number it holds, as MCP always has.
func TestWriteNumberUnquoted(t *testing.T) {
	recorder := httptest.NewRecorder()
	Write(recorder, http.StatusOK,
		map[string]any{"id": encodingjson.Number("9007199254740993")})
	if got, want := recorder.Body.String(),
		`{"id":9007199254740993}`; got != want {
		t.Errorf("Write wrote %s, want %s", got, want)
	}
}

// A Number is written as its own text, so one that is not a JSON number
// would put invalid JSON on the wire unless Encode refuses it.
func TestEncodeRefusesAMalformedNumber(t *testing.T) {
	for _, number := range []string{"12abc", "", " 12", `"12"`, "01"} {
		value := map[string]any{"id": encodingjson.Number(number)}
		if encoded, err := Encode(value); err == nil {
			t.Errorf("Encode(json.Number(%q)) = %s, want an error",
				number, encoded)
		}
	}
}
