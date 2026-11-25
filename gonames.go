package rastrillo

import (
	"fmt"

	"amadan.net/rastrillo/rastrillo/internal/opsnames"
)

const msgStoreNameTaken = "name: %q would be the Go name %q, which " +
	"every generated store package already declares; rename the resource"

// checkGoNames refuses a resource whose generated Go names would be
// declared twice: its type beside gen/ops' own (Page, Update,
// ListInput...), or its sqlc model beside gen/store's Declaration.
// Refused where the manifest is read, rather than left to the compiler
// to explain why the package does not build.
func checkGoNames(name string) error {
	if taken := opsnames.Taken(name); taken != "" {
		return fmt.Errorf(msgOpsNameTaken, name, taken)
	}
	if taken := opsnames.StoreTaken(name); taken != "" {
		return fmt.Errorf(msgStoreNameTaken, name, taken)
	}
	return nil
}
