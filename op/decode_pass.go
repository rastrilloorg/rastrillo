//go:build !goexperiment.jsonv2 || go1.27

package op

import "errors"

// errPassOn is what refuseNull returns for a value it lets through, so
// that json decodes it as if refuseNull were not there. json v2 asks for
// errors.ErrUnsupported: go-json-experiment's own code does, and so does
// Go 1.27's encoding/json/v2, which go-json-experiment becomes an alias
// of under GOEXPERIMENT=jsonv2 (on by default from Go 1.27).
var errPassOn = errors.ErrUnsupported
