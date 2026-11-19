//go:build goexperiment.jsonv2 && !go1.27

package op

import jsonv2 "encoding/json/v2"

// errPassOn, for Go 1.26 built with GOEXPERIMENT=jsonv2. There
// go-json-experiment is an alias of Go 1.26's encoding/json/v2, which
// lets a value through only on its own SkipFunc, compared with ==. Any
// other error, errors.ErrUnsupported included, would refuse every value
// that is not null.
var errPassOn = jsonv2.SkipFunc
