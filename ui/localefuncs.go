package ui

import (
	"errors"
	"fmt"
	"html/template"
	"reflect"

	"amadan.net/rastrillo/rastrillo/currency"
	"amadan.net/rastrillo/rastrillo/locales"
)

// LocaleFuncs are the template functions whose output depends on the
// locale, and so take the request's localizer first.
var LocaleFuncs = []string{"T", "Tf", "Tbdi", "number", "displayValue", "dateWords", "money"}

// Appendix C.2 of the multi-currency money spec, verbatim: op.funcs.arity,
// op.funcs.arg_type, op.money.first_arg and op.money.arity.
const (
	arityMsg         = "%s: %d arguments, want %d"
	argTypeMsg       = "%s: argument %d is %T, want %s"
	moneyArityMsg    = "money: want $L, an amount and a currency; got %d arguments after $L"
	moneyFirstArgMsg = "money: the first argument must be $L (money has no legacy form)"
)

var errMoneyFirstArg = errors.New(moneyFirstArgMsg)

// pick decides a locale call's form by its first argument's type,
// never its value: a *Localizer, nil or not, is the localizer form, so
// a nil value passed to a legacy {{number .Value}} can never be taken
// for a missing localizer. A nil localizer here means the data reached
// the template without "L"; ui.Funcs answers with its own options
// (today's behaviour), and ui.Pages replaces these entries with strict
// ones.
func pick(args []any) (l *Localizer, localizerForm bool, rest []any) {
	if len(args) > 0 && reflect.TypeOf(args[0]) == localizerType {
		l, _ = args[0].(*Localizer)
		return l, true, args[1:]
	}
	return nil, false, args
}

// localeCall is one locale call after pick: the localizer (nil for the
// legacy form or a missing L) and the arguments after it. offset is 1
// in the localizer form, so an error counts arguments as the template
// author wrote them, $L included.
type localeCall struct {
	name   string
	l      *Localizer
	rest   []any
	offset int
}

func newCall(name string, args []any) localeCall {
	l, lf, rest := pick(args)
	c := localeCall{name: name, l: l, rest: rest}
	if lf {
		c.offset = 1
	}
	return c
}

// arity checks the argument count. html/template checked it against the
// old fixed signatures; a variadic any checks nothing, so without this
// {{number}} would render nothing instead of failing.
func (c localeCall) arity(want int, atLeast bool) error {
	if n := len(c.rest); n == want || atLeast && n > want {
		return nil
	}
	return fmt.Errorf(arityMsg, c.name, len(c.rest)+c.offset, want+c.offset)
}

// key is the catalog key every T-shaped call starts with, checked for
// the same reason as arity: {{T 5}} must fail, not look up "5".
func (c localeCall) key() (string, error) {
	if err := c.arity(1, true); err != nil {
		return "", err
	}
	k, ok := c.rest[0].(string)
	if !ok {
		return "", fmt.Errorf(argTypeMsg, c.name, 1+c.offset, c.rest[0], "string")
	}
	return k, nil
}

// localeFuncs is the dispatching half of Funcs: each entry uses the
// localizer when the call passed a non-nil one, and Funcs' own options
// otherwise, so a legacy template, and a migrated one reached by an app
// that still renders through ui.Funcs, render exactly as before.
func localeFuncs(o config) template.FuncMap {
	t := func(c localeCall) func(string, ...any) string {
		if c.l != nil {
			return c.l.t
		}
		return o.t
	}
	format := func(c localeCall) string {
		if c.l != nil {
			return c.l.format
		}
		return o.locale
	}
	number := func(name string, integerStrings bool) func(...any) (any, error) {
		return func(args ...any) (any, error) {
			c := newCall(name, args)
			if err := c.arity(1, false); err != nil {
				return nil, err
			}
			return formatNumber(c.rest[0], format(c), integerStrings), nil
		}
	}
	return template.FuncMap{
		"T": func(args ...any) (any, error) {
			c := newCall("T", args)
			k, err := c.key()
			if err != nil {
				return nil, err
			}
			return t(c)(k, c.rest[1:]...), nil
		},
		"Tf": func(args ...any) (any, error) {
			c := newCall("Tf", args)
			k, err := c.key()
			if err != nil {
				return nil, err
			}
			// The legacy form keeps its own entry, o.tf, rather than o.t:
			// without WithT the two differ, and only tf interpolates.
			tf := o.tf
			if c.l != nil {
				tf = c.l.t
			}
			return tf(k, c.rest[1:]...), nil
		},
		"Tbdi": func(args ...any) (any, error) {
			c := newCall("Tbdi", args)
			k, err := c.key()
			if err != nil {
				return nil, err
			}
			return tbdi(t(c))(k, c.rest[1:]...), nil
		},
		"number":       number("number", true),
		"displayValue": number("displayValue", false),
		"dateWords": func(args ...any) (any, error) {
			c := newCall("dateWords", args)
			if err := c.arity(0, false); err != nil {
				return nil, err
			}
			return dateWords(t(c))(), nil
		},
		"money": func(args ...any) (any, error) {
			c := newCall("money", args)
			if c.offset == 0 {
				return nil, errMoneyFirstArg
			}
			if len(c.rest) != 2 {
				return nil, fmt.Errorf(moneyArityMsg, len(c.rest))
			}
			minor, present, err := currency.AmountFromAny(c.rest[0])
			if err != nil {
				return nil, err
			}
			// A nil pointer is an absent amount, and an absent amount has
			// no currency either, so the code is not examined: it is
			// typically nil or empty beside it.
			if !present {
				return "", nil
			}
			code, ok := c.rest[1].(string)
			if !ok {
				return nil, fmt.Errorf(argTypeMsg, "money", 3, c.rest[1], "string")
			}
			loc := format(c)
			if loc == "" {
				// No localizer and no WithLocale: the app's installed
				// default, never a hardcoded English.
				loc = locales.FormatFor("")
			}
			return currency.Format(loc, minor, code)
		},
	}
}
