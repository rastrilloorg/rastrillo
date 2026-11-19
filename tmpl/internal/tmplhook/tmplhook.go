// Package tmplhook tells a test which templates ran compiled code. tmpl
// calls Executed for every Execute and ExecuteTemplate; the equivalence
// corpus, which renders sets ui.Pages builds and does not hand out,
// listens to know its renders were compiled ones. tmplrt calls
// TypesChanged when it finds compiled code written for other types.
// Being internal to tmpl, it is nothing an app can reach.
package tmplhook

import "sync/atomic"

var executed atomic.Pointer[func(entry string, compiled bool)]

// Executed reports that entry ran, on compiled code or not. With no
// listener it does nothing.
func Executed(entry string, compiled bool) {
	if fn := executed.Load(); fn != nil {
		(*fn)(entry, compiled)
	}
}

// Listen makes fn the listener until the returned function is called.
func Listen(fn func(entry string, compiled bool)) (stop func()) {
	executed.Store(&fn)
	return func() { executed.Store(nil) }
}

var typesChanged atomic.Pointer[func(err error)]

// TypesChanged reports that compiled code was bound for types that are
// no longer the program's, which tmplrt.Holds found. The corpus listens
// to know that every typed variant it compiled is in use.
func TypesChanged(err error) {
	if fn := typesChanged.Load(); fn != nil {
		(*fn)(err)
	}
}

// ListenTypes makes fn the listener for TypesChanged until the returned
// function is called.
func ListenTypes(fn func(err error)) (stop func()) {
	typesChanged.Store(&fn)
	return func() { typesChanged.Store(nil) }
}
