// Package oauth lets an agent connect to an app on a person's behalf.
package oauth

import (
	"errors"
	"time"

	rastrillo "amadan.net/rastrillo/rastrillo"
)

// Client is an application that asks to connect.
type Client struct {
	ID, Name     string
	Redirects    []string
	Document     bool // identified by a metadata document
	RegisteredAt time.Time
	UsedAt       time.Time
}

// Connection is what a person approved for one client.
type Connection struct {
	ID, Subject, ClientID, ClientName, ReturnHost string
	// Resource is the one the person approved.
	Resource          string
	Access            rastrillo.Access
	Created, LastUsed time.Time
	Ended             *time.Time
}

// Pending is an authorization request waiting for the person's answer.
type Pending struct {
	ID, Subject, ClientID, Redirect, Challenge, Resource, State string
	// ClientName is the name the approve page showed.
	ClientName string
	// StateSent tells an empty state from none, since a sent one must
	// come back even when empty.
	StateSent bool
	// Return is the authorize GET's local path and query.
	Return    string
	Requested rastrillo.Access
	Expires   time.Time
}

// Code is an authorization code, held by hash.
type Code struct {
	Hash                                                []byte
	Connection, ClientID, Redirect, Challenge, Resource string
	Expires                                             time.Time
}

// Refresh is a refresh token, held by hash.
type Refresh struct {
	Hash       []byte
	Connection string
	Expires    time.Time
}

// ErrNotFound is every refusal to hand something back: absent,
// expired, or not the caller's.
var ErrNotFound = errors.New("oauth: not found")
