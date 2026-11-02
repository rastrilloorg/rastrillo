package op

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	rastrillo "amadan.net/rastrillo/rastrillo"
)

// FenceVerbs are the verbs that undo or send something, so a write
// whose ID's last part starts with one must be marked Destructive. It
// is exported for the generator's test.
var FenceVerbs = []string{
	"cancel", "refund", "revoke", "reject", "remove", "withdraw",
	"discard", "disconnect", "rotate", "regenerate", "replace", "delete",
	"send", "resend", "purge",
}

// CheckFlags refuses a read marked Destructive, and a write whose ID's
// last part starts with a fence verb but isn't Destructive. A hand-
// written descriptor can't say "safe" for a verb that undoes things by
// forgetting the flag, and a read can't change anything to warn about.
func CheckFlags(descriptor Descriptor) error {
	if descriptor.Access == rastrillo.Read {
		if descriptor.Destructive {
			return fmt.Errorf("op: %s is a read and cannot be Destructive",
				descriptor.ID)
		}
		return nil
	}
	if descriptor.Destructive {
		return nil
	}
	name := descriptor.ID[strings.LastIndex(descriptor.ID, ".")+1:]
	for _, verb := range FenceVerbs {
		if strings.HasPrefix(name, verb) {
			return fmt.Errorf(
				"op: %s starts with %q, so it must be Destructive",
				descriptor.ID, verb)
		}
	}
	return nil
}

// ResponseGuard checks, without side effects, that what an answer
// discloses is still the actor's to see, for example that each note in
// a search result is still in a team the actor belongs to. It must
// never run the operation again or change state; descriptor is nil for
// the tool list.
//
// Put a slow check against another service before the app's own
// revision and link checks: the guard runs last, and the last check
// should be the closest to the bytes going out.
type ResponseGuard func(
	ctx context.Context,
	actor rastrillo.Actor,
	descriptor *Descriptor,
	input json.RawMessage,
	result any,
) error
