package op

// schemaFor's refusals. Each names the Go field and says the fix, and
// Define wraps it with the operation's ID, so a declaration the doors
// would read differently from its schema stops the app before it serves.
const (
	msgUntagged = `field %s.%s has no JSON name. Tag it json:"%s"`
	msgWireName = `field %s.%s is %q on the wire. ` +
		`Use %q as its JSON name so errors use the same field name`
	msgUnsupported = `field %s.%s is a %s, which JSON can't carry. ` +
		`Use a string, number, bool, struct, slice, or map with string keys`
	msgTagOption = `field %s.%s has unsupported json option %q. ` +
		`Use only omitzero, omitempty or inline`
	msgPointerInput = `field %s.%s is a pointer in an input. ` +
		`Use op.Opt for a field that may be left out, ` +
		`or op.Nullable for one that may be null`
	msgOptResult = `field %s.%s is an op.Opt or op.Nullable in a result. ` +
		`Use a pointer for a value that may be null`
	msgCycle = `type %s contains itself, ` +
		`which a JSON Schema here can't describe. Break the cycle with an ID`
)
