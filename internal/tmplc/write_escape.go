package tmplc

import (
	"fmt"
	"go/types"
	"slices"
	"strings"

	"amadan.net/rastrillo/rastrillo/tmpl/tmplrt"
)

// The escapers' fast paths, by the name of tmplrt's port: the function
// for a string, the one that writes a string to the buffer, and those
// that leave an integer's digits as they are, which tmplrt names.
var (
	stringEscapers = map[string]string{
		"HTMLEscaper":   "HTMLEscaperString",
		"AttrEscaper":   "AttrEscaperString",
		"RCDATAEscaper": "RCDATAEscaperString",
		"URLFilter":     "URLFilterString",
		"URLNormalizer": "URLNormalizerString",
		"URLEscaper":    "URLEscaperString",
		"JSValEscaper":  "JSValEscaperString",
		"JSStrEscaper":  "JSStrEscaperString",
	}
	escapeWriters = map[string]string{
		"HTMLEscaper":   "WriteHTMLEscaped",
		"AttrEscaper":   "WriteAttrEscaped",
		"RCDATAEscaper": "WriteHTMLEscaped",
	}
	decimalEscapers = func() map[string]bool {
		names := map[string]bool{}
		for _, name := range tmplrt.DecimalEscapers {
			names[escapers[name]] = true
		}
		return names
	}()
	// ownPrinting are the escapers that do not print a value that is
	// not a string as fmt.Sprint does, and so may tell a named string
	// type from a string: JSValEscaper marshals it as JSON, which a
	// MarshalJSON or MarshalText method changes.
	ownPrinting = map[string]bool{"JSValEscaper": true, "EvalArgs": true}
)

// escaperChain is the escapers expression ends in, innermost first, and the
// value they escape, when each takes only the value piped into it, as
// escaping writes them.
func escaperChain(expression expr) (fns []string, base expr) {
	for {
		call, ok := expression.(escaperCall)
		if !ok {
			break
		}
		if len(call.args) > 0 || call.final == nil {
			return nil, nil
		}
		fns = append(fns, call.fn)
		expression = call.final
	}
	if len(fns) == 0 {
		return nil, nil
	}
	slices.Reverse(fns)
	return fns, expression
}

// writeEscaped writes the string an action's escapers make of its value:
// for a string, through the fast paths, the last of them straight into
// the buffer; for an integer, its digits, where every escaper leaves
// them as they are.
func (emitter *writer) writeEscaped(expression expr) {
	rt := emitter.file.rt
	fns, base := escaperChain(expression)
	if base == nil {
		emitter.line("b.WriteString(%s)", emitter.eval(expression).code)
		return
	}
	value := emitter.eval(base)
	digits := false // v stands for its own digits
	for index, fn := range fns {
		last := index == len(fns)-1
		if number, signed, ok := plainInt(value); ok && decimalEscapers[fn] {
			if last {
				if signed {
					emitter.line("%s.WriteInt(b, %s)", rt, number)
				} else {
					emitter.line("%s.WriteUint(b, %s)", rt, number)
				}
				return
			}
			digits = true
			continue
		}
		if digits {
			value, digits = emitter.digits(value), false
		}
		if text, _, ok := plainString(value); ok && last &&
			escapeWriters[fn] != "" {
			emitter.line("%s.%s(b, %s)", rt, escapeWriters[fn], text)
			return
		}
		value = emitter.escape(fn, value)
	}
	emitter.line("b.WriteString(%s)", value.code)
}

// digits is the integer value's digits, as a string.
func (emitter *writer) digits(value val) val {
	digits, signed, _ := plainInt(value)
	if signed {
		return val{
			code: emitter.file.rt + ".IntString(" + digits + ")",
			t:    types.Typ[types.String],
		}
	}
	return val{
		code: emitter.file.rt + ".UintString(" + digits + ")",
		t:    types.Typ[types.String],
	}
}

// escape is the escaper fn, by the name of tmplrt's port, applied to value.
func (emitter *writer) escape(fn string, value val) val {
	rt, str := emitter.file.rt, types.Typ[types.String]
	if _, _, ok := plainInt(value); ok && decimalEscapers[fn] {
		return emitter.digits(value)
	}
	if text, exact, ok := plainString(value); ok &&
		(exact || !ownPrinting[fn]) {
		if fast := stringEscapers[fn]; fast != "" {
			return val{code: fmt.Sprintf("%s.%s(%s)", rt, fast, text), t: str}
		}
		return val{code: fmt.Sprintf("%s.%s(%s)", rt, fn, text), t: str}
	}
	return val{
		code: fmt.Sprintf("%s.%s(%s)", rt, fn, emitter.anyOf(value)),
		t:    str,
	}
}

// escaped is an escaper's string, where it is not the action's last,
// held at once: an escaper prints a value by its String method, which
// may read what a later argument changes.
func (emitter *writer) escaped(call escaperCall) val {
	if fns, base := escaperChain(call); base != nil {
		value := emitter.eval(base)
		for _, fn := range fns {
			value = emitter.escape(fn, value)
		}
		return emitter.hold(value)
	}
	var anys []string
	emitter.inOrder(
		call.args,
		call.final,
		func(_ int, argument argVal, _ bool) string {
			value := argument.v
			if argument.c != nil {
				value = emitter.constVal(argument.c)
			}
			anys = append(anys, emitter.anyOf(value))
			return ""
		},
	)
	return emitter.hold(
		val{
			code: fmt.Sprintf(
				"%s.%s(%s)",
				emitter.file.rt,
				call.fn,
				strings.Join(anys, ", "),
			),
			t: types.Typ[types.String],
		},
	)
}
