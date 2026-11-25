package migrate

import "slices"

// Kind says whose data a table holds, which decides whether erasure
// ever has to look at it.
type Kind int

const (
	// Personal tables hold data about people and answer to erasure.
	Personal Kind = iota + 1
	// Shared tables hold data that belongs to no one person.
	Shared
	// System tables are the framework's own bookkeeping.
	System
)

// Holds says what sort of personal value a column carries, because an
// address and a free-text note are found and erased differently.
type Holds int

const (
	// HoldsSubject is the identifier of the person the row is about.
	HoldsSubject Holds = iota + 1
	// HoldsAddress is a contact address such as an email.
	HoldsAddress
	// HoldsUserID is the id of a signed-in user.
	HoldsUserID
	// HoldsFreeText is text a person typed, which may name anyone.
	HoldsFreeText
)

// Treatment is how erasure deals with a value, so the choice is
// written down once per table or field instead of decided at erase time.
type Treatment int

const (
	// EraseRow deletes the whole row.
	EraseRow Treatment = iota + 1
	// EraseNull clears the value and keeps the row.
	EraseNull
	// EraseTombstone replaces the value with a marker that it was erased.
	EraseTombstone
	// EraseKeep leaves the value alone and must say why in KeepReason.
	EraseKeep
)

// Owner names the column that ties a row to the person it belongs to,
// so erasing a person can find their rows. Via reaches the person
// through a parent table instead, for rows with no column of their own.
type Owner struct {
	// Column is the column on this table, or the foreign key column
	// for Via.
	Column string
	Holds  Holds
	// Parent is non-empty for Via: the table whose row owns this one.
	Parent string
}

// Subject owns a row by the column holding its subject.
func Subject(column string) Owner {
	return Owner{Column: column, Holds: HoldsSubject}
}

// Address owns a row by the column holding a contact address.
func Address(column string) Owner {
	return Owner{Column: column, Holds: HoldsAddress}
}

// UserID owns a row by the column holding a user's id.
func UserID(column string) Owner {
	return Owner{Column: column, Holds: HoldsUserID}
}

// Via owns a row through the parent table its foreign key points at.
func Via(column, parent string) Owner {
	return Owner{Column: column, Parent: parent}
}

// Field is a personal column that is not the owner, with how erasure
// treats it. It is not called Column because migrate.Column already
// describes a column in a schema snapshot.
type Field struct {
	Name  string
	Holds Holds
	Erase Treatment
}

// Data declares what one table holds, whose it is, whether changes to
// it are audited and how it is erased. It is not called Table because
// migrate.Table already describes a table in a schema snapshot.
type Data struct {
	Name string
	// Key is the primary key columns; "rowid" alone for a table
	// without one.
	Key  []string
	Kind Kind
	// Owner is zero when the table has no owner.
	Owner    Owner
	Fields   []Field
	Recorded []string
	// NotAudited is the reason the table is left out of the audit log;
	// empty means it is audited.
	NotAudited string
	Erase      Treatment
	// KeepReason is required when Erase is EraseKeep.
	KeepReason string
	Export     bool
	// Namespace is set by Declare from the Set, never by callers, so a
	// merged schema still knows which package declared each table.
	Namespace string
}

// Declare records what a table holds beside the migrations that create
// it, and stamps the Set's namespace on the declaration. It returns the
// Set so it chains like Add.
func (set *Set) Declare(decl Data) *Set {
	decl.Namespace = set.namespace
	set.declarations = append(set.declarations, decl)
	return set
}

// Retire records that this set's migrations drop tables another
// package declared. A set whose migration drops another package's table
// retires its declaration, so the declaration ends where the table
// does. Merge applies it to earlier inputs only.
func (set *Set) Retire(names ...string) *Set {
	set.retired = append(set.retired, names...)
	return set
}

// retire removes the accumulated declarations of the named tables.
func (set *Set) retire(names []string) {
	set.declarations = slices.DeleteFunc(set.declarations,
		func(decl Data) bool { return slices.Contains(names, decl.Name) })
}

// Declarations returns the declarations in composed order, each with
// its Namespace filled.
func (set *Set) Declarations() []Data {
	return append([]Data(nil), set.declarations...)
}
