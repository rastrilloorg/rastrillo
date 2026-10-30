package codelimits

import (
	"bytes"
	"cmp"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// FileName is where a module records the offences it had before the
// gate, so that the gate can hold the line from there on.
const FileName = ".rastrillo/code.txt"

// Record is one line of the file: a known offence and how bad it may be.
// Count is the ceiling for a func or file (the body length, the file's
// lines) and how many identical offences there are for columns and
// names. Line is where the record sits in the file, for messages.
type Record struct {
	Kind      Kind
	Path, Key string
	Count     int
	Reason    string
	Line      int
}

// File is every record of a module.
type File struct {
	Records []Record

	// fromTip is set by LoadBase on a file read from origin/main's tip
	// instead of the merge base, so that CompareWithBase can say a raise
	// may be main having moved on.
	fromTip bool
}

// identity is what a record and an offence share: what they are about,
// whatever their size.
type identity struct {
	kind      Kind
	path, key string
}

func (record Record) identity() identity {
	return identity{record.Kind, record.Path, record.Key}
}

// Load reads root's file. A module with no file has no records, which is
// not an error: a clean module should not need an empty file to say so.
func Load(root string) (*File, error) {
	data, err := os.ReadFile(filepath.Join(root, FileName))
	if errors.Is(err, fs.ErrNotExist) {
		return &File{}, nil
	}
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse refuses rather than guesses, and reports every bad line at once.
func Parse(data []byte) (*File, error) {
	file := &File{}
	seen := map[identity]int{}
	var problems []error
	for index, text := range strings.Split(string(data), "\n") {
		number := index + 1
		record, ok, err := parseLine(number, text)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		if !ok {
			continue
		}
		if first, again := seen[record.identity()]; again {
			problems = append(problems, duplicateRecord(number, first))
			continue
		}
		seen[record.identity()] = number
		file.Records = append(file.Records, record)
	}
	if len(problems) > 0 {
		return nil, joined(problems)
	}
	return file, nil
}

// parseLine reads one line; ok is false for a blank line or a comment.
func parseLine(number int, text string) (record Record, ok bool, err error) {
	text = strings.TrimSpace(text)
	if text == "" || strings.HasPrefix(text, "#") {
		return Record{}, false, nil
	}
	fields := strings.Fields(text)
	if len(fields) < 4 {
		return Record{}, false, tooFewFields(number)
	}
	kind := Kind(fields[0])
	if !slices.Contains(kinds, kind) {
		return Record{}, false, unknownKind(number, fields[0])
	}
	count, err := strconv.Atoi(fields[3])
	if err != nil || count < 1 {
		return Record{}, false, badCount(number, fields[3])
	}
	key, err := parseKey(number, kind, fields[2])
	if err != nil {
		return Record{}, false, err
	}
	return Record{
		Kind: kind, Path: fields[1], Key: key, Count: count,
		Reason: strings.Join(fields[4:], " "), Line: number,
	}, true, nil
}

var kinds = []Kind{Columns, FileLines, FuncLines, Names}

// parseKey maps the dash a file record writes to the empty key it has
// in an offence, and refuses a dash anywhere else, where a key is
// needed to tell records apart.
func parseKey(number int, kind Kind, word string) (string, error) {
	switch {
	case kind == FileLines && word != "-":
		return "", keyNotWanted(number)
	case kind != FileLines && word == "-":
		return "", keyWanted(number, kind)
	case word == "-":
		return "", nil
	}
	return word, nil
}

func displayKey(key string) string {
	if key == "" {
		return "-"
	}
	return key
}

// Format writes the file as Parse reads it, sorted so that two runs on
// the same records give the same bytes and a diff shows only a change.
func Format(file *File) []byte {
	records := slices.Clone(file.Records)
	slices.SortFunc(records, func(left, right Record) int {
		return cmp.Or(
			cmp.Compare(left.Path, right.Path),
			cmp.Compare(left.Kind, right.Kind),
			cmp.Compare(left.Key, right.Key),
		)
	})
	var out bytes.Buffer
	out.WriteString(header)
	for _, record := range records {
		out.WriteString(strings.Join([]string{
			string(record.Kind), record.Path, displayKey(record.Key),
			strconv.Itoa(record.Count),
		}, " "))
		if record.Reason != "" {
			out.WriteString(" " + record.Reason)
		}
		out.WriteString("\n")
	}
	return out.Bytes()
}
