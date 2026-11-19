package vendored

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/css"
)

// ErrUnreadable is what Slice and StyledHooks wrap when they cannot be
// sure where the stylesheet's rules begin and end, or what one of them
// applies to. A guess there could unstyle a page, so the caller serves
// the whole stylesheet instead.
var ErrUnreadable = errors.New("unreadable stylesheet")

// keptWhole lists the at-rules that do not select elements, so no set
// of hooks can make them useless.
var keptWhole = map[string]bool{
	"@property": true, "@keyframes": true, "@font-face": true,
	"@counter-style": true, "@page": true,
}

// ruleGroups and tokenGroups list the at-rules that hold style rules,
// each kept on its own merits. The parser reads a ruleGroup's block as
// rules; it hands a tokenGroup's block back as tokens, so its block is
// read again as a stylesheet of its own.
var (
	ruleGroups = map[string]bool{
		"@media": true, "@supports": true, "@layer": true,
	}
	tokenGroups = map[string]bool{"@container": true, "@scope": true}
)

// A rule is one rule of a stylesheet as the bytes it spans, so a kept
// rule is written back exactly as it was read. A group has no text of
// its own: it is its opening, up to and including its brace, the rules
// inside, and its closing brace; emptied is what it leaves when none
// of its rules are kept.
type rule struct {
	text     []byte
	selector []css.Token // nil for an at-rule or comment: always kept
	nested   bool        // its block holds a rule or an at-rule
	named    []string    // hooks named by every selector it holds
	opening  []byte
	rules    []rule
	closing  []byte
	emptied  []byte
}

// Slice returns stylesheet with only the rules a page carrying hooks
// can show: a style rule is kept with those of its comma-separated
// selectors that need no hook outside hooks, no value of a hook that
// hooks' ShownValue entries rule out, and, when hooks hold
// ListedClasses, no class outside their ShownClass entries; a group
// such as @media keeps its kept rules and goes when none are left. A
// kept rule comes back byte for byte but for the selectors it loses,
// so a minified stylesheet stays minified.
func Slice(stylesheet []byte, hooks map[string]bool) ([]byte, error) {
	rules, err := read(stylesheet, 0)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	writeKept(&out, rules, readShown(hooks))
	return out.Bytes(), nil
}

// StyledHooks returns the hooks the stylesheet's selectors name,
// wherever they stand in them, :not() included.
func StyledHooks(stylesheet []byte) (map[string]bool, error) {
	rules, err := read(stylesheet, 0)
	if err != nil {
		return nil, err
	}
	hooks := map[string]bool{}
	addNamed(hooks, rules)
	return hooks, nil
}

func writeKept(out *bytes.Buffer, rules []rule, page shown) {
	for _, rule := range rules {
		switch {
		case rule.opening != nil:
			var inside bytes.Buffer
			writeKept(&inside, rule.rules, page)
			if inside.Len() == 0 {
				out.Write(rule.emptied)
				continue
			}
			out.Write(rule.opening)
			out.Write(inside.Bytes())
			out.Write(rule.closing)
		case rule.selector == nil:
			out.Write(rule.text)
		default:
			out.Write(keptText(rule, page))
		}
	}
}

func addNamed(hooks map[string]bool, rules []rule) {
	for _, rule := range rules {
		for _, name := range rule.named {
			hooks[name] = true
		}
		addNamed(hooks, rule.rules)
	}
}

// A ruleReader walks the parser's grammar and cuts the rules it finds
// out of stylesheet by the parser's offsets, rather than writing them
// back from its tokens, which it normalises. base is where stylesheet
// starts in the file, for the errors.
type ruleReader struct {
	parser     *css.Parser
	stylesheet []byte
	base       int
}

func read(stylesheet []byte, base int) ([]rule, error) {
	if err := checkBrackets(stylesheet, base); err != nil {
		return nil, err
	}
	parser := css.NewParser(input(stylesheet), false)
	reader := &ruleReader{parser: parser, stylesheet: stylesheet, base: base}
	rules, _, err := reader.rules(false)
	return rules, err
}

// input hands the parser a copy of stylesheet, because the parser
// writes a terminating byte past the end of what it reads, and the
// caller's stylesheet may be shared between goroutines.
func input(stylesheet []byte) *parse.Input {
	private := make([]byte, len(stylesheet), len(stylesheet)+1)
	copy(private, stylesheet)
	return parse.NewInputBytes(private)
}

// checkBrackets refuses a stylesheet whose brackets do not pair by
// kind, or that holds a bad string or url. The parser closes a block
// at any closing bracket where a browser waits for the matching one,
// so only on a stylesheet that passes do the two agree where every
// rule ends.
func checkBrackets(stylesheet []byte, base int) error {
	lexer := css.NewLexer(input(stylesheet))
	var open []byte
	for offset := base; ; {
		token, data := lexer.Next()
		switch token {
		case css.ErrorToken:
			if lexer.Err() != io.EOF {
				return unreadable("an unreadable byte", offset)
			}
			if len(open) > 0 {
				return unreadable("an unclosed block", offset)
			}
			return nil
		case css.BadStringToken, css.BadURLToken:
			return unreadable("a bad string or url", offset)
		case css.LeftBraceToken, css.LeftBracketToken,
			css.LeftParenthesisToken, css.FunctionToken:
			open = append(open, closer[data[len(data)-1]])
		case css.RightBraceToken, css.RightBracketToken,
			css.RightParenthesisToken:
			if len(open) == 0 || open[len(open)-1] != data[0] {
				return unreadable("an unpaired "+string(data), offset)
			}
			open = open[:len(open)-1]
		}
		offset += len(data)
	}
}

// closer maps each opening bracket to the one that closes it.
var closer = map[byte]byte{'{': '}', '[': ']', '(': ')'}

func unreadable(what string, offset int) error {
	return fmt.Errorf("%w: %s at byte %d", ErrUnreadable, what, offset)
}

// rules reads rules to the end of the stylesheet, or with inGroup to
// the end of the group block the parser has just opened, whose closing
// brace it also returns.
func (reader *ruleReader) rules(inGroup bool) ([]rule, []byte, error) {
	var rules []rule
	for {
		start := reader.parser.Offset()
		grammar, token, data := reader.parser.Next()
		var next rule
		var err error
		switch {
		case grammar == css.CommentGrammar:
			next = rule{text: reader.since(start)}
		case grammar == css.BeginRulesetGrammar:
			next, err = reader.styleRule(start)
		case grammar == css.AtRuleGrammar:
			next = rule{text: reader.since(start)}
		case grammar == css.BeginAtRuleGrammar:
			next, err = reader.atRule(start, string(data))
		case grammar == css.EndAtRuleGrammar && inGroup &&
			token == css.RightBraceToken:
			return rules, reader.since(start), nil
		case grammar == css.ErrorGrammar && !inGroup &&
			!reader.parser.HasParseError() && reader.parser.Err() == io.EOF:
			return rules, nil, nil
		default:
			err = reader.unreadable("unexpected " + grammar.String())
		}
		if err != nil {
			return nil, nil, err
		}
		rules = append(rules, next)
	}
}

func (reader *ruleReader) styleRule(start int) (rule, error) {
	selector := slices.Clone(reader.parser.Values())
	if escapedFunction(selector) {
		return rule{}, reader.unreadable("an escaped function name")
	}
	named, nested, err := reader.skipBlock()
	if err != nil {
		return rule{}, err
	}
	named = append(hooksNamed(selector), named...)
	text := reader.since(start)
	return rule{text: text, selector: selector, nested: nested,
		named: named}, nil
}

func (reader *ruleReader) atRule(start int, name string) (rule, error) {
	named := hooksNamed(reader.parser.Values())
	opening := reader.since(start)
	switch {
	case keptWhole[name]:
		nested, _, err := reader.skipBlock()
		named = append(named, nested...)
		return rule{text: reader.since(start), named: named}, err
	case ruleGroups[name]:
		emptied := layerStatement(name, reader.parser.Values(), opening)
		rules, closing, err := reader.rules(true)
		return rule{named: named, opening: opening, rules: rules,
			closing: closing, emptied: emptied}, err
	case tokenGroups[name]:
		if _, _, err := reader.skipBlock(); err != nil {
			return rule{}, err
		}
		// The whitespace before the closing brace belongs to no rule
		// inside, so it goes with the brace: the minifier leaves a
		// container query's block as written, and a sliced block that
		// keeps every rule must come back byte for byte.
		inside := start + len(opening)
		block := reader.stylesheet[inside : reader.parser.Offset()-len("}")]
		end := inside + len(bytes.TrimRight(block, " \t\n\r\f"))
		rules, err := read(reader.stylesheet[inside:end], reader.base+inside)
		return rule{named: named, opening: opening, rules: rules,
			closing: reader.stylesheet[end:reader.parser.Offset()]}, err
	}
	return rule{}, reader.unreadable("a block of " + name)
}

// layerStatement returns what a named @layer block leaves when none of
// its rules are kept: the statement "@layer name;", because a layer
// ranks where its name first appears, and dropping the name would let
// a later block of the same layer outrank the layers declared between.
// Any other group, an anonymous layer included, leaves nothing.
func layerStatement(name string, prelude []css.Token, opening []byte) []byte {
	named := slices.ContainsFunc(prelude, func(token css.Token) bool {
		return token.TokenType != css.WhitespaceToken
	})
	if name != "@layer" || !named {
		return nil
	}
	head := bytes.TrimRight(opening[:len(opening)-len("{")], " \t\n\r\f")
	return append(slices.Clip(head), ';')
}

// skipBlock reads past the end of the block the parser has just
// opened, returning the hooks named by the selectors of rules nested
// in it, and whether it holds any rule or at-rule.
func (reader *ruleReader) skipBlock() ([]string, bool, error) {
	var named []string
	nested := false
	for depth := 1; depth > 0; {
		grammar, token, _ := reader.parser.Next()
		switch grammar {
		case css.BeginRulesetGrammar:
			named = append(named, hooksNamed(reader.parser.Values())...)
			depth++
			nested = true
		case css.BeginAtRuleGrammar:
			depth++
			nested = true
		case css.AtRuleGrammar, css.QualifiedRuleGrammar:
			nested = true
		case css.EndRulesetGrammar, css.EndAtRuleGrammar:
			if token != css.RightBraceToken {
				return nil, false, reader.unreadable("an unclosed block")
			}
			depth--
		case css.ErrorGrammar:
			return nil, false, reader.unreadable("a parse error")
		}
	}
	return named, nested, nil
}

func (reader *ruleReader) since(start int) []byte {
	return reader.stylesheet[start:reader.parser.Offset()]
}

func (reader *ruleReader) unreadable(what string) error {
	var parseError *parse.Error
	if errors.As(reader.parser.Err(), &parseError) {
		what += " (" + parseError.Message + ")"
	}
	return unreadable(what, reader.base+reader.parser.Offset())
}

// escapedFunction reports whether a selector calls a function whose
// name is written with a CSS escape, such as n\6ft() for not(): what
// it needs is read off the name's bytes, so an escaped :not() would be
// taken for a hook the rule requires, and its rule dropped.
func escapedFunction(selector []css.Token) bool {
	return slices.ContainsFunc(selector, func(token css.Token) bool {
		return token.TokenType == css.FunctionToken &&
			bytes.ContainsRune(token.Data, '\\')
	})
}
