package migrate

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

func checkSet(t *testing.T, schema *Set) []Problem {
	t.Helper()
	got, err := CheckDeclarations(context.Background(), schema)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func wantProblems(t *testing.T, got []Problem, want ...Problem) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("problems = %+v, want %+v", got, want)
	}
}

const notesSQL = `CREATE TABLE notes (
	id INTEGER PRIMARY KEY, email TEXT, body TEXT, who TEXT);`

func notesSet(decl Data) *Set {
	return set("app", Migration{ID: "0001_init", SQL: notesSQL}).Declare(decl)
}

func notesData(edit func(*Data)) Data {
	decl := Data{
		Name: "notes", Key: []string{"id"}, Kind: Shared,
		NotAudited: "test",
	}
	if edit != nil {
		edit(&decl)
	}
	return decl
}

func TestCleanDeclarationHasNoProblems(t *testing.T) {
	wantProblems(t, checkSet(t, notesSet(notesData(nil))))
}

func TestUndeclaredTableIsReported(t *testing.T) {
	schema := set("app", Migration{ID: "0001_init", SQL: notesSQL})
	wantProblems(t, checkSet(t, schema), Problem{"notes",
		"no declaration; add one with Declare next to the " +
			"migration that creates it"})
}

func TestDeclaredButNoTableIsReported(t *testing.T) {
	schema := notesSet(notesData(nil)).Declare(Data{
		Name: "ghost", Key: []string{"id"}, Kind: Shared,
	})
	wantProblems(t, checkSet(t, schema), Problem{"ghost",
		"declared but no migration creates it"})
}

func TestDeclaredTwiceNamesBothNamespaces(t *testing.T) {
	first := set("alpha", Migration{ID: "0001_init", SQL: notesSQL}).
		Declare(notesData(nil))
	second := set("beta").Declare(notesData(nil))
	merged := Merge(first, second)
	wantProblems(t, checkSet(t, merged), Problem{"notes",
		"declared by alpha and beta"})
}

func TestMissingKeyColumnIsReported(t *testing.T) {
	schema := notesSet(notesData(func(decl *Data) {
		decl.Key = []string{"nope"}
	}))
	wantProblems(t, checkSet(t, schema), Problem{"notes",
		"key column nope does not exist"})
}

func TestMissingOwnerColumnIsReported(t *testing.T) {
	schema := notesSet(notesData(func(decl *Data) {
		decl.Kind, decl.Erase, decl.Owner = Personal, EraseRow, Subject("nope")
	}))
	wantProblems(t, checkSet(t, schema), Problem{"notes",
		"column nope does not exist"})
}

func TestMissingFieldColumnIsReported(t *testing.T) {
	schema := notesSet(notesData(func(decl *Data) {
		decl.Fields = []Field{{Name: "nope", Holds: HoldsFreeText,
			Erase: EraseKeep}}
	}))
	wantProblems(t, checkSet(t, schema), Problem{"notes",
		"column nope does not exist"})
}

func TestMissingRecordedColumnIsReported(t *testing.T) {
	schema := notesSet(notesData(func(decl *Data) {
		decl.Recorded = []string{"nope"}
	}))
	wantProblems(t, checkSet(t, schema), Problem{"notes",
		"column nope does not exist"})
}

func TestViaNeedsDeclaredParent(t *testing.T) {
	schema := notesSet(notesData(func(decl *Data) {
		decl.Kind, decl.Erase = Personal, EraseRow
		decl.Owner = Via("who", "people")
	}))
	wantProblems(t, checkSet(t, schema), Problem{"notes",
		"owner parent people is not declared"})
}

func TestViaForeignKeyColumnMustExist(t *testing.T) {
	schema := notesSet(notesData(func(decl *Data) {
		decl.Kind, decl.Erase = Personal, EraseRow
		decl.Owner = Via("nope", "notes")
	}))
	wantProblems(t, checkSet(t, schema), Problem{"notes",
		"column nope does not exist"})
}

func TestPersonalNeedsErase(t *testing.T) {
	schema := notesSet(notesData(func(decl *Data) {
		decl.Kind, decl.Owner = Personal, Subject("who")
	}))
	wantProblems(t, checkSet(t, schema), Problem{"notes",
		"personal table needs an Erase"})
}

func TestEraseRowNeedsOwner(t *testing.T) {
	schema := notesSet(notesData(func(decl *Data) {
		decl.Kind, decl.Erase = Personal, EraseRow
	}))
	wantProblems(t, checkSet(t, schema), Problem{"notes",
		"erasing rows needs an Owner"})
}

func TestFieldTreatmentNeedsOwner(t *testing.T) {
	schema := notesSet(notesData(func(decl *Data) {
		decl.Kind, decl.Erase = Personal, EraseKeep
		decl.KeepReason = "legal"
		decl.Fields = []Field{{Name: "email", Holds: HoldsAddress,
			Erase: EraseNull}}
	}))
	wantProblems(t, checkSet(t, schema), Problem{"notes",
		"erasing rows needs an Owner"})
}

func TestKeepNeedsReason(t *testing.T) {
	schema := notesSet(notesData(func(decl *Data) {
		decl.Kind, decl.Erase, decl.Owner = Personal, EraseKeep, Subject("who")
	}))
	wantProblems(t, checkSet(t, schema), Problem{"notes",
		"Keep needs a reason"})
}

func TestRecordedColumnMayNotBePersonal(t *testing.T) {
	schema := notesSet(notesData(func(decl *Data) {
		decl.Kind, decl.Erase, decl.Owner = Personal, EraseRow, Subject("who")
		decl.Fields = []Field{{Name: "email", Holds: HoldsAddress,
			Erase: EraseNull}}
		decl.Recorded = []string{"email"}
	}))
	wantProblems(t, checkSet(t, schema), Problem{"notes",
		"recorded column email holds personal data"})
}

func TestGoMigrationTablesAreSeen(t *testing.T) {
	build := func() *Set {
		return set("app",
			Migration{ID: "0001_init", SQL: notesSQL},
			Migration{ID: "0002_go", Fn: func(ctx context.Context,
				tx Tx) error {
				_, err := tx.ExecContext(ctx,
					"CREATE TABLE made_in_go (id INTEGER PRIMARY KEY)")
				return err
			}}).Declare(notesData(nil))
	}
	wantProblems(t, checkSet(t, build()), Problem{"made_in_go",
		"no declaration; add one with Declare next to the " +
			"migration that creates it"})
	wantProblems(t, checkSet(t, build().Declare(Data{
		Name: "made_in_go", Key: []string{"id"}, Kind: Shared,
		NotAudited: "test",
	})))
}

func TestDroppedTableIsNotThere(t *testing.T) {
	schema := set("app",
		Migration{ID: "0001_init", SQL: notesSQL},
		Migration{ID: "0002_drop", SQL: "DROP TABLE notes;"},
	).Declare(notesData(nil))
	wantProblems(t, checkSet(t, schema), Problem{"notes",
		"declared but no migration creates it"})
}

func TestShadowTablesAreCovered(t *testing.T) {
	schema := set("app", Migration{ID: "0001_init",
		SQL: "CREATE VIRTUAL TABLE notes_fts USING fts5(body);"}).
		Declare(Data{Name: "notes_fts", Key: []string{"rowid"},
			Kind: Shared, NotAudited: "index"})
	wantProblems(t, checkSet(t, schema))
}

func TestRowidKeyAndCompositeKey(t *testing.T) {
	const ddl = `CREATE TABLE loose (a TEXT);
		CREATE TABLE log (stream TEXT, writer TEXT, seq INTEGER,
			PRIMARY KEY (stream, writer, seq)) WITHOUT ROWID;`
	build := func(loose, log []string) *Set {
		return set("app", Migration{ID: "0001_init", SQL: ddl}).
			Declare(Data{Name: "loose", Key: loose, Kind: Shared,
				NotAudited: "test"}).
			Declare(Data{Name: "log", Key: log, Kind: Shared,
				NotAudited: "test"})
	}
	composite := []string{"stream", "writer", "seq"}
	wantProblems(t, checkSet(t, build([]string{"rowid"}, composite)))
	wantProblems(t, checkSet(t,
		build([]string{"rowid"}, []string{"stream", "nope"})),
		Problem{"log", "key column nope does not exist"})
	wantProblems(t, checkSet(t,
		build([]string{"rowid"}, []string{"rowid"})),
		Problem{"log", "key column rowid does not exist"})
}

func TestFrameworkTablesIgnored(t *testing.T) {
	wantProblems(t, checkSet(t, set("app")))
}

func TestApplyReportsWithoutFailing(t *testing.T) {
	decl := openDB(t)
	schema := set("app", Migration{ID: "0001_init", SQL: notesSQL})
	res, err := Apply(context.Background(), decl, schema)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Problems) != 1 || res.Problems[0].Table != "notes" {
		t.Fatalf("Problems = %+v, want one for notes", res.Problems)
	}
}

// The owner column identifies a person as surely as a personal field,
// so recording it would keep in the log what erasure removes.
func TestRecordedColumnMayNotBeTheOwner(t *testing.T) {
	schema := notesSet(notesData(func(decl *Data) {
		decl.Kind, decl.Erase, decl.Owner = Personal, EraseRow, Subject("who")
		decl.Recorded = []string{"who"}
	}))
	wantProblems(t, checkSet(t, schema), Problem{"notes",
		"recorded column who holds personal data"})
}

// Capture keys rows by Key and erasure starts from Kind, so a
// declaration with neither describes nothing it can act on.
func TestDeclarationNeedsKeyAndKind(t *testing.T) {
	schema := notesSet(Data{Name: "notes", NotAudited: "test"})
	wantProblems(t, checkSet(t, schema),
		Problem{"notes", "declaration needs a Key"},
		Problem{"notes", "declaration needs a Kind: Personal, Shared " +
			"or System"})
	schema = notesSet(notesData(func(decl *Data) { decl.Kind = 9 }))
	wantProblems(t, checkSet(t, schema), Problem{"notes",
		"declaration needs a Kind: Personal, Shared or System"})
}

// A nonzero Owner that names no column passes the Owner-needed check
// while giving erasure nothing to match on.
func TestOwnerMustNameAColumn(t *testing.T) {
	owned := func(owner Owner) *Set {
		return notesSet(notesData(func(decl *Data) {
			decl.Kind, decl.Erase, decl.Owner = Personal, EraseRow, owner
		}))
	}
	wantProblems(t, checkSet(t, owned(Subject(""))), Problem{"notes",
		"owner has no column"})
	wantProblems(t, checkSet(t, owned(Via("", "notes"))), Problem{"notes",
		"owner parent notes needs a foreign key column"})
	const cannotMatch = "owner column who must hold a subject, an " +
		"address or a user ID"
	for _, holds := range []Holds{0, HoldsFreeText, 99} {
		t.Run(fmt.Sprint("holds ", int(holds)), func(t *testing.T) {
			wantProblems(t, checkSet(t, owned(Owner{Column: "who",
				Holds: holds})), Problem{"notes", cannotMatch})
		})
	}
}
