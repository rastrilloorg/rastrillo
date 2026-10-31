package corpus

import (
	"bytes"
	"html/template"
	"testing"

	"amadan.net/rastrillo/rastrillo/tmpl"
)

// BenchmarkTodoMVC renders the bake-off page with 50 items through
// html/template and through the compiled code, into a reused buffer as a
// handler that pools its buffers would. The project's bar is the
// compiled page at a third of html/template's CPU or less at -cpu 1.
//
// The numbers only mean something if the two sides really differ, so
// the benchmark refuses to run unless the compiled side runs compiled
// code and both sides write the same bytes. Without those, a stale
// generated file would have it compare html/template with itself and
// report a ratio of one without complaint. Shadow mode, which would run
// html/template inside the compiled side, is off because TestMain
// clears it.
func BenchmarkTodoMVC(b *testing.B) {
	data := newView(todos(50), filterAll, 0)
	// Executed once before the timer so html/template's lazy escaping,
	// which it does on first use of each template, is not counted.
	std := template.Must(
		template.New("").
			Funcs(todoFuncs).
			ParseFS(todoFS, "testdata/todomvc/*.html"),
	)
	compiled := tmpl.Must(
		tmpl.New("").
			Funcs(todoFuncs).
			ParseFS(todoFS, "testdata/todomvc/*.html"),
	)
	if !compiled.IsCompiled("index") {
		b.Fatal(
			"index has no compiled code; " + regenerate,
		)
	}
	cases := []struct {
		name string
		exec func(*bytes.Buffer) error
	}{
		{
			"html-template",
			func(buffer *bytes.Buffer) error {
				return std.ExecuteTemplate(buffer, "index", data)
			},
		},
		{
			"compiled",
			func(buffer *bytes.Buffer) error {
				return compiled.ExecuteTemplate(buffer, "index", data)
			},
		},
		// A pointer runs the *view variant, whose range rereads the
		// slice through its field on every iteration.
		{
			"compiled-pointer",
			func(buffer *bytes.Buffer) error {
				return compiled.ExecuteTemplate(buffer, "index", &data)
			},
		},
	}
	var want bytes.Buffer
	if err := cases[0].exec(&want); err != nil {
		b.Fatal(err)
	}
	for _, testCase := range cases[1:] {
		var got bytes.Buffer
		if err := testCase.exec(&got); err != nil {
			b.Fatalf("%s: %v", testCase.name, err)
		}
		if !bytes.Equal(got.Bytes(), want.Bytes()) {
			b.Fatalf(
				"%s writes different bytes from html-template",
				testCase.name,
			)
		}
	}
	for _, testCase := range cases {
		b.Run(testCase.name, func(b *testing.B) {
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				var buffer bytes.Buffer
				for pb.Next() {
					buffer.Reset()
					if err := testCase.exec(&buffer); err != nil {
						b.Error(err)
						return
					}
				}
			})
		})
	}
}
