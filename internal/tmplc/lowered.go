package tmplc

import "text/template/parse"

// The lowered form of one template. It says nothing of types: the
// writer gives each expr one as it writes it (see val in types.go), a
// static Go type where the data's type is known and a reflect.Value,
// text/template's own representation, where it is not.
type (
	block []stmt
	stmt  interface{ isStmt() }
	expr  interface{ isExpr() }

	// writeText writes text bytes as they are in the escaped tree.
	writeText struct{ text string }
	// writeValue writes the string an action's escapers made.
	writeValue struct{ value expr }
	// printValue writes a value as text/template prints one.
	printValue struct {
		value expr
		at    string
	}
	// declare makes each of vars, Go variables standing for template
	// variables, and sets it to value; assign sets existing ones.
	declare struct {
		vars  []string
		value expr
	}
	assign struct {
		vars  []string
		value expr
	}
	ifStmt struct {
		cond      expr
		then, els block
	}
	// withStmt runs then with value as dot, if value is true.
	withStmt struct {
		value     expr
		then, els block
	}
	rangeStmt struct {
		over expr
		at   string // the pipeline, for errors
		// vars are the variables the range sets for each element: none,
		// the element, or the index or key and the element. Each holds
		// over itself first, which is what {{else}} sees. assign is
		// whether they are existing variables, set with =; otherwise the
		// range declares them. two is whether there are two, which
		// changes what the range can range over.
		vars   []string
		assign bool
		two    bool
		body   block
		els    block
		// end labels the statement's end, for a {{break}} in its else
		// branch; "" when none breaks there.
		end string
	}
	// breakStmt is {{break}}: it ends the range body it is in, or, in a
	// range's {{else}}, the range statement (to), as text/template's
	// walkRange recovers it there.
	breakStmt    struct{ to string }
	continueStmt struct{}
	// templateCall is {{template}}: callee is the template's escaped
	// name, and arg its dot, nil for none.
	templateCall struct {
		callee string
		arg    expr
		at     string
	}

	dot      struct{}
	variable struct{ name string } // the Go variable
	// constant is a bool, string or number written in the template, or
	// nil, as a value of its own: text/template's idealConstant.
	constant struct{ node parse.Node }
	// commandValue is what a pipeline's command evaluated to, taken out
	// of an empty interface as text/template's evalPipeline does at the
	// end of every command.
	commandValue struct{ x expr }
	// fieldOf is a chain of names on receiver, such as .Owner.Name, none
	// of them given arguments.
	fieldOf struct {
		receiver expr
		names    []string
		at       string
	}
	// methodCall is .name on receiver given arguments, the last of
	// them final when it is not nil.
	methodCall struct {
		receiver expr
		name     string
		args     []arg
		final    expr
		at       string
	}
	// funcCall is a function of the set's FuncMap.
	funcCall struct {
		fn    *fn
		args  []arg
		final expr
		at    string
	}
	// builtinCall is one of text/template's builtins other than and
	// and or. callee is how call's function was written, which call
	// passes on for errors; "" for one piped in.
	builtinCall struct {
		name   string
		args   []arg
		final  expr
		at     string
		callee string
	}
	// andOr is and or or, which evaluate their arguments only until one
	// decides the result.
	andOr struct {
		or    bool
		args  []arg
		final expr
	}
	// escaperCall is one of html/template's escapers, which escaping put
	// at the end of each action.
	escaperCall struct {
		fn    string // its name in package tmplrt
		args  []arg
		final expr
	}
	// failure is an error execution meets when it gets here, which the
	// compiler knows of already: text/template reports it only then.
	// final is the value piped into the command that fails, nil for
	// none: the commands before it run first, and may fail first.
	failure struct {
		at, msg string
		final   expr
	}
)

// arg is an argument: a constant, which text/template reads according to
// the parameter's type, or a value.
type arg struct {
	c parse.Node // a *parse.BoolNode, StringNode, NumberNode or NilNode
	x expr
}

func (writeText) isStmt()    {}
func (writeValue) isStmt()   {}
func (printValue) isStmt()   {}
func (declare) isStmt()      {}
func (assign) isStmt()       {}
func (ifStmt) isStmt()       {}
func (withStmt) isStmt()     {}
func (rangeStmt) isStmt()    {}
func (breakStmt) isStmt()    {}
func (continueStmt) isStmt() {}
func (templateCall) isStmt() {}

func (dot) isExpr()          {}
func (variable) isExpr()     {}
func (constant) isExpr()     {}
func (commandValue) isExpr() {}
func (fieldOf) isExpr()      {}
func (methodCall) isExpr()   {}
func (funcCall) isExpr()     {}
func (builtinCall) isExpr()  {}
func (andOr) isExpr()        {}
func (escaperCall) isExpr()  {}
func (failure) isExpr()      {}

// unsupported is why a template is not compiled.
type unsupported string

func (reason unsupported) Error() string { return string(reason) }

// body is one template lowered, and the Go function it is written as.
type body struct {
	name  string // the template's escaped name
	fn    string // the Go function
	code  block
	funcs map[string]*fn // the FuncMap functions it calls, by field
}

// terminates reports whether code always ends in a {{break}} or
// {{continue}}, which are a return or a goto in the generated code, and
// a terminating statement in Go's sense.
func terminates(code block) bool {
	if len(code) == 0 {
		return false
	}
	switch last := code[len(code)-1].(type) {
	case breakStmt, continueStmt:
		return true
	case ifStmt:
		return terminates(last.then) && terminates(last.els)
	case withStmt:
		return terminates(last.then) && terminates(last.els)
	}
	return false
}

// breaksTo reports whether a {{break}} in code ends the range labelled
// end.
func breaksTo(code block, end string) bool {
	for _, statement := range code {
		switch statement := statement.(type) {
		case breakStmt:
			if statement.to == end {
				return true
			}
		case ifStmt:
			if breaksTo(statement.then, end) || breaksTo(statement.els, end) {
				return true
			}
		case withStmt:
			if breaksTo(statement.then, end) || breaksTo(statement.els, end) {
				return true
			}
		case rangeStmt:
			if breaksTo(statement.els, end) {
				return true
			}
		}
	}
	return false
}
