package carlos

import (
	"errors"
	"os"
)

// instanceKeyEnv is the key the platform mints for an instance before
// its first start and backs up before it runs. It is written create-only
// into the instance's overlay env file, beside $CARLOS_ADMIN_TOKEN.
const instanceKeyEnv = "CARLOS_INSTANCE_KEY"

// ErrNoInstanceKey means the process is on CARLOS, the app passed no
// key of its own, and the platform delivered none. Refuse to start: a
// key minted now would seal rows the database replicator uploads while
// the key itself waits for the next files backup, and an instance
// relocated in that window keeps its data and cannot unseal it.
var ErrNoInstanceKey = errors.New("rastrillo/carlos: on CARLOS with no instance key: pass the app's own or have the platform set $CARLOS_INSTANCE_KEY; never mint one here")

// InstanceKey returns the key an app seals with: what it hands to
// auth.Config.InstanceKey, pow.Config.InstanceKey and the like.
//
// own is the key the app already has — its explicit setting, or a key
// file it finds on disk — and "" when it has none. Load it before the
// call and without minting. An app that only consults its key file in
// the fallback would hand over "" on CARLOS and be switched to the
// platform's key, orphaning everything sealed under the file's.
//
//	own, err := loadKeyWithoutMinting(dataDir) // setting, else key file, else ""
//	if err != nil {
//		return err
//	}
//	key, err := carlos.InstanceKey(own)
//	if errors.Is(err, carlos.ErrNotOnCarlos) {
//		key, err = mintKey(dataDir) // off CARLOS: what the app did before
//	}
//	if err != nil {
//		return err // ErrNoInstanceKey: do not start
//	}
//
// In order:
//
//   - own, when set. An app that has a key keeps it even where the
//     platform offers another, because everything already sealed under
//     it would be unreadable after a switch.
//   - $CARLOS_INSTANCE_KEY, when set, verbatim. It is opaque; any
//     non-empty string derives.
//   - On CARLOS ([Running]), [ErrNoInstanceKey].
//   - Otherwise [ErrNotOnCarlos]: the app is on a laptop, in a test or
//     on a box of its own, and minting or a development key is its call.
func InstanceKey(own string) (string, error) {
	if own != "" {
		return own, nil
	}
	if key := os.Getenv(instanceKeyEnv); key != "" {
		return key, nil
	}
	if Running() {
		return "", ErrNoInstanceKey
	}
	return "", ErrNotOnCarlos
}
