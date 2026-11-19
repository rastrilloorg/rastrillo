package locales

import "sync"

// datesMu guards linkedDates: packs register at init, but a test that
// links a pack late, or a tool reading from another goroutine, must
// not race the registration.
var datesMu sync.RWMutex

// linkedDates is each linked locale's date data, as the JSON the dates
// package reads, from the packs' dates_gen.go (hack/cldrdates).
var linkedDates = map[string]string{}

// RegisterDates adds a pack's date data. Packs call it from init,
// beside Register, so linking a language links its dates. A locale
// registered twice is a generator bug.
func RegisterDates(pack string, records map[string]string) {
	datesMu.Lock()
	defer datesMu.Unlock()
	for id, record := range records {
		if _, dup := linkedDates[id]; dup {
			panic("locales: dates for " + id + " registered twice, " +
				"the second time by pack " + pack)
		}
		linkedDates[id] = record
	}
}

// Dates is the date data for a linked locale id, exactly as Resolve
// spells it. ok is false where its pack is not linked, and for the few
// locales whose dates the formatter cannot draw (hack/cldrdates/
// coverage.tsv says which, and why).
func Dates(id string) (record string, ok bool) {
	datesMu.RLock()
	defer datesMu.RUnlock()
	record, ok = linkedDates[id]
	return record, ok
}
