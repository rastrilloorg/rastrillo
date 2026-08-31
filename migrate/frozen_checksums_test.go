package migrate_test

import (
	"testing"

	"amadan.net/rastrillo/rastrillo/auth"
	"amadan.net/rastrillo/rastrillo/blobs"
	"amadan.net/rastrillo/rastrillo/eventlog"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/passkey"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// frozenChecksums is the migrate.Checksum of every migration this
// framework ships, recorded the day each one shipped.
//
// ==> THESE CONSTANTS MAY NEVER BE UPDATED. <==
//
// A failure here does not mean the constant is stale. It means an edit
// to a framework migration file changed its checksum, and that edit
// must be reverted. Every deployed app has one of these checksums in
// its ledger; Apply compares the two on every boot and refuses with
// "was applied with different SQL than the file now contains". Change
// one of these files and every app already running on rastrillo stops
// booting — not on the next schema change, immediately, on the next
// wake, with no recovery short of a hand-edited ledger row per
// database.
//
// The immutability rule is documented for app authors, but the
// framework's own migrations are equally frozen and nothing else in
// the tree says so. The failure mode is silent by construction: adding
// an explanatory comment to a .sql file changes the hash (Checksum
// normalises whitespace, not comments), changes no DDL, and passes
// every other test in the repo. This test is the only thing that can
// catch it before release.
//
// The only legitimate way to change a shipped migration's effect is a
// new migration file beside it, with a new ID and a new entry here.
//
// One exception has been taken, once, and it does not generalise. When
// this branch merged origin/main, main had grown two statements the
// branch's .sql files did not have: eventlog_writer, and
// passkey_recovery_codes plus its index. They were folded into the
// existing eventlog/0001_init and passkey/0001_init rather than into a
// 0002_, and the two constants below were regenerated to match.
//
// That was legitimate for one reason only: this ledger has never run
// anywhere. The branch is unreleased, so no deployed database holds a
// row for either ID, and there is no old checksum to contradict. It
// was also required — adoption replays the set and compares it against
// a live v0.16.0 database, which HAS those tables, so a replay missing
// them would send every deployed app down the refuse-to-boot path.
//
// The moment this branch releases, that reason expires. There is no
// second exception: from the first deploy, a checksum failure here
// means revert the edit, and adding a table means adding a 0002_.
var frozenChecksums = map[string]string{
	"sessions/0001_init":          "50bbfc92cfcc708b09b672b71f24da789ae6065f426c9db7af61befa90098bbb",
	"auth/0001_init":              "3f6bf7e80d71e5d008fc56284e1342a28eecff1dc1da67cc5353bee4fd41a76a",
	"auth/0002_sessions_backfill": "90798138a17a2f5d3f89ea4591985fdd53e9144c63040ae59d45ad6ca6a01a98",
	"blobs/0001_init":             "005e7bef1f2007a3ac88c05944ceba3a3db9c39ba824f73af3d3e7d4140c2427",
	"eventlog/0001_init":          "e507976d87082ac5ee20e2d9cabca305b2d685c7ab4db2c23e650781aa6f9595",
	"passkey/0001_init":           "56d5073a880c3b1b8330654873b13cbd42a72596138499380b5a013e4d43196e",
}

func TestFrameworkMigrationsAreFrozen(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range []*migrate.Set{
		sessions.Schema, auth.Schema, blobs.Schema, eventlog.Schema, passkey.Schema,
	} {
		for _, m := range s.All() {
			seen[m.ID] = true
			want, ok := frozenChecksums[m.ID]
			if !ok {
				t.Errorf("%s ships with no frozen checksum. If it is new, add its current "+
					"migrate.Checksum to frozenChecksums; never edit an existing entry.", m.ID)
				continue
			}
			if got := migrate.Checksum(m.SQL); got != want {
				t.Errorf("%s: checksum is now %s, was %s.\n"+
					"A shipped migration file was edited. Revert the edit — do NOT update the "+
					"constant. Every deployed app has the old checksum in its ledger and will "+
					"refuse to boot with \"applied with different SQL\". Comments count: Checksum "+
					"normalises whitespace, not comments. To change what the schema becomes, add "+
					"a new migration file instead.", m.ID, got, want)
			}
		}
	}
	for id := range frozenChecksums {
		if !seen[id] {
			t.Errorf("%s is pinned here but no longer shipped. Deleting a migration a deployed "+
				"database has already applied is not a supported change.", id)
		}
	}
}
