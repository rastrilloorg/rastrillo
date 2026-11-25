package counts

import (
	"go/ast"
	"go/token"

	"github.com/uudashr/gocognit"
)

// farShort is how many lines of code a one- or two-letter name may be
// used from where it is declared. Past it, a reader has forgotten what
// it stands for.
const farShort = 10

// funcNode is a function or function literal, found again from the
// lines authored gave it.
type funcNode struct {
	declaration ast.Node // the *ast.FuncDecl or *ast.FuncLit
	body        *ast.BlockStmt
	function    *ast.FuncDecl // declaration, or one made to measure it
}

// functionNodes indexes the file's functions and literals by first and
// last line, outermost first: where two start and end on the same
// lines, the outer one is the unit.
func (file *sourceFile) functionNodes() map[int]map[int]funcNode {
	found := map[int]map[int]funcNode{}
	add := func(first token.Pos, node funcNode) {
		start, end := file.line(first), file.line(node.declaration.End())
		if found[start] == nil {
			found[start] = map[int]funcNode{}
		}
		if _, taken := found[start][end]; !taken {
			found[start][end] = node
		}
	}
	ast.Inspect(file.syntax, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.FuncDecl:
			if typed.Body != nil {
				first := typed.Pos()
				if typed.Doc != nil {
					first = typed.Doc.Pos()
				}
				add(first, funcNode{typed, typed.Body, typed})
			}
		case *ast.FuncLit:
			made := &ast.FuncDecl{
				Name: ast.NewIdent("literal"), Type: typed.Type,
				Body: typed.Body,
			}
			add(typed.Pos(), funcNode{typed, typed.Body, made})
		}
		return true
	})
	return found
}

func cognitive(node funcNode) int {
	return gocognit.Complexity(node.function)
}

// nesting is the depth of the deepest run of blocks that branch or
// loop: if, for, range, switch, type switch and select. An else if
// continues its if, so it is not one deeper.
func nesting(body *ast.BlockStmt) int {
	return deepest(body, 0)
}

// deepest is the deepest nesting under node, which sits at depth.
func deepest(node ast.Node, depth int) int {
	most := depth
	ast.Inspect(node, func(child ast.Node) bool {
		if child == node {
			return true
		}
		switch typed := child.(type) {
		case *ast.IfStmt:
			most = max(most, ifDepth(typed, depth))
		case *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt,
			*ast.TypeSwitchStmt, *ast.SelectStmt:
			most = max(most, deepest(child, depth+1), depth+1)
		default:
			return true
		}
		return false
	})
	return most
}

// ifDepth reads an if statement and the else-if chain it starts as one
// level; each branch is a block one level in.
func ifDepth(statement *ast.IfStmt, depth int) int {
	most := depth + 1
	for _, part := range []ast.Node{
		statement.Init, statement.Cond, statement.Body,
	} {
		if part != nil {
			most = max(most, deepest(part, depth+1))
		}
	}
	switch other := statement.Else.(type) {
	case *ast.IfStmt:
		most = max(most, ifDepth(other, depth))
	case *ast.BlockStmt:
		most = max(most, deepest(other, depth+1))
	}
	return most
}

// farShortNames counts the one- and two-letter variables and parameters
// declared in the function and used more than farShort lines of code
// from their declaration.
func (file *sourceFile) farShortNames(node funcNode) int {
	ranks := file.lines.ranks()
	//lint:ignore SA1019 Counts must work on apps that do not type-check, so names resolve by parse-time scopes, not go/types.
	far := map[*ast.Object]bool{}
	ast.Inspect(node.declaration, func(child ast.Node) bool {
		name, ok := child.(*ast.Ident)
		if !ok || name.Obj == nil || name.Obj.Kind != ast.Var {
			return true
		}
		declared := name.Obj.Pos()
		if len(name.Name) > 2 || name.Name == "_" || !declared.IsValid() ||
			declared < node.declaration.Pos() || name.Pos() == declared {
			return true
		}
		if ranks[file.line(name.Pos())]-ranks[file.line(declared)] > farShort {
			far[name.Obj] = true
		}
		return true
	})
	return len(far)
}
