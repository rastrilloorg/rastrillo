package blobs

import "amadan.net/rastrillo/rastrillo/migrate"

// declare calls inline blobs personal because Inline keeps the bytes
// here and they may describe someone; with no owner column, erasure
// cannot find them, so the table is kept and says why.
func declare(set *migrate.Set) *migrate.Set {
	return set.Declare(migrate.Data{
		Name:  "blobs",
		Key:   []string{"hash"},
		Kind:  migrate.Personal,
		Erase: migrate.EraseKeep,
		KeepReason: "inline blob bytes have no owner column; an " +
			"ownership-aware eraser comes with erasure (phase 3)",
	})
}
