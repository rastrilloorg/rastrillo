package tmpl

import (
	"bytes"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"amadan.net/rastrillo/rastrillo/tmpl/tmplrt"
)

func TestConcurrentFirstExecution(t *testing.T) {
	var prepares atomic.Int32
	testHookPrepare = func() { prepares.Add(1) }
	t.Cleanup(func() { testHookPrepare = nil })

	const source = `{{define "cell"}}<td>{{.}}</td>{{end}}` +
		`<tr>{{range .}}{{template "cell" .}}{{end}}</tr>`
	build := func() *Template { return Must(New("racerow").Parse(source)) }
	fp := build().fingerprints()["racerow"]
	tmplrt.Register(fp, func(map[string]any) (tmplrt.Exec, error) {
		return func(buffer *bytes.Buffer, data any) error {
			buffer.WriteString("<tr>")
			for _, text := range data.([]string) {
				buffer.WriteString("<td>" + text + "</td>")
			}
			buffer.WriteString("</tr>")
			return nil
		}, nil
	})
	prepares.Store(0)

	set := build()
	data := []string{"a", "b"}
	const runs = 64
	outs := make([]string, 2*runs)
	var start, done sync.WaitGroup
	start.Add(1)
	for run := range runs {
		done.Add(2)
		go func() { // compiled, into a writer that is not a Buffer
			defer done.Done()
			start.Wait()
			var builder strings.Builder
			if err := set.Execute(&builder, data); err != nil {
				t.Error(err)
			}
			outs[run] = builder.String()
		}()
		go func() { // html/template
			defer done.Done()
			start.Wait()
			var buffer bytes.Buffer
			if err := set.ExecuteTemplate(&buffer, "cell", "a"); err != nil {
				t.Error(err)
			}
			outs[runs+run] = buffer.String()
		}()
	}
	start.Done()
	done.Wait()

	if got := prepares.Load(); got != 1 {
		t.Errorf("prepare ran %d times, want 1", got)
	}
	for index, out := range outs {
		want := "<tr><td>a</td><td>b</td></tr>"
		if index >= runs {
			want = "<td>a</td>"
		}
		if out != want {
			t.Fatalf("goroutine %d wrote %q, want %q", index, out, want)
		}
	}
}
