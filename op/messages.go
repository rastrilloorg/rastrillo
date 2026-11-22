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
	msgNestedNone = `field %s.%s holds op.None, ` +
		`which can only be a whole result. Remove the field`
)

// Define's refusals. Define prefixes each but msgBadID with "op: <id>: ",
// and msgBadID quotes the ID itself, because it may not be one.
const (
	msgBadID = `%q is not an operation name. Write it as <area>.<verb> ` +
		`in lowercase letters, digits and underscores, such as posts.publish`
	msgLongToolName = `its tool name %s is %d characters. ` +
		`Keep <area>_<verb> to 64 or fewer`
	msgNoDescription = `Description is empty. ` +
		`Write one sentence that says what the operation does`
	msgBadAccess = `set Spec.Access to rastrillo.Read or rastrillo.Write`
	msgNilRun    = `the function is nil. ` +
		`Pass the function that does the work`
	msgInputNotStruct = `the input type %s must be a struct. ` +
		`Use a struct for the input`
	msgResultNotStruct = `the result type %s must be a struct or op.None. ` +
		`Use op.None when there is nothing to return`
	msgQueryWrite = `InQuery is only for reads. ` +
		`Remove it, or set Access to rastrillo.Read`
	msgQueryNotFlat = `InQuery needs every input field to be a string, ` +
		`number or bool, but %s is not. Remove InQuery to take a JSON body`
	msgInputNotObject = `the input type %s is not a JSON object ` +
		`on the wire. Use a struct whose fields have JSON names`
	msgResultNotObject = `the result type %s is not a JSON object ` +
		`on the wire. Use a struct whose fields have JSON names, ` +
		`or op.None when there is nothing to return`
)
