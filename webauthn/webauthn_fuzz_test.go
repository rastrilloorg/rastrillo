package webauthn

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"testing"

	"amadan.net/rastrillo/rastrillo/webauthn/authtest"
)

// fuzzChallenge is fixed, as is fuzzAuthenticator's key, so a ceremony
// saved in the corpus still verifies on the next run.
var fuzzChallenge = bytes.Repeat([]byte{7}, 32)

func fuzzAuthenticator(f *testing.F) *authtest.Authenticator {
	f.Helper()
	key, err := ecdsa.ParseRawPrivateKey(elliptic.P256(),
		bytes.Repeat([]byte{9}, 32))
	if err != nil {
		f.Fatal(err)
	}
	return &authtest.Authenticator{Key: key, CredID: []byte("fuzz")}
}

// FuzzCBOR feeds the hand-rolled reader raw bytes. Beyond not
// panicking, an integer must keep its sign: CBOR's major type 0 is never
// negative and type 1 never is anything else, so a key whose COSE
// fields read back with the other sign was not the key that was sent.
func FuzzCBOR(f *testing.F) {
	key := fuzzAuthenticator(f).COSEKey()
	f.Add(key)
	f.Add(authtest.Map(authtest.Text("authData"), authtest.Bytes(key)))
	f.Add(authtest.Head(majArray, 1<<32-1))
	f.Add(authtest.Head(majMap, 1<<32-1))
	f.Add([]byte{0x9f})
	f.Add(bytes.Repeat([]byte{0x81}, 4096))
	f.Fuzz(func(t *testing.T, encoded []byte) {
		reader := &cborReader{buf: encoded}
		value, err := reader.value()
		if err != nil {
			return
		}
		number, isInt := value.(int64)
		switch major := encoded[0] >> 5; {
		case major == majUint && (!isInt || number < 0):
			t.Fatalf("unsigned %x read as %v", encoded, value)
		case major == majNegInt && (!isInt || number >= 0):
			t.Fatalf("negative %x read as %v", encoded, value)
		}
	})
}

// FuzzRegister mutates a whole creation ceremony. Whatever it accepts
// must hand back a key that is a point on P-256 and a credential id,
// since both are stored and trusted from then on.
func FuzzRegister(f *testing.F) {
	authenticator := fuzzAuthenticator(f)
	options := authtest.Options{RPID: cfg.RPID, Origin: cfg.Origin}
	clientData, attestation := authenticator.Create(fuzzChallenge, options)
	_, err := cfg.Register(fuzzChallenge, clientData, attestation)
	if err != nil {
		f.Fatalf("the seed ceremony: %v", err)
	}
	f.Add(clientData, attestation)
	for _, change := range []func(*authtest.Options){
		func(bad *authtest.Options) { bad.RPID = "evil.example" },
		func(bad *authtest.Options) { bad.NoCredential = true },
	} {
		bad := options
		change(&bad)
		badClient, badAttestation := authenticator.Create(fuzzChallenge, bad)
		f.Add(badClient, badAttestation)
	}
	f.Add(clientData, []byte{0xa1, 0x68})
	f.Add([]byte(`{`), attestation)
	f.Fuzz(func(t *testing.T, clientData, attestation []byte) {
		cred, err := cfg.Register(fuzzChallenge, clientData, attestation)
		if err != nil {
			return
		}
		if len(cred.ID) == 0 || len(cred.AAGUID) != 16 {
			t.Fatalf("registered id %x, aaguid %x", cred.ID, cred.AAGUID)
		}
		if _, err := publicKey(cred.PublicKey); err != nil {
			t.Fatalf("registered key %x: %v", cred.PublicKey, err)
		}
	})
}

// FuzzAssert mutates a signed assertion. The authenticator signed one
// pair of client and authenticator data, so that pair is the only one
// that may pass; any other that verifies is a forgery.
func FuzzAssert(f *testing.F) {
	authenticator := fuzzAuthenticator(f)
	cred := Credential{PublicKey: append([]byte{4}, append(
		authenticator.Key.X.FillBytes(make([]byte, 32)),
		authenticator.Key.Y.FillBytes(make([]byte, 32))...)...)}
	options := authtest.Options{RPID: cfg.RPID, Origin: cfg.Origin}
	clientData, authData, signature, err := authenticator.Get(
		fuzzChallenge, options)
	if err != nil {
		f.Fatal(err)
	}
	// A seed that stopped verifying would leave the property below
	// with nothing to hold.
	_, err = cfg.Assert(cred, fuzzChallenge, clientData, authData, signature)
	if err != nil {
		f.Fatalf("the signed assertion: %v", err)
	}
	f.Add(clientData, authData, signature)
	f.Add(clientData, authData[:36], signature)
	f.Add(clientData, authData, signature[:len(signature)-1])
	f.Add([]byte(`null`), authData, signature)
	f.Fuzz(func(t *testing.T, gotClient, gotAuth, gotSignature []byte) {
		_, err := cfg.Assert(cred, fuzzChallenge, gotClient, gotAuth,
			gotSignature)
		if err != nil {
			return
		}
		if !bytes.Equal(gotClient, clientData) ||
			!bytes.Equal(gotAuth, authData) {
			t.Fatalf("assertion over %q and %x verified; only %q and "+
				"%x were signed", gotClient, gotAuth, clientData, authData)
		}
	})
}

// A reader that recursed as deep as the bytes go would let one
// registration hold hundreds of megabytes of stack, which FuzzCBOR's
// property does not see.
func TestCBORNestingIsBounded(t *testing.T) {
	nested := append(bytes.Repeat([]byte{0x81}, maxDepth), 0)
	if _, err := (&cborReader{buf: nested}).value(); err != nil {
		t.Fatalf("%d deep: %v", maxDepth, err)
	}
	deeper := append([]byte{0x81}, nested...)
	if _, err := (&cborReader{buf: deeper}).value(); err == nil {
		t.Fatalf("%d deep was read", maxDepth+1)
	}
}
