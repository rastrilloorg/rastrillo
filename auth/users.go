package auth

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/objrec"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// User is a person on a centre that keeps no database: the record at
// users/<uid>.json. ID is the session subject. Email is empty in blind
// mode, where EH is all the store knows of the address. Epoch is what
// every session cookie for this user carries; EndSessions moves it.
type User struct {
	ID      string    `json:"uid"`
	Email   string    `json:"email,omitempty"`
	EH      string    `json:"eh"`
	Created time.Time `json:"created"`
	Epoch   int64     `json:"epoch"`
}

// emailClaim is emails/<eh>.json: which user owns an address. It is
// the record that must be unique, so it is the one created first.
type emailClaim struct {
	UID string `json:"uid"`
}

// ErrRecords answers SpendLinks in records mode.
var ErrRecords = errors.New("rastrillo/auth: SpendLinks is not available with Records: links are stored by token, not by address")

// errNoRecords answers the records-only methods on an Auth built with
// a database.
var errNoRecords = errors.New("rastrillo/auth: this needs Config.Records; with a database, end sessions with sessions.RevokeAll")

// uidRE is what newUID makes. Anything else is refused before it
// becomes part of a key: a uid comes from a cookie's subject, and a
// crafted one must not name some other record.
var uidRE = regexp.MustCompile(`^[a-z2-7]{26}$`)

var uidEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// newUID is a random 128-bit id in lowercase unpadded base32: 26
// characters, lowercase because objrec keys are.
func newUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToLower(uidEncoding.EncodeToString(b)), nil
}

// randomEpoch is a user's first epoch, in [0, 2^62). Not 0 and not the
// clock: a user deleted and created again would start where the old
// record did, and every cookie from before the deletion would refresh.
// The headroom above 2^62 keeps EndSessions' increment from ever
// overflowing.
func randomEpoch() (int64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(b[:]) >> 2), nil
}

func userKey(uid string) string { return "users/" + uid + ".json" }

func claimKey(eh string) string { return "emails/" + eh + ".json" }

// users keeps people in objrec: users/<uid>.json and the claim
// emails/<eh>.json that maps an address to its uid.
type users struct {
	store  objrec.Store
	pepper []byte
	// blind keeps the address out of users/; only EH is written.
	blind bool
	// now is a closure over auth's a.now, called on every use, for the
	// reason recordLinks.now gives.
	now func() time.Time
}

// ensure returns the user for a verified address, creating it on the
// first sign-in. The claim is written before the user (deviation 1):
// two devices signing in at once both try to create the claim and only
// one wins, so one address never gets two users. A claim whose user
// record is missing (the winner stopped between the two writes, or the
// record was deleted) is repaired here at the next sign-in.
func (u *users) ensure(ctx context.Context, address string) (User, error) {
	eh := emailHash(u.pepper, address)
	// Twice at most: a lost claim means another device's claim exists
	// now, and the second read finds it.
	for range 2 {
		claim, _, err := objrec.GetJSON[emailClaim](ctx, u.store, claimKey(eh))
		switch {
		case err == nil:
			if !uidRE.MatchString(claim.UID) {
				return User{}, fmt.Errorf("rastrillo/auth: %s holds a malformed uid", claimKey(eh))
			}
			usr, err := u.read(ctx, claim.UID)
			if errors.Is(err, objrec.ErrNotFound) {
				return u.createOrLoadUser(ctx, claim.UID, eh, address)
			}
			return usr, err
		case !errors.Is(err, objrec.ErrNotFound):
			return User{}, err
		}
		uid, err := newUID()
		if err != nil {
			return User{}, err
		}
		if _, err := objrec.CreateJSON(ctx, u.store, claimKey(eh), emailClaim{UID: uid}); err != nil {
			if errors.Is(err, objrec.ErrExists) {
				continue
			}
			return User{}, err
		}
		return u.createOrLoadUser(ctx, uid, eh, address)
	}
	return User{}, fmt.Errorf("rastrillo/auth: the claim %s changed while signing in", claimKey(eh))
}

// createOrLoadUser creates users/<uid>.json, and on ErrExists reads
// what is there. The device that won the claim can still lose this
// create, to another device that read the claim and repaired the
// missing user first; failing it then would refuse a sign-in that
// should succeed.
func (u *users) createOrLoadUser(ctx context.Context, uid, eh, address string) (User, error) {
	epoch, err := randomEpoch()
	if err != nil {
		return User{}, err
	}
	usr := User{ID: uid, EH: eh, Created: u.now().UTC(), Epoch: epoch}
	if !u.blind {
		usr.Email = address
	}
	_, err = objrec.CreateJSON(ctx, u.store, userKey(uid), usr)
	if errors.Is(err, objrec.ErrExists) {
		usr, err = u.read(ctx, uid)
	}
	if err != nil {
		return User{}, err
	}
	return usr, nil
}

// readAttempts bounds read's retries, as mutateAttempts bounds
// Mutate's: a few writers on one user settle well inside it.
const readAttempts = 8

// read is every read of users/<uid>.json. In blind mode it also strips
// an address written before Blind was turned on, by compare-and-swap,
// and returns the user without it. Without this, a centre that turns
// Blind on keeps every existing address readable in the store and
// through User, which is what blind mode promises not to do. The swap
// is conditional so a write that lands between the read and the strip
// (an epoch bump, or a sign-in on an instance not yet blind) is never
// overwritten: on ErrChanged it reads again and strips what is there.
func (u *users) read(ctx context.Context, uid string) (User, error) {
	for range readAttempts {
		usr, ver, err := objrec.GetJSON[User](ctx, u.store, userKey(uid))
		if err != nil || !u.blind || usr.Email == "" {
			return usr, err
		}
		usr.Email = ""
		_, err = objrec.UpdateJSON(ctx, u.store, userKey(uid), usr, ver)
		switch {
		case err == nil:
			return usr, nil
		case errors.Is(err, objrec.ErrChanged), errors.Is(err, objrec.ErrNotFound):
			// Read again: the new record may hold the address again, or
			// be gone, and the next read reports which.
			continue
		default:
			// Returning the user here would hand back an address the
			// store still holds, so a failed strip fails the read.
			return User{}, err
		}
	}
	return User{}, fmt.Errorf("rastrillo/auth: %s kept changing while its address was removed", userKey(uid))
}

// stripAll runs read on every user, so a centre that turns Blind on
// loses the addresses of people who never come back too, not only of
// those who sign in again. Sweep calls it in blind mode.
func (u *users) stripAll(ctx context.Context) error {
	cursor := ""
	for {
		keys, next, err := u.store.List(ctx, "users/", cursor, 0)
		if err != nil {
			return err
		}
		for _, k := range keys {
			uid, ok := strings.CutSuffix(strings.TrimPrefix(k, "users/"), ".json")
			// A key this store did not write is left alone, as the link
			// sweep leaves one, rather than read as a user.
			if !ok || !uidRE.MatchString(uid) {
				continue
			}
			if _, err := u.read(ctx, uid); err != nil && !errors.Is(err, objrec.ErrNotFound) {
				return err
			}
		}
		if next == "" {
			return nil
		}
		cursor = next
	}
}

// epoch is sessions.Signed.Epoch: read at sign-in and at every refresh.
func (u *users) epoch(ctx context.Context, uid string) (int64, error) {
	if !uidRE.MatchString(uid) {
		return 0, sessions.ErrNoUser
	}
	usr, err := u.read(ctx, uid)
	if errors.Is(err, objrec.ErrNotFound) {
		return 0, sessions.ErrNoUser
	}
	if err != nil {
		return 0, err
	}
	return usr.Epoch, nil
}

// User reads users/<uid>.json. A uid with no record, or one that is not
// a uid at all, is an error wrapping objrec.ErrNotFound.
func (a *Auth) User(ctx context.Context, uid string) (User, error) {
	if a.users == nil {
		return User{}, errNoRecords
	}
	if !uidRE.MatchString(uid) {
		return User{}, fmt.Errorf("rastrillo/auth: user %q: %w", uid, objrec.ErrNotFound)
	}
	usr, err := a.users.read(ctx, uid)
	if err != nil {
		return User{}, fmt.Errorf("rastrillo/auth: user %s: %w", uid, err)
	}
	return usr, nil
}

// EndSessions ends every session of uid within one cookie lifetime: it
// moves the epoch, and the refresh route signs out any cookie still
// carrying the old one. A user with no record answers
// sessions.ErrNoUser and nothing is written: Mutate would otherwise
// create a record with no id and no hash, a user nothing can sign in
// as and nothing removes.
func (a *Auth) EndSessions(ctx context.Context, uid string) error {
	if a.users == nil {
		return errNoRecords
	}
	if !uidRE.MatchString(uid) {
		return sessions.ErrNoUser
	}
	_, _, err := objrec.Mutate(ctx, a.users.store, userKey(uid), func(cur User, exists bool) (User, error) {
		if !exists {
			return User{}, sessions.ErrNoUser
		}
		cur.Epoch++
		// Mutate writes back the whole record it read, so in blind mode
		// an address from before Blind would be written again here.
		if a.users.blind {
			cur.Email = ""
		}
		return cur, nil
	})
	return err
}
