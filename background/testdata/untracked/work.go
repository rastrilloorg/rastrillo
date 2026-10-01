package untracked

import (
	"time"
	tm "time"
)

func start() {
	go func() {}()
	time.AfterFunc(time.Second, func() {})
	// Syntax only: a renamed import is not seen. Documented, not fixed.
	tm.AfterFunc(time.Second, func() {})
}
