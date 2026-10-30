package action_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/action"
	"amadan.net/rastrillo/rastrillo/form"
)

// multipartBody encodes fields as a browser's multipart/form-data post
// would, and returns it with the Content-Type that names its boundary.
func multipartBody(t *testing.T, fields map[string]string) (string, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body.String(), writer.FormDataContentType()
}

func TestAMultipartFormsFieldsReachReadForm(t *testing.T) {
	body, contentType := multipartBody(t, map[string]string{"Name": "Ada"})
	fields := []form.Field{{Name: "Name", Kind: form.Text, Required: true}}
	serveAs(t, contentType, body, func(visit *action.Visit) {
		parsed, err := visit.ReadForm(fields...)
		if err != nil || parsed.String("Name") != "Ada" {
			t.Errorf("ReadForm() = %v, %v, want Ada, nil", parsed, err)
		}
	})
}

func TestAMalformedMultipartBodyIsABadRequest(t *testing.T) {
	want := action.BadRequest{Status: 400, Message: "Bad request."}
	cases := map[string]struct{ contentType, body string }{
		"garbage under a boundary": {
			"multipart/form-data; boundary=xyz", "not a multipart body",
		},
		"no boundary": {"multipart/form-data", "--xyz--\r\n"},
		"cut off mid-part": {
			"multipart/form-data; boundary=xyz",
			"--xyz\r\nContent-Disposition: form-data; name=\"a\"\r\n\r\nb",
		},
		"one stray byte": {"multipart/form-data; boundary=xyz", "x"},
	}
	for name, scenario := range cases {
		serveAs(t, scenario.contentType, scenario.body,
			func(visit *action.Visit) {
				if err := visit.ReadBody(); err != want {
					t.Errorf("%s: ReadBody() = %v, want %v",
						name, err, want)
				}
			})
	}
}

func TestAnOversizedMultipartBodyIsABadRequest(t *testing.T) {
	want := action.BadRequest{Status: 400, Message: "Bad request."}
	body, contentType := multipartBody(t, map[string]string{
		"Name": strings.Repeat("a", 1<<20+1),
	})
	serveAs(t, contentType, body, func(visit *action.Visit) {
		parsed, err := visit.ReadForm(form.Field{Name: "Name"})
		if err != want || parsed != nil {
			t.Errorf("ReadForm() = %v, %v, want nil, %v", parsed, err, want)
		}
	})
}

// A body that is not multipart keeps the answer it had before multipart
// was read: no fields, no refusal.
func TestRequestsWithNoMultipartBodyAreNotRefused(t *testing.T) {
	cases := map[string]struct{ method, contentType, body string }{
		"GET with no body": {http.MethodGet, "", ""},
		"GET naming multipart, no body": {
			http.MethodGet, "multipart/form-data; boundary=xyz", "",
		},
		"POST with no fields": {http.MethodPost, "", ""},
		"POST of plain text":  {http.MethodPost, "text/plain", "hello"},
		"POST naming multipart, no body": {
			http.MethodPost, "multipart/form-data; boundary=xyz", "",
		},
	}
	for name, scenario := range cases {
		serveAt(t, scenario.method, "/things/1", scenario.contentType,
			scenario.body, func(visit *action.Visit) {
				if err := visit.ReadBody(); err != nil {
					t.Errorf("%s: ReadBody() = %v, want nil", name, err)
				}
			})
	}
}
