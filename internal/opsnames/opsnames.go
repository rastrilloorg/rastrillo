// Package opsnames holds the names every generated operations package
// (gen/ops/<resource>) declares, and the rule that turns a resource
// name into its Go type. It imports nothing, so both sides that need
// the rule can share it: Resource.Validate refuses a resource whose
// type would be one of these names, and the operations emitter
// declares them. The emitter imports the root package, so neither can
// own the list without an import cycle, and a copy in each would let
// Validate pass a name the emitter then declares twice.
package opsnames

import "strings"

// Fixed is every top-level name a gen/ops package declares for itself,
// whatever the resource. A resource whose singular is one of these
// would declare it a second time and the package would not compile.
// opsgen's TestFixedNamesMatchTheEmitter holds this list to what the
// emitter writes.
var Fixed = []string{
	"List", "Get", "Create", "Update", "Delete",
	"ListInput", "Page", "ListResult", "CreateInput", "UpdateInput",
	"ListArgs", "GetArgs", "UpdateArgs", "DeleteArgs",
	"ListOp", "GetOp", "CreateOp", "UpdateOp", "DeleteOp",
	"ValidateCreate", "ValidateUpdate", "Descriptors",
	"checkMoney", "settle", "load", "fromRow", "moneyOf", "items", "update", "heldOf",
	"moneyInSchema", "optionalMoneyInSchema", "moneyOutSchema",
	"listArgsSchema", "idArgsSchema", "createArgsSchema", "updateArgsSchema",
	"resultSchema", "listResultSchema", "noResultSchema",
}

// Pascal title-cases a snake_case name for a Go or sqlc identifier:
// "ticket_types" -> "TicketTypes".
func Pascal(name string) string {
	parts := strings.Split(name, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

// Singular is Pascal after stripping one trailing "s": the per-record
// type and query names' rule, "notes" -> "Note".
func Singular(name string) string {
	return Pascal(strings.TrimSuffix(name, "s"))
}

// ListResultMembers is what ListResult declares beside the page under
// the resource's plural: a resource named next_after would give the
// struct two NextAfter fields.
var ListResultMembers = []string{"NextAfter"}

// ResultMembers is what the result type declares beside the columns.
// A column of one of these names is already refused as a reserved
// column, which Validate's tests pin against this list.
var ResultMembers = []string{"ID", "CreatedAt", "UpdatedAt"}

// Taken reports the generated name a resource would collide with, or
// "": its singular against the package's top-level names, its plural
// against ListResult's members. Each per-resource identifier is checked
// in the scope the emitter declares it in.
func Taken(name string) string {
	if s := Singular(name); contains(Fixed, s) {
		return s
	}
	if p := Pascal(name); contains(ListResultMembers, p) {
		return p
	}
	return ""
}

func contains(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}
