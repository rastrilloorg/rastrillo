package tmpl

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"html/template"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"text/template/parse"

	"amadan.net/rastrillo/rastrillo/internal/tmplescape"
	"amadan.net/rastrillo/rastrillo/tmpl/tmplrt"
)

// fingerprintEnv is what besides the trees decides the bytes
// html/template writes for a set.
type fingerprintEnv struct {
	// goVersion is the release escaping was forked from. A new Go may
	// escape differently, so code compiled under another is not reused.
	goVersion string
	// generatorVersion is tmplrt.GeneratorVersion: code written by
	// another version of the compiler is not reused either.
	generatorVersion int
	// metaContentURLEscape is the process's GODEBUG
	// htmlmetacontenturlescape, which changes how a URL in a meta refresh
	// is escaped.
	metaContentURLEscape bool
	// missingKey is the set's missingkey option: "default", "zero" or
	// "error".
	missingKey string
}

func currentEnv(missingKey string) fingerprintEnv {
	return fingerprintEnv{
		goVersion:            tmplescape.GoVersion,
		generatorVersion:     tmplrt.GeneratorVersion,
		metaContentURLEscape: metaContentURLEscape(),
		missingKey:           missingKey,
	}
}

// metaContentURLEscape is the meta-refresh URL escaping this process's
// html/template applies. Generate-time escaping has to use the value the
// app will run with, so capture records it.
var metaContentURLEscape = sync.OnceValue(probeMetaContentURLEscape)

// probeMetaContentURLEscape asks html/template rather than parsing
// GODEBUG: the setting can come from the environment, a //go:debug line
// or the main module's go version, and only the runtime knows which won.
func probeMetaContentURLEscape() bool {
	probe := template.Must(
		template.New("").
			Parse(`<meta http-equiv="refresh" content="0;url={{.}}">`),
	)
	var builder strings.Builder
	if err := probe.Execute(&builder, "javascript:x"); err != nil {
		return true
	}
	return !strings.Contains(builder.String(), "javascript:x")
}

// fingerprint names the compiled code that renders entry. It hashes
// entry's tree and every template entry reaches through {{template}},
// resolved in trees, so a set that overrides a partial gets a different
// fingerprint for everything that calls it. It hashes the name and Go
// type of every function those templates call, and env.
//
// The trees must be as parsed. html/template's escaping edits them in
// place, so a fingerprint taken after a set executed would match
// nothing. The escaped trees need not be hashed: escaping is decided by
// the parsed trees and env, except in a set that defines a template
// under a name escaping derives, which is never compiled (treesLocked).
func fingerprint(
	entry string,
	trees map[string]*parse.Tree,
	funcs FuncMap,
	env fingerprintEnv,
) string {
	called, used := reach(entry, trees)

	digest := sha256.New()
	field(digest, "rastrillo tmpl fingerprint 1")
	field(digest, env.goVersion)
	field(digest, strconv.Itoa(env.generatorVersion))
	field(digest, strconv.FormatBool(env.metaContentURLEscape))
	field(digest, env.missingKey)
	field(digest, entry)
	for _, name := range called {
		if tree := trees[name]; tree != nil && tree.Root != nil {
			field(digest, "define")
			field(digest, name)
			field(digest, tree.Root.String())
		} else {
			// Defining it later has to change the fingerprint.
			field(digest, "undefined")
			field(digest, name)
		}
	}
	for _, name := range used {
		field(digest, "func")
		field(digest, name)
		if fn, ok := funcs[name]; ok {
			field(digest, reflect.TypeOf(fn).String())
		} else {
			field(digest, "builtin")
		}
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// field writes text length-first, so that no two lists of fields hash the
// same bytes.
func field(digest hash.Hash, text string) {
	var varint [binary.MaxVarintLen64]byte
	digest.Write(varint[:binary.PutUvarint(varint[:], uint64(len(text)))])
	digest.Write([]byte(text))
}

// reach returns, sorted, the templates entry reaches (entry included)
// and the functions they call.
func reach(entry string, trees map[string]*parse.Tree) (called, used []string) {
	seen := map[string]bool{entry: true}
	funcs := map[string]bool{}
	queue := []string{entry}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		tree := trees[name]
		if tree == nil || tree.Root == nil {
			continue
		}
		walk(tree.Root, func(node parse.Node) {
			switch concrete := node.(type) {
			case *parse.TemplateNode:
				if !seen[concrete.Name] {
					seen[concrete.Name] = true
					queue = append(queue, concrete.Name)
				}
			case *parse.IdentifierNode:
				funcs[concrete.Ident] = true
			}
		})
	}
	for name := range seen {
		called = append(called, name)
	}
	for name := range funcs {
		used = append(used, name)
	}
	slices.Sort(called)
	slices.Sort(used)
	return called, used
}

// walk calls visit for node and every node under it.
func walk(node parse.Node, visit func(parse.Node)) {
	visit(node)
	switch concrete := node.(type) {
	case *parse.ListNode:
		if concrete == nil {
			return
		}
		for _, child := range concrete.Nodes {
			walk(child, visit)
		}
	case *parse.ActionNode:
		walk(concrete.Pipe, visit)
	case *parse.PipeNode:
		if concrete == nil {
			return
		}
		for _, command := range concrete.Cmds {
			walk(command, visit)
		}
	case *parse.CommandNode:
		for _, argument := range concrete.Args {
			walk(argument, visit)
		}
	case *parse.ChainNode:
		walk(concrete.Node, visit)
	case *parse.IfNode:
		walkBranch(&concrete.BranchNode, visit)
	case *parse.RangeNode:
		walkBranch(&concrete.BranchNode, visit)
	case *parse.WithNode:
		walkBranch(&concrete.BranchNode, visit)
	case *parse.TemplateNode:
		walk(concrete.Pipe, visit)
	}
}

func walkBranch(branch *parse.BranchNode, visit func(parse.Node)) {
	walk(branch.Pipe, visit)
	walk(branch.List, visit)
	walk(branch.ElseList, visit)
}
