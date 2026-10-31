// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Forked from html/template/content.go in Go 1.26.10 for Rastrillo's compiled
// templates; see LICENSE-go. Names are exported for tmplrt and
// internal/tmplescape (doc.go); behaviour is unchanged.

//rastrillo:vendored go1.26.10 src/html/template/content.go

package htmlctx

// Only contentType is kept: attrType answers in it, and tmplrt's
// escapers read it off their arguments.

type ContentType uint8

const (
	ContentTypePlain ContentType = iota
	ContentTypeCSS
	ContentTypeHTML
	ContentTypeHTMLAttr
	ContentTypeJS
	ContentTypeJSStr
	ContentTypeURL
	ContentTypeSrcset
	// contentTypeUnsafe is used in attr.go for values that affect how
	// embedded content and network messages are formed, vetted,
	// or interpreted; or which credentials network messages carry.
	ContentTypeUnsafe
)
