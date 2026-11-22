package auth

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// carrierGradeNAT is 100.64.0.0/10, which includes the Tailscale range.
// netip's IsPrivate does not cover it.
var carrierGradeNAT = netip.MustParsePrefix("100.64.0.0/10")

// publicUnicast reports whether address is somewhere a stranger's
// server may legitimately live: not this machine, not a private or
// shared network, not a multicast or unspecified address.
func publicUnicast(address netip.Addr) bool {
	address = address.Unmap()
	if carrierGradeNAT.Contains(address) {
		return false
	}
	return address.IsGlobalUnicast() && !address.IsPrivate() &&
		!address.IsLoopback() && !address.IsLinkLocalUnicast()
}

var errNotPublic = errors.New("keymail: refusing a non-public address")

// refuseNonPublic runs after DNS resolution, on the address about to be
// dialed, so a name that resolves somewhere private (or changes its
// answer between lookups) cannot get past it.
func refuseNonPublic(_, address string, _ syscall.RawConn) error {
	target, err := netip.ParseAddrPort(address)
	if err != nil {
		return err
	}
	if !publicUnicast(target.Addr()) {
		return errNotPublic
	}
	return nil
}

// publicOnlyClient is the keymail client for an app that lists no
// servers: any delegated server may be dialed, but only at a public
// address, redirects included.
func publicOnlyClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// A proxy from the environment would dial on our behalf and so
	// bypass the address check.
	transport.Proxy = nil
	dialer := &net.Dialer{
		Timeout: 30 * time.Second, KeepAlive: 30 * time.Second,
		Control: refuseNonPublic,
	}
	transport.DialContext = dialer.DialContext
	return &http.Client{Transport: transport, Timeout: timeout}
}
