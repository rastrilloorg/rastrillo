package tmpl

import (
	"bytes"
	"fmt"
	"io"

	"amadan.net/rastrillo/rastrillo/tmpl/tmplrt"
)

// The errors shadow mode returns, kept together so they can be reviewed
// as copy. Each names the set (the template Execute was called on) and
// the entry that ran.
const (
	shadowDiffers = "tmpl: shadow: template %q, entry %q: " +
		"compiled code differs from html/template at byte %d: " +
		"compiled %q, html/template %q"
	shadowCompiledFails = "tmpl: shadow: template %q, entry %q: " +
		"compiled code failed where html/template did not: %w"
	shadowStdFails = "tmpl: shadow: template %q, entry %q: " +
		"html/template failed where compiled code did not: %w"
)

// shadowContext is how many bytes either side of the first difference
// the error quotes.
const shadowContext = 24

// shadow renders entry both ways and writes html/template's output, or
// fails if the two differ.
//
// html/template runs entry on a set of its own, cloned from the set as
// it was before anything executed, on which entry runs first. That is
// what compiled code matches. On a set shared by several entries,
// html/template's output can depend on which ran first, because
// escaping edits the trees in place: a partial executed directly is
// escaped for HTML, and a page that then calls it inside <script>
// escapes the value twice. Compiled code does not repeat that.
func (template *Template) shadow(
	writer io.Writer,
	entry string,
	exec tmplrt.Exec,
	data any,
	sets *shadowSets,
) error {
	std, err := sets.entry(entry)
	if err != nil {
		return err
	}
	var compiled, want bytes.Buffer
	compiledErr := exec(&compiled, data)
	stdErr := std.ExecuteTemplate(&want, entry, data)
	switch {
	case stdErr != nil && compiledErr != nil:
		return stdErr
	case stdErr != nil:
		return fmt.Errorf(shadowStdFails, template.Name(), entry, stdErr)
	case compiledErr != nil:
		return fmt.Errorf(
			shadowCompiledFails,
			template.Name(),
			entry,
			compiledErr,
		)
	}
	if at, differ := firstDifference(compiled.Bytes(), want.Bytes()); differ {
		return fmt.Errorf(shadowDiffers, template.Name(), entry, at,
			around(compiled.Bytes(), at), around(want.Bytes(), at))
	}
	_, err = writer.Write(want.Bytes())
	return err
}

// firstDifference is the offset of the first byte at which left and right
// differ, counting the end of the shorter one as a difference.
func firstDifference(left, right []byte) (int, bool) {
	shortest := min(len(left), len(right))
	for index := range shortest {
		if left[index] != right[index] {
			return index, true
		}
	}
	return shortest, len(left) != len(right)
}

func around(text []byte, at int) string {
	return string(
		text[max(0, at-shadowContext):min(len(text), at+shadowContext)],
	)
}
