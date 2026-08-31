package keyring

import "amadan.net/rastrillo/rastrillo/crypto"

// WrapSeed seals seed under the key derived from a credential's PRF
// output: crypto.SealSym(r.WrapKey(prf), seed) → iv(12) ‖ AES-256-GCM
// ciphertext. Wrapping the same seed under a different credential's
// PRF output is the whole of device add — and of the RPID move below.
// The wrap goes to the server, stored per credential ID (the package
// doc's transport contract); the seed itself never travels.
//
// The RPID-move drill. Passkeys are scoped to an RP ID, so renaming a
// deployment would strand every wrapped seed if done as a cutover.
// webauthn.Config.LegacyRPID exists for the middle phase; the drill
// (kass's make rpid-move) is three:
//
//  1. Old name — normal operation, the seed wrapped under old-RPID
//     passkeys.
//
//  2. Crossover — serve under the new RPID with LegacyRPID set to the
//     old one (assertions only: legacy credentials may sign in, never
//     enrol). A fresh device signs in via the legacy fallback, unwraps
//     the seed with UnwrapSeed, and enrols a new-RPID credential —
//     WrapSeed with the new credential's PRF output, same seed.
//
//  3. Settled — LegacyRPID removed; only new-name credentials remain.
//
// WrapSeed is sufficient mechanism for all three phases. The
// executable drill — a browser walking them — is a harness scenario
// and lands with #80; this package commits the API and the doc.
func (r Ring) WrapSeed(prf, seed []byte) ([]byte, error) {
	return crypto.SealSym(r.WrapKey(prf), seed)
}

// UnwrapSeed reverses WrapSeed. It fails the way crypto.OpenSym fails:
// a wrong credential's PRF output, a wrong namespace, and a tampered
// blob are indistinguishable by design.
func (r Ring) UnwrapSeed(prf, wrapped []byte) ([]byte, error) {
	return crypto.OpenSym(r.WrapKey(prf), wrapped)
}
