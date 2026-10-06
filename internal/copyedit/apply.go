package copyedit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// DefaultProse is the prose file an edit changes when it names none.
const DefaultProse = "internal/designsystem/prose.go"

// output is one file's new contents, held until every file in the edit
// has been computed. note is the line Run prints once it is written;
// prose.go's is "prose.go: +N -M" whatever its path, the form the copy
// steps check for.
type output struct {
	path string
	data []byte
	note string
}

// Run applies one edit file. Every output is computed before anything
// is written, and nothing is written if any part fails: a half-applied
// edit leaves prose.go holding its new keys, so the corrected re-run is
// refused with "already has" and the tree has to be repaired by hand.
func Run(path string, w io.Writer) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var e Edit
	// A misspelt field would otherwise decode to an edit that silently
	// does less than its author meant.
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	approved, err := LoadApproved(e.Approved)
	if err != nil {
		return err
	}
	var outs []output
	if len(e.Remove)+len(e.Add) > 0 {
		if e.Prose == "" {
			e.Prose = DefaultProse
		}
		src, err := os.ReadFile(e.Prose)
		if err != nil {
			return err
		}
		out, err := ApplyProse(string(src), approved, e.Remove, e.Add)
		if err != nil {
			return err
		}
		outs = append(outs, output{e.Prose, []byte(out), fmt.Sprintf("prose.go: +%d -%d", len(e.Add), len(e.Remove))})
	}
	for _, f := range e.Fill {
		src, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		out, n, err := Fill(string(src), approved)
		if err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		if n == 0 {
			return fmt.Errorf("%s: no ⟦id⟧ marker to fill", f)
		}
		outs = append(outs, output{f, []byte(out), fmt.Sprintf("%s: %d filled", f, n)})
	}
	// Two outputs for one file would each be computed from the original,
	// and the second write would silently drop the first's changes.
	seen := map[string]bool{}
	for _, o := range outs {
		abs, err := filepath.Abs(o.path)
		if err != nil {
			return err
		}
		if seen[abs] {
			return fmt.Errorf("%s: named twice in one edit", o.path)
		}
		seen[abs] = true
	}
	for _, o := range outs {
		if err := writeAtomic(o.path, o.data); err != nil {
			return err
		}
		fmt.Fprintf(w, "copyedit: %s\n", o.note)
	}
	return nil
}

// writeAtomic replaces a file by renaming a finished temp file over it
// in the same directory, so an interrupted write leaves the old file
// whole rather than truncated. The file keeps its mode.
func writeAtomic(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".copyedit-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := firstErr(werr, cerr, os.Chmod(tmp.Name(), info.Mode().Perm())); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
