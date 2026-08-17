package webauthn

import (
	"crypto/rand"
	"errors"
	"testing"

	"github.com/carlosframework/rastrillo/webauthn/authtest"
)

var cfg = Config{RPID: "kass.training", Origin: "https://dev.kass.training"}

func good() authtest.Options {
	return authtest.Options{RPID: cfg.RPID, Origin: cfg.Origin}
}

func auth(t *testing.T) *authtest.Authenticator {
	t.Helper()
	a, err := authtest.New()
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func chal(t *testing.T) []byte {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func register(t *testing.T, a *authtest.Authenticator, c []byte, o authtest.Options) (Credential, error) {
	t.Helper()
	clientData, attestation := a.Create(c, o)
	return cfg.Register(c, clientData, attestation)
}

func assert(t *testing.T, a *authtest.Authenticator, cred Credential, c []byte, o authtest.Options) (uint32, error) {
	t.Helper()
	clientData, authData, sig, err := a.Get(c, o)
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Verify(cred, c, clientData, authData, sig)
}

func TestRegisterThenAssert(t *testing.T) {
	a := auth(t)
	cred, err := register(t, a, chal(t), good())
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if string(cred.ID) != string(a.CredID) {
		t.Error("credential id did not survive registration")
	}
	if len(cred.PublicKey) != 65 || cred.PublicKey[0] != 4 {
		t.Errorf("public key is %d bytes starting %#x, want 65 starting 0x04", len(cred.PublicKey), cred.PublicKey[0])
	}
	count, err := assert(t, a, cred, chal(t), good())
	if err != nil {
		t.Fatalf("assert: %v", err)
	}
	if count == 0 {
		t.Error("signature counter did not advance")
	}
}

func TestRejectsATamperedCeremony(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate func(*authtest.Options)
		want   error
	}{
		"a challenge that was never issued": {
			func(o *authtest.Options) { o.SignChallenge = []byte("some other challenge") }, ErrChallenge},
		"another origin": {
			func(o *authtest.Options) { o.Origin = "https://kass.training.evil.example" }, ErrOrigin},
		"another relying party": {
			func(o *authtest.Options) { o.RPID = "evil.example" }, ErrRPID},
		"a user who was never verified": {
			func(o *authtest.Options) { o.Flags = authtest.FlagUserPresent }, ErrNotVerified},
		"nobody present at all": {
			func(o *authtest.Options) { o.Flags = authtest.FlagUserVerified }, ErrNotVerified},
		"a corrupted signature": {
			func(o *authtest.Options) { o.CorruptSig = true }, ErrSignature},
	} {
		t.Run(name, func(t *testing.T) {
			a := auth(t)
			cred, err := register(t, a, chal(t), good())
			if err != nil {
				t.Fatal(err)
			}
			opts := good()
			tc.mutate(&opts)
			if _, err := assert(t, a, cred, chal(t), opts); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestRegistrationRequiresAVerifiedUser(t *testing.T) {
	o := good()
	o.Flags = authtest.FlagUserPresent | authtest.FlagAttestedData
	if _, err := register(t, auth(t), chal(t), o); !errors.Is(err, ErrNotVerified) {
		t.Errorf("err = %v, want ErrNotVerified", err)
	}
}

func TestRegistrationWithoutACredentialIsRefused(t *testing.T) {
	o := good()
	o.NoCredential = true
	if _, err := register(t, auth(t), chal(t), o); err == nil {
		t.Error("a registration carrying no credential was accepted")
	}
}

func TestRejectsAnotherCredentialsKey(t *testing.T) {
	a, b := auth(t), auth(t)
	credA, err := register(t, a, chal(t), good())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := assert(t, b, credA, chal(t), good()); !errors.Is(err, ErrSignature) {
		t.Errorf("err = %v, want ErrSignature", err)
	}
}

// A counter going backwards is the one signal WebAuthn gives that a credential
// has been cloned.
func TestRejectsACounterGoingBackwards(t *testing.T) {
	a := auth(t)
	cred, err := register(t, a, chal(t), good())
	if err != nil {
		t.Fatal(err)
	}
	count, err := assert(t, a, cred, chal(t), good())
	if err != nil {
		t.Fatal(err)
	}
	cred.SignCount = count
	stale := count - 1
	o := good()
	o.Count = &stale
	if _, err := assert(t, a, cred, chal(t), o); !errors.Is(err, ErrCounter) {
		t.Errorf("err = %v, want ErrCounter", err)
	}
}

// Most platform authenticators report zero forever. That is not evidence of
// cloning and must never lock anyone out.
func TestAllowsAnAuthenticatorThatNeverCounts(t *testing.T) {
	a := auth(t)
	cred, err := register(t, a, chal(t), good())
	if err != nil {
		t.Fatal(err)
	}
	zero := uint32(0)
	o := good()
	o.Count = &zero
	for i := 0; i < 3; i++ {
		if _, err := assert(t, a, cred, chal(t), o); err != nil {
			t.Fatalf("assertion %d: %v", i, err)
		}
	}
}

func TestRejectsKeysOutsideTheSubset(t *testing.T) {
	x, y := make([]byte, 32), make([]byte, 32)
	for name, key := range map[string][]byte{
		"RSA": authtest.Map(authtest.Int(1), authtest.Int(3), authtest.Int(3), authtest.Int(-257)),
		"Ed25519": authtest.Map(authtest.Int(1), authtest.Int(1), authtest.Int(3), authtest.Int(-8),
			authtest.Int(-1), authtest.Int(6), authtest.Int(-2), authtest.Bytes(x)),
		"P-384": authtest.Map(authtest.Int(1), authtest.Int(2), authtest.Int(3), authtest.Int(authtest.AlgES256),
			authtest.Int(-1), authtest.Int(2), authtest.Int(-2), authtest.Bytes(x), authtest.Int(-3), authtest.Bytes(y)),
		"short coordinates": authtest.Map(authtest.Int(1), authtest.Int(2), authtest.Int(3), authtest.Int(authtest.AlgES256),
			authtest.Int(-1), authtest.Int(1), authtest.Int(-2), authtest.Bytes(x[:16]), authtest.Int(-3), authtest.Bytes(y[:16])),
		"a point that is not on the curve": authtest.Map(authtest.Int(1), authtest.Int(2), authtest.Int(3), authtest.Int(authtest.AlgES256),
			authtest.Int(-1), authtest.Int(1), authtest.Int(-2), authtest.Bytes(x), authtest.Int(-3), authtest.Bytes(y)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseCOSEKey(key); err == nil {
				t.Error("expected an error, got none")
			}
		})
	}
}

func TestRejectsRubbish(t *testing.T) {
	a := auth(t)
	c := chal(t)
	clientData, _ := a.Create(c, good())
	full := a.AuthData(cfg.RPID, authtest.FlagUserPresent|authtest.FlagUserVerified|authtest.FlagAttestedData, true)

	for name, att := range map[string][]byte{
		"empty":                 {},
		"not a map":             authtest.Text("hello"),
		"no authenticator data": authtest.Map(authtest.Text("fmt"), authtest.Text("none")),
		"truncated":             authtest.Map(authtest.Text("authData"), authtest.Bytes(full[:20])),
		"a length that runs off the end": authtest.Map(authtest.Text("authData"),
			append(authtest.Head(2, 4096), 1, 2, 3)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := cfg.Register(c, clientData, att); err == nil {
				t.Error("expected an error, got none")
			}
		})
	}
}

// A creation ceremony's client data replayed into an assertion.
func TestRejectsTheWrongCeremony(t *testing.T) {
	a := auth(t)
	c := chal(t)
	cred, err := register(t, a, c, good())
	if err != nil {
		t.Fatal(err)
	}
	clientData := authtest.ClientData("webauthn.create", cfg.Origin, c)
	authData := a.AuthData(cfg.RPID, authtest.FlagUserPresent|authtest.FlagUserVerified, false)
	if _, err := cfg.Verify(cred, c, clientData, authData, nil); err == nil {
		t.Error("expected an error, got none")
	}
}

// --- moving an instance to a new hostname ---
//
// A passkey is bound to its relying party, and the seed that decrypts a
// training log is wrapped under that passkey's PRF output. So a hostname change
// is not an inconvenience that costs somebody a login — done wrong it makes
// their log permanently unreadable, because the server holds only bytes it
// cannot open. These are the tests that say the door stays open long enough.

// moving is the instance mid-move: it is now home.kass.training, and it used to
// be kass.training.
var moving = Config{RPID: "home.kass.training", Origin: "https://home.kass.training", LegacyRPID: "kass.training"}

func TestAPasskeyFromTheOldNameStillSignsIn(t *testing.T) {
	a := auth(t)
	// Registered back when the instance was kass.training.
	before := Config{RPID: "kass.training", Origin: "https://home.kass.training"}
	c := chal(t)
	clientData, attestation := a.Create(c, authtest.Options{RPID: before.RPID, Origin: before.Origin})
	cred, err := before.Register(c, clientData, attestation)
	if err != nil {
		t.Fatalf("register under the old name: %v", err)
	}

	// And now asserted against the instance under its new name.
	c = chal(t)
	clientData, authData, sig, err := a.Get(c, authtest.Options{RPID: "kass.training", Origin: moving.Origin})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := moving.Verify(cred, c, clientData, authData, sig); err != nil {
		t.Fatalf("the old passkey was refused, which strands the seed it wraps: %v", err)
	}
}

func TestTheNewNameWorksAtTheSameTime(t *testing.T) {
	a := auth(t)
	c := chal(t)
	clientData, attestation := a.Create(c, authtest.Options{RPID: moving.RPID, Origin: moving.Origin})
	cred, err := moving.Register(c, clientData, attestation)
	if err != nil {
		t.Fatalf("register under the new name: %v", err)
	}

	c = chal(t)
	clientData, authData, sig, err := a.Get(c, authtest.Options{RPID: moving.RPID, Origin: moving.Origin})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := moving.Verify(cred, c, clientData, authData, sig); err != nil {
		t.Fatalf("a passkey minted under the new name was refused: %v", err)
	}
}

// The crossover has to drain rather than fill back up: every new credential is
// minted under the name we are moving to, so there is a day when the old one
// can be switched off.
func TestANewPasskeyCannotBeMintedUnderTheOldName(t *testing.T) {
	a := auth(t)
	c := chal(t)
	clientData, attestation := a.Create(c, authtest.Options{RPID: "kass.training", Origin: moving.Origin})
	if _, err := moving.Register(c, clientData, attestation); !errors.Is(err, ErrRPID) {
		t.Fatalf("registration under the old name was allowed: %v", err)
	}
}

// Without a move in progress, another relying party is just another relying
// party. The fallback must not be a permanent hole.
func TestSomeoneElsesRelyingPartyIsStillRefused(t *testing.T) {
	a := auth(t)
	settled := Config{RPID: "home.kass.training", Origin: "https://home.kass.training"}

	c := chal(t)
	clientData, attestation := a.Create(c, authtest.Options{RPID: settled.RPID, Origin: settled.Origin})
	cred, err := settled.Register(c, clientData, attestation)
	if err != nil {
		t.Fatal(err)
	}

	c = chal(t)
	clientData, authData, sig, err := a.Get(c, authtest.Options{RPID: "kass.training", Origin: settled.Origin})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := settled.Verify(cred, c, clientData, authData, sig); !errors.Is(err, ErrRPID) {
		t.Fatalf("an assertion for a different relying party was accepted: %v", err)
	}
	// And the same instance mid-move accepts exactly that one, and no other.
	c = chal(t)
	clientData, authData, sig, _ = a.Get(c, authtest.Options{RPID: "kass.example", Origin: moving.Origin})
	if _, err := moving.Verify(cred, c, clientData, authData, sig); !errors.Is(err, ErrRPID) {
		t.Fatalf("a relying party that was never ours was accepted: %v", err)
	}
}
