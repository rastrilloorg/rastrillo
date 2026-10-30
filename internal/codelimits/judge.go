package codelimits

import (
	"cmp"
	"slices"
)

func (offence Offence) identity() identity {
	return identity{offence.Kind, offence.Path, offence.Key}
}

// tally is every offence that shares one identity, boiled down to the
// one number a record holds.
type tally struct {
	first  Offence // the one nearest the top of the file
	count  int
	amount int // the number to compare with a record's Count
}

// tallies groups offences by identity, in order of first appearance.
// A func offence's key is already unique in its file: Measure numbers
// namesakes while it still sees the short ones, which here are gone.
func tallies(offences []Offence) (map[identity]*tally, []identity) {
	ordered := slices.Clone(offences)
	slices.SortStableFunc(ordered, func(left, right Offence) int {
		return cmp.Or(
			cmp.Compare(left.Path, right.Path),
			cmp.Compare(left.Kind, right.Kind),
			cmp.Compare(left.Key, right.Key),
			cmp.Compare(left.Line, right.Line),
		)
	})
	byIdentity := map[identity]*tally{}
	var order []identity
	for _, offence := range ordered {
		entry, ok := byIdentity[offence.identity()]
		if !ok {
			entry = &tally{first: offence}
			byIdentity[offence.identity()] = entry
			order = append(order, offence.identity())
		}
		entry.add(offence)
	}
	return byIdentity, order
}

// add counts one more offence in. Columns and names are judged by how
// many there are; a func or file by how big the one is.
func (entry *tally) add(offence Offence) {
	entry.count++
	switch offence.Kind {
	case Columns, Names:
		entry.amount = entry.count
	default:
		entry.amount = max(entry.amount, offence.Size)
	}
}

// Init records every offence, with no reason: these are the ones that
// existed before the gate, and the base comparison asks a reason only
// of records a branch adds or raises later.
func Init(offences []Offence) *File {
	byIdentity, order := tallies(offences)
	file := &File{}
	for _, id := range order {
		file.Records = append(file.Records, Record{
			Kind: id.kind, Path: id.path, Key: id.key,
			Count: byIdentity[id].amount,
		})
	}
	return file
}

// Check judges the offences against the records and says, one message
// each, what to fix. No message means the code is exactly as good as the
// records say: a record that is looser than the code fails too, or a
// fixed offence could creep back unnoticed.
func Check(offences []Offence, file *File) []string {
	byIdentity, order := tallies(offences)
	records := map[identity]*Record{}
	for index := range file.Records {
		records[file.Records[index].identity()] = &file.Records[index]
	}
	var messages []string
	for _, id := range order {
		if message := judge(byIdentity[id], records[id]); message != "" {
			messages = append(messages, message)
		}
	}
	for _, record := range file.Records {
		if _, found := byIdentity[record.identity()]; !found {
			messages = append(messages, staleMessage(record))
		}
	}
	return messages
}

// judge compares one group of offences with its record, which is nil
// when there is none.
func judge(entry *tally, record *Record) string {
	switch {
	case record == nil:
		return unrecordedMessage(entry)
	case entry.amount > record.Count:
		return overRecordMessage(entry, record)
	case entry.amount < record.Count:
		return lowerMessage(entry, record)
	}
	return ""
}

// Lower brings the records down to what the code now is, and deletes
// those with nothing left. It never adds a record or raises one: the
// only way up is a person writing a reason.
func Lower(offences []Offence, file *File) *File {
	byIdentity, _ := tallies(offences)
	lowered := &File{}
	for _, record := range file.Records {
		entry, found := byIdentity[record.identity()]
		if !found {
			continue
		}
		record.Count = min(record.Count, entry.amount)
		lowered.Records = append(lowered.Records, record)
	}
	return lowered
}
