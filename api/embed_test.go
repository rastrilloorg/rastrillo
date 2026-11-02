package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"amadan.net/rastrillo/rastrillo/op"
)

// UpdateInput and UpdateArgs have the shape opsgen generates for every
// resource: the update's input embedded, with a `json:",inline"` tag.
// go-json-experiment renamed that option to embed, and from Go 1.27 so
// did encoding/json/v2, which it aliases under GOEXPERIMENT=jsonv2; Go
// 1.26's still reads inline. The members stay flat on every one only
// because the field is a Go embedding, and next-go runs this test on
// all three.
type UpdateInput struct {
	Title   op.Opt[string]        `json:"title"`
	Deposit op.Nullable[op.Money] `json:"deposit"`
}

type UpdateArgs struct {
	ID          int64 `json:"id"`
	UpdateInput `json:",inline"`
}

// TestEmbeddedInputStaysFlat decodes flat members into the embedded
// input and writes them back out flat, byte for byte.
func TestEmbeddedInputStaysFlat(t *testing.T) {
	const body = `{"id":3,"title":"x","deposit":null}`
	var args UpdateArgs
	if err := op.DecodeInput([]byte(body), &args); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if args.ID != 3 || args.Title.Value != "x" || !args.Deposit.Null {
		t.Fatalf("decoded %+v; want id 3, title x, deposit cleared", args)
	}
	recorder := httptest.NewRecorder()
	Write(recorder, http.StatusOK, args)
	if written := recorder.Body.String(); written != body {
		t.Errorf("Write wrote %s, want %s", written, body)
	}
}
