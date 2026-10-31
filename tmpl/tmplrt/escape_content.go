// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Ported from html/template/content.go in Go 1.26.10 for Rastrillo's compiled
// templates; see LICENSE-go. Behaviour is unchanged.
// Only what the escapers need is kept.

//rastrillo:vendored go1.26.10 src/html/template/content.go

package tmplrt

import (
	"fmt"
	"html/template"
	"reflect"

	"amadan.net/rastrillo/rastrillo/internal/htmlctx"
)

// filterFailsafe is an innocuous word that is emitted in place of unsafe values
// by sanitizer functions. It is not a keyword in any programming language,
// contains no special characters, is not empty, and when it appears in output
// it is distinct enough that a developer can find the source of the problem
// via a search engine. (From escape.go.)
const filterFailsafe = "ZgotmplZ"

// indirect returns the value, after dereferencing as many times
// as necessary to reach the base type (or nil).
func indirect(a any) any {
	if a == nil {
		return nil
	}
	if t := reflect.TypeOf(a); t.Kind() != reflect.Pointer {
		// Avoid creating a reflect.Value if it's not a pointer.
		return a
	}
	v := reflect.ValueOf(a)
	for v.Kind() == reflect.Pointer && !v.IsNil() {
		v = v.Elem()
	}
	return v.Interface()
}

var (
	errorType       = reflect.TypeFor[error]()
	fmtStringerType = reflect.TypeFor[fmt.Stringer]()
)

// indirectToStringerOrError returns the value, after dereferencing as many times
// as necessary to reach the base type (or nil) or an implementation of fmt.Stringer
// or error.
func indirectToStringerOrError(a any) any {
	if a == nil {
		return nil
	}
	v := reflect.ValueOf(a)
	for !v.Type().Implements(fmtStringerType) && !v.Type().Implements(errorType) && v.Kind() == reflect.Pointer && !v.IsNil() {
		v = v.Elem()
	}
	return v.Interface()
}

// stringify converts its arguments to a string and the type of the content.
// All pointers are dereferenced, as in the text/template package.
func stringify(args ...any) (string, htmlctx.ContentType) {
	if len(args) == 1 {
		switch s := indirect(args[0]).(type) {
		case string:
			return s, htmlctx.ContentTypePlain
		case template.CSS:
			return string(s), htmlctx.ContentTypeCSS
		case template.HTML:
			return string(s), htmlctx.ContentTypeHTML
		case template.HTMLAttr:
			return string(s), htmlctx.ContentTypeHTMLAttr
		case template.JS:
			return string(s), htmlctx.ContentTypeJS
		case template.JSStr:
			return string(s), htmlctx.ContentTypeJSStr
		case template.URL:
			return string(s), htmlctx.ContentTypeURL
		case template.Srcset:
			return string(s), htmlctx.ContentTypeSrcset
		}
	}
	i := 0
	for _, arg := range args {
		// We skip untyped nil arguments for backward compatibility.
		// Without this they would be output as <nil>, escaped.
		// See issue 25875.
		if arg == nil {
			continue
		}

		args[i] = indirectToStringerOrError(arg)
		i++
	}
	return fmt.Sprint(args[:i]...), htmlctx.ContentTypePlain
}
