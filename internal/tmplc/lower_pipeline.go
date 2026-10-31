package tmplc

import (
	"fmt"
	"strconv"
	"text/template/parse"
)

// pipeline lowers a pipeline's commands, each given the value of the one
// before. Its variables, if it declares any, are the caller's to set.
func (lowerer *lowerer) pipeline(pipe *parse.PipeNode) (expr, error) {
	var value expr
	for _, command := range pipe.Cmds {
		next, err := lowerer.command(command, value)
		if err != nil {
			return nil, err
		}
		// An escaper's result is a string, never an interface, and a
		// parenthesized pipeline's value has been taken out of one.
		switch next.(type) {
		case escaperCall, commandValue:
		default:
			next = commandValue{x: next}
		}
		value = next
	}
	return value, nil
}

// builtins are text/template's builtin functions, with the number of
// arguments each must have and whether it takes more.
var builtins = map[string]struct {
	fixed    int
	variadic bool
}{
	"and": {1, true}, "or": {1, true}, "not": {1, false},
	"call": {1, true}, "index": {1, true}, "slice": {1, true},
	"len": {1, false}, "print": {0, true}, "printf": {1, true},
	"println": {0, true}, "html": {0, true}, "js": {0, true},
	"urlquery": {0, true}, "eq": {1, true}, "ne": {2, false},
	"lt": {2, false}, "le": {2, false}, "gt": {2, false}, "ge": {2, false},
}

// command lowers one command of a pipeline. final is the value piped
// into it, nil for the first.
func (lowerer *lowerer) command(
	command *parse.CommandNode, final expr,
) (expr, error) {
	rest := command.Args[1:]
	switch first := command.Args[0].(type) {
	case *parse.IdentifierNode:
		return lowerer.identifierCommand(command, first, final)
	case *parse.FieldNode:
		at := first.String()
		return lowerer.fieldChain(dot{}, first.Ident, rest, final, at)
	case *parse.ChainNode:
		receiver, err := lowerer.chainReceiver(first)
		if err != nil {
			return nil, err
		}
		at := first.String()
		return lowerer.fieldChain(receiver, first.Field, rest, final, at)
	case *parse.VariableNode:
		return lowerer.variableCommand(command, first, final)
	case *parse.PipeNode:
		if failed := notAFunction(command, final); failed != nil {
			return failed, nil
		}
		return lowerer.nested(first)
	case *parse.NilNode:
		return failure{
			at: command.String(), msg: "nil is not a command", final: final,
		}, nil
	}
	if failed := notAFunction(command, final); failed != nil {
		return failed, nil
	}
	switch first := command.Args[0].(type) {
	case *parse.DotNode:
		return dot{}, nil
	case *parse.BoolNode, *parse.NumberNode, *parse.StringNode:
		return constant{node: first}, nil
	}
	return nil, unsupported("uses the command " + command.String())
}

// identifierCommand is a command that calls the function first names.
func (lowerer *lowerer) identifierCommand(
	command *parse.CommandNode, first *parse.IdentifierNode, final expr,
) (expr, error) {
	args, err := lowerer.args(command.Args[1:])
	if err != nil {
		return nil, err
	}
	callee := ""
	if len(command.Args) > 1 {
		callee = command.Args[1].String()
	}
	at := command.String()
	return lowerer.function(first.Ident, args, final, at, callee)
}

// variableCommand is a command that starts with the variable first.
func (lowerer *lowerer) variableCommand(
	command *parse.CommandNode, first *parse.VariableNode, final expr,
) (expr, error) {
	goName, err := lowerer.lookup(first.Ident[0])
	if err != nil {
		return nil, err
	}
	if len(first.Ident) == 1 {
		if failed := notAFunction(command, final); failed != nil {
			return failed, nil
		}
		return variable{goName}, nil
	}
	names, rest := first.Ident[1:], command.Args[1:]
	return lowerer.fieldChain(
		variable{goName}, names, rest, final, first.String())
}

// notAFunction is the failure for arguments given to what is not a
// function, or nil.
func notAFunction(command *parse.CommandNode, final expr) expr {
	if len(command.Args) > 1 || final != nil {
		return failure{
			at: command.String(),
			msg: "can't give argument to non-function " +
				command.Args[0].String(),
			final: final,
		}
	}
	return nil
}

// nested lowers a parenthesized pipeline.
func (lowerer *lowerer) nested(pipe *parse.PipeNode) (expr, error) {
	if len(pipe.Decl) > 0 {
		// Its variables would outlive the expression that sets them only
		// on the paths that evaluate it, which Go's scopes cannot say.
		return nil, unsupported(
			"declares a variable inside parentheses in " + pipe.String())
	}
	return lowerer.pipeline(pipe)
}

// chainReceiver is the value (pipe).Field reads Field on.
func (lowerer *lowerer) chainReceiver(chain *parse.ChainNode) (expr, error) {
	switch node := chain.Node.(type) {
	case *parse.NilNode:
		return failure{
			at:  chain.String(),
			msg: "indirection through explicit nil in " + chain.String(),
		}, nil
	case *parse.PipeNode:
		return lowerer.nested(node)
	case *parse.IdentifierNode:
		return lowerer.function(node.Ident, nil, nil, node.String(), "")
	}
	return nil, unsupported("uses the chain " + chain.String())
}

// fieldChain is names read in turn on receiver, the last given args and
// final.
func (lowerer *lowerer) fieldChain(
	receiver expr, names []string, argNodes []parse.Node, final expr,
	at string,
) (expr, error) {
	if len(argNodes) == 0 && final == nil {
		return fieldOf{receiver: receiver, names: names, at: at}, nil
	}
	args, err := lowerer.args(argNodes)
	if err != nil {
		return nil, err
	}
	if len(names) > 1 {
		receiver = fieldOf{
			receiver: receiver, names: names[:len(names)-1], at: at,
		}
	}
	return methodCall{
		receiver: receiver, name: names[len(names)-1],
		args: args, final: final, at: at,
	}, nil
}

// function is a call of the function called name. callee is the source
// of its first argument.
func (lowerer *lowerer) function(
	name string, args []arg, final expr, at, callee string,
) (expr, error) {
	if fn, ok := escapers[name]; ok {
		return escaperCall{fn: fn, args: args, final: final}, nil
	}
	// The set's own functions come first, as in text/template's
	// findFunction: one may replace a builtin.
	if sig, ok := lowerer.set.Funcs[name]; ok {
		fn := lowerer.file.fn(name, sig)
		lowerer.cur.funcs[fn.field] = fn
		return funcCall{fn: fn, args: args, final: final, at: at}, nil
	}
	builtin, ok := builtins[name]
	if !ok {
		msg := strconv.Quote(name) + " is not a defined function"
		return failure{at: at, msg: msg, final: final}, nil
	}
	count := len(args)
	if final != nil {
		count++
	}
	switch {
	case builtin.variadic && count < builtin.fixed:
		msg := fmt.Sprintf(
			"wrong number of args for %s: want at least %d got %d",
			name, builtin.fixed, len(args))
		return failure{at: at, msg: msg, final: final}, nil
	case !builtin.variadic && count != builtin.fixed:
		msg := fmt.Sprintf("wrong number of args for %s: want %d got %d",
			name, builtin.fixed, count)
		return failure{at: at, msg: msg, final: final}, nil
	}
	if name == "and" || name == "or" {
		return andOr{or: name == "or", args: args, final: final}, nil
	}
	return builtinCall{
		name: name, args: args, final: final, at: at, callee: callee,
	}, nil
}

// args lowers a command's arguments.
func (lowerer *lowerer) args(nodes []parse.Node) ([]arg, error) {
	out := make([]arg, 0, len(nodes))
	for _, node := range nodes {
		argument, err := lowerer.operand(node)
		if err != nil {
			return nil, err
		}
		out = append(out, argument)
	}
	return out, nil
}

// operand lowers an argument, which text/template evaluates without
// taking it out of an interface unless it is a parenthesized pipeline.
func (lowerer *lowerer) operand(node parse.Node) (arg, error) {
	switch node := node.(type) {
	case *parse.BoolNode, *parse.StringNode, *parse.NumberNode,
		*parse.NilNode:
		return arg{c: node}, nil
	case *parse.DotNode:
		return arg{x: dot{}}, nil
	case *parse.FieldNode:
		return arg{x: fieldOf{
			receiver: dot{}, names: node.Ident, at: node.String(),
		}}, nil
	case *parse.VariableNode:
		return lowerer.variableOperand(node)
	case *parse.PipeNode:
		value, err := lowerer.nested(node)
		return arg{x: value}, err
	case *parse.IdentifierNode:
		at := node.String()
		value, err := lowerer.function(node.Ident, nil, nil, at, "")
		return arg{x: value}, err
	case *parse.ChainNode:
		receiver, err := lowerer.chainReceiver(node)
		if err != nil {
			return arg{}, err
		}
		return arg{x: fieldOf{
			receiver: receiver, names: node.Field, at: node.String(),
		}}, nil
	}
	return arg{}, unsupported("uses the argument " + node.String())
}

func (lowerer *lowerer) variableOperand(
	node *parse.VariableNode,
) (arg, error) {
	goName, err := lowerer.lookup(node.Ident[0])
	if err != nil {
		return arg{}, err
	}
	if len(node.Ident) == 1 {
		return arg{x: variable{goName}}, nil
	}
	return arg{x: fieldOf{
		receiver: variable{goName}, names: node.Ident[1:], at: node.String(),
	}}, nil
}
