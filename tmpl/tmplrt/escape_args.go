// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Ported from html/template/escape.go in Go 1.26.10 for Rastrillo's compiled
// templates; see LICENSE-go. Behaviour is unchanged.

//rastrillo:vendored go1.26.10 src/html/template/escape.go

package tmplrt

import "fmt"

// EvalArgs is html/template's _eval_args_. Escaping rewrites a predefined
// escaper called with arguments, {{html .X}}, to {{_eval_args_ .X | html}}
// so that html can merge with the escapers it adds, and this formats the
// arguments the way html would have.
//
// EvalArgs formats the list of arguments into a string. It is equivalent to
// fmt.Sprint(args...), except that it dereferences all pointers.
func EvalArgs(args ...any) string {
	// Optimization for simple common case of a single string argument.
	if len(args) == 1 {
		if s, ok := args[0].(string); ok {
			return s
		}
	}
	for i, arg := range args {
		args[i] = indirectToStringerOrError(arg)
	}
	return fmt.Sprint(args...)
}
