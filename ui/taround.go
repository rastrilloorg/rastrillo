package ui

import (
	"fmt"
	"strings"
)

// translator is the catalog a locale call reads: its localizer's, or,
// for a legacy call or a nil localizer, the one Funcs was given.
func (options config) translator(
	call localeCall,
) func(string, ...any) string {
	if call.l != nil {
		return call.l.t
	}
	return options.t
}

// around is a catalog string cut at one placeholder: the words before
// it and the words after it.
type around struct{ Before, After string }

// taroundFunc is {{Taround $L "key" "name"}}: the catalog string for
// key, its placeholders unfilled, cut at {name}. A template puts what
// it renders itself where the value goes, a block's output, which no
// template can capture to hand Tf:
//
//	aria-label="{{$up.Before}}{{template "upLabel" .}}{{$up.After}}"
//
// so the words around it keep each language's own order ("Back to
// {name}", "{name}に戻る"). A string without {name} is all Before.
func taroundFunc(
	translator func(localeCall) func(string, ...any) string,
) func(...any) (any, error) {
	return func(args ...any) (any, error) {
		call := newCall("Taround", args)
		key, err := call.key()
		if err != nil {
			return nil, err
		}
		if err := call.arity(2, false); err != nil {
			return nil, err
		}
		name, ok := call.rest[1].(string)
		if !ok {
			return nil, fmt.Errorf(argTypeMsg,
				"Taround", 2+call.offset, call.rest[1], "string")
		}
		sentence := translator(call)(key)
		before, after, _ := strings.Cut(sentence, "{"+name+"}")
		return around{Before: before, After: after}, nil
	}
}
