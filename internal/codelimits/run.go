package codelimits

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Mode is what Run does with a module's records. The modes are not
// called Check, Init and Lower because those are the functions they use.
type Mode int

const (
	// CheckMode measures, judges the code against the records and the
	// records against the base's. It writes nothing.
	CheckMode Mode = iota
	// InitMode records every offence there is now. It refuses a module
	// that already has records, which it would replace along with every
	// reason a person wrote in them.
	InitMode
	// LowerMode brings the records down to the code, then checks.
	LowerMode
)

// Outcome is what a run found. Messages are the problems, one each, and
// none means the module passes. Note is printed either way: why the
// comparison with the base was skipped. Records is how many records the
// module has after the run. Malformed means the file could not be
// parsed, and Messages say which lines; Exists means InitMode found a
// file and left it alone.
type Outcome struct {
	Messages  []string
	Note      string
	Records   int
	Malformed bool
	Exists    bool
}

// Run does mode to the module at root. An error is a failure to read,
// measure or ask git, never a verdict on the code: a gate that turned
// those into a pass or a skip would stop holding the line unseen.
func Run(mode Mode, root string) (Outcome, error) {
	path := filepath.Join(root, FileName)
	data, err := os.ReadFile(path)
	present := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Outcome{}, err
	}
	if mode == InitMode && present {
		return Outcome{Exists: true}, nil
	}
	file, err := Parse(data)
	if err != nil {
		return Outcome{Malformed: true, Messages: []string{err.Error()}}, nil
	}
	offences, err := Measure(root)
	if err != nil {
		return Outcome{}, err
	}
	switch {
	case mode == InitMode:
		file = Init(offences)
		return Outcome{Records: len(file.Records)}, write(path, file)
	case mode == LowerMode && present:
		if file, err = lowerFile(path, offences, file); err != nil {
			return Outcome{}, err
		}
	}
	return check(root, offences, file)
}

// lowerFile rewrites the file through Lower and reads it back, so that
// the messages of the check after it point at the lines as they now are.
func lowerFile(path string, offences []Offence, file *File) (*File, error) {
	lowered := Lower(offences, file)
	if err := write(path, lowered); err != nil {
		return nil, err
	}
	return Parse(Format(lowered))
}

// write refuses a path with white space before it touches the disk:
// Parse splits a line on white space, so such a record would be read
// back with its key and count shifted, and the next check would reject
// the file this run had reported as written.
func write(path string, file *File) error {
	for _, record := range file.Records {
		if strings.ContainsFunc(record.Path, unicode.IsSpace) {
			return spacedPath(record.Path)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, Format(file), 0o644)
}

func check(root string, offences []Offence, file *File) (Outcome, error) {
	base, skipped, err := LoadBase(root)
	if err != nil {
		return Outcome{}, err
	}
	messages := Check(offences, file)
	messages = append(messages, CompareWithBase(file, base)...)
	return Outcome{
		Messages: messages, Note: skipped, Records: len(file.Records),
	}, nil
}
