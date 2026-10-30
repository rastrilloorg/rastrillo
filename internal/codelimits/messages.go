package codelimits

import (
	"errors"
	"fmt"
	"strings"
)

// Every sentence the gate says to a person is here, so the wording can
// be reviewed in one place. Each says what is wrong, where, and what to
// do about it.

// NoOrigin and NoRecordsAtBase are said once when the comparison with
// the base is skipped, one for each reason it can be.
const (
	NoOrigin = "No origin/main to compare with: skipping the check that " +
		"records only go down."
	NoRecordsAtBase = "The base has no " + FileName + ": skipping the " +
		"check that records only go down."
	// ShallowTip is said when the comparison is made, but against the
	// tip of origin/main.
	ShallowTip = "This checkout is shallow, so records are compared with " +
		"the tip of origin/main. A raise reported against a main that has " +
		"moved on may need origin/main merged in first."
)

const header = `# Offences against the code limits that already existed when the
# gate began. Records only go down: lower or delete a record when the
# code improves, and give a reason after the count on any record you add
# or raise.
#
# <kind> <path> <key, or -> <count> [reason]
`

func unrecordedMessage(entry *tally) string {
	offence := entry.first
	where := fmt.Sprintf("%s line %d", offence.Path, offence.Line)
	switch offence.Kind {
	case Columns:
		return fmt.Sprintf("%s: %d columns wide, over the %d allowed. "+
			"Wrap the line, or add a record with a reason to %s.",
			where, offence.Size, offence.Limit, FileName)
	case FileLines:
		return fmt.Sprintf("%s: %d lines, over the %d allowed. Split it "+
			"by feature, or add a record with a reason to %s.",
			offence.Path, offence.Size, offence.Limit, FileName)
	case FuncLines:
		return fmt.Sprintf("%s: %s is %d lines, over the %d allowed. "+
			"Split it into smaller functions, or add a record with a "+
			"reason to %s.",
			where, offence.Key, offence.Size, offence.Limit, FileName)
	}
	return fmt.Sprintf("%s: %s is a single-letter name%s. Name it for "+
		"what it holds, or add a record with a reason to %s.",
		where, nameIn(offence.Key), times(entry.count), FileName)
}

// nameIn turns a names key, "function/name", into words.
func nameIn(key string) string {
	cut := strings.LastIndex(key, "/")
	function, name := key[:cut], key[cut+1:]
	if function == "-" {
		return fmt.Sprintf("%q at package level", name)
	}
	return fmt.Sprintf("%q in %s", name, function)
}

func times(count int) string {
	if count < 2 {
		return ""
	}
	return fmt.Sprintf(" (%d times)", count)
}

func overRecordMessage(entry *tally, record *Record) string {
	offence := entry.first
	where := fmt.Sprintf("%s line %d", offence.Path, offence.Line)
	switch offence.Kind {
	case Columns:
		return fmt.Sprintf("%s: %d lines this wide, but the record on "+
			"line %d allows %d. Wrap the new one.",
			where, entry.amount, record.Line, record.Count)
	case Names:
		return fmt.Sprintf("%s: %s%s, but the record on line %d allows "+
			"%d. Name the new one for what it holds.",
			where, nameIn(offence.Key), times(entry.amount),
			record.Line, record.Count)
	}
	return fmt.Sprintf("%s: %s is now %d lines, over the %d its record "+
		"on line %d allows. Make it shorter. Raising a record needs a "+
		"reason.",
		where, describe(offence), entry.amount, record.Count, record.Line)
}

// describe names a func or file offence for a person: a file offence
// has no key, so it is the path.
func describe(offence Offence) string {
	if offence.Kind == FileLines {
		return offence.Path
	}
	return offence.Key
}

func lowerMessage(entry *tally, record *Record) string {
	offence := entry.first
	where := fmt.Sprintf("%s line %d", offence.Path, offence.Line)
	var now string
	switch offence.Kind {
	case Columns:
		now = fmt.Sprintf("%s: %s this wide",
			where, plural(entry.amount, "line"))
	case FileLines:
		now = fmt.Sprintf("%s is now %d lines", offence.Path, entry.amount)
	case FuncLines:
		now = fmt.Sprintf("%s: %s is now %d lines",
			where, offence.Key, entry.amount)
	default:
		now = fmt.Sprintf("%s: %s appears %s", where,
			nameIn(offence.Key), plural(entry.amount, "time"))
	}
	return fmt.Sprintf("%s, but the record on line %d allows %d. Lower "+
		"the record to %d, or run budget code --lower.",
		now, record.Line, record.Count, entry.amount)
}

func plural(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

func staleMessage(record Record) string {
	return fmt.Sprintf("%s line %d: the record for %s in %s has nothing "+
		"left to record. Delete it, or run budget code --lower.",
		FileName, record.Line, recordName(record), record.Path)
}

func recordName(record Record) string {
	if record.Kind == FileLines {
		return "the file's length"
	}
	return fmt.Sprintf("%s %s", record.Kind, displayKey(record.Key))
}

func addedMessage(record Record) string {
	return fmt.Sprintf("%s line %d: the record for %s in %s is new since "+
		"the base and has no reason. Say why after the count, so a "+
		"reviewer can judge it.",
		FileName, record.Line, recordName(record), record.Path)
}

func raisedMessage(record, base Record, fromTip bool) string {
	text := fmt.Sprintf("%s line %d: the record for %s in %s is raised "+
		"from %d to %d and has no reason. Say why after the count, or "+
		"lower it again.",
		FileName, record.Line, recordName(record), record.Path,
		base.Count, record.Count)
	if fromTip {
		text += " If main has changed this record since your branch " +
			"began, merge origin/main into your branch."
	}
	return text
}

// malformed explains one bad line. The line number leads, so that a
// person can go to it.
func malformed(number int, problem string) error {
	return fmt.Errorf("%s line %d: %s", FileName, number, problem)
}

func tooFewFields(number int) error {
	return malformed(number, "want <kind> <path> <key or -> <count> "+
		"[reason], for example: func app/web.go Serve 45")
}

func unknownKind(number int, word string) error {
	return malformed(number, fmt.Sprintf("%q is not a kind: write "+
		"columns, file, func or names", word))
}

func badCount(number int, word string) error {
	return malformed(number, fmt.Sprintf("%q is not a count: write a "+
		"whole number above zero", word))
}

func keyNotWanted(number int) error {
	return malformed(number, "a file record has no key: write - in its place")
}

func keyWanted(number int, kind Kind) error {
	return malformed(number, fmt.Sprintf("a %s record needs a key, not -",
		kind))
}

func duplicateRecord(number, first int) error {
	return malformed(number, fmt.Sprintf("a second record for the same "+
		"thing as line %d: keep one", first))
}

// spacedPath refuses to write a record whose path the file could not
// hold: its fields are separated by spaces.
func spacedPath(path string) error {
	return fmt.Errorf("%q: the gate cannot record a path with spaces, so "+
		"%s was not written. Rename the file without spaces and run it "+
		"again", path, FileName)
}

// joined reports every bad line at once, so that one pass fixes the file.
func joined(problems []error) error {
	return errors.Join(problems...)
}

// gitFailed carries git's own words, because "exit status 128" alone
// does not say what to fix.
func gitFailed(arguments []string, err error, stderr string) error {
	detail := ""
	if stderr != "" {
		detail = ": " + stderr
	}
	return fmt.Errorf("cannot find the base to compare records with: "+
		"git %s: %w%s. Fix git, or run the gate in a checkout with full "+
		"history", strings.Join(arguments, " "), err, detail)
}

// The lines budget code ends a run with. A failing run's summary goes
// to stderr after its messages; a passing run says how many records the
// code is within, so a person sees the count go down.
const (
	MalformedSummary = "budget code: " + FileName + " has lines it cannot " +
		"read. Fix them and run it again."
	ExistsSummary = "budget code: " + FileName + " already exists, so " +
		"--init left it alone. Run budget code --lower to bring its " +
		"records down to the code."
	UsageCode = "usage: rastrillo budget code [--init | --lower] [dir]"
)

// ProblemsSummary ends a run that found problems.
func ProblemsSummary(count int) string {
	return "budget code: " + plural(count, "problem")
}

// PassSummary ends a run that passed.
func PassSummary(mode Mode, records int) string {
	counted := plural(records, "record")
	switch {
	case mode == InitMode:
		return fmt.Sprintf("budget code: wrote %s to %s.", counted, FileName)
	case records == 0:
		return "budget code: within the limits."
	case mode == LowerMode:
		return fmt.Sprintf("budget code: lowered %s to %s. The code is "+
			"within them.", FileName, counted)
	}
	return fmt.Sprintf("budget code: within the limits, with %s in %s.",
		counted, FileName)
}
