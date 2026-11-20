package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// The states the console derives for a custom domain (domainstate.go).
// It adds words over time, so treat one you do not know as
// DomainUnknown.
const (
	DomainUnverified  = "unverified"
	DomainChecking    = "checking"
	DomainPendingDNS  = "pending-dns"
	DomainMisdirected = "misdirected"
	DomainIssuingCert = "issuing-cert"
	DomainFailed      = "failed"
	DomainLive        = "live"
	DomainUnknown     = "unknown"
	DomainRegional    = "regional"
)

// maxDomainBody is how much of a domain read is read: the answer carries
// the hostname's whole published path map, up to 10,000 prefixes.
const maxDomainBody = 8 << 20

// DNSRecord is one record the console asks the owner to publish. Records
// sharing a Set are one alternative, such as a CNAME or a set of A
// records: publish every record of one set per name. Required marks a
// record needed whichever set is chosen, such as the ownership TXT.
type DNSRecord struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Value    string `json:"value"`
	TTL      int    `json:"ttl"`
	Set      string `json:"set"`
	Required bool   `json:"required,omitempty"`
}

// Attached is the console's answer to an attach. Verified is false while
// the platform waits for proof of ownership: Verify is then the TXT
// record to publish, and attaching again once it is published verifies
// the hostname.
type Attached struct {
	Host     string
	Verified bool
	Verify   *DNSRecord
}

// Domain is the console's reading of one hostname the app holds. DNS is
// the console's dns_status ("pointing", "elsewhere", "unresolved", or ""
// before an answer). Guidance and Records say what to publish, and are
// empty once DNS points here. CheckedAt and CertExpiry are zero until an
// edge has checked the name and a certificate exists.
type Domain struct {
	Host, Route string
	Verified    bool
	State       string
	Reason      string
	CheckedAt   time.Time
	DNS         string
	CNAMETarget string
	Guidance    string
	Records     []DNSRecord
	CertExpiry  time.Time
}

// domainItem is the per-host read's "domain" object (apidomains.go,
// domainListItem), with times in Unix seconds.
type domainItem struct {
	Host        string      `json:"host"`
	Route       string      `json:"route"`
	Verified    bool        `json:"verified"`
	State       string      `json:"state"`
	Reason      string      `json:"reason"`
	CheckedAt   int64       `json:"checked_at"`
	DNS         string      `json:"dns_status"`
	CNAMETarget string      `json:"cname_target"`
	Guidance    string      `json:"guidance"`
	Records     []DNSRecord `json:"records"`
	CertExpiry  int64       `json:"cert_expires_at"`
}

// AttachDomain claims host for the app, served as route, one of the
// app's instance hosts. Attaching a hostname the app already holds moves
// it to route, and is how a pending claim is verified.
//
// A hostname another app or account holds is ErrConflict, and so is a
// 409 the console gives for anything else. A 429, a 5xx or no answer is
// a *RetryError: the attach may have landed, and repeating it is safe.
func (console *Console) AttachDomain(
	ctx context.Context, host, route string,
) (Attached, error) {
	if host == "" {
		return Attached{}, fmt.Errorf("%w: no host", ErrBadRequest)
	}
	body := map[string]string{"host": host, "route": route}
	raw, err := console.domainCall(ctx, http.MethodPost,
		console.appPath("domains"), body, maxBody)
	if err != nil {
		return Attached{}, err
	}
	var answer struct {
		Host     string     `json:"host"`
		Verified bool       `json:"verified"`
		Verify   *DNSRecord `json:"verify"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil || answer.Host == "" {
		return Attached{}, &RetryError{
			Status: http.StatusOK, Message: message(raw), Err: err,
		}
	}
	attached := Attached{Host: answer.Host, Verified: answer.Verified}
	if !answer.Verified {
		attached.Verify = answer.Verify
	}
	return attached, nil
}

// Domain reads one hostname the app holds. A hostname it does not hold
// is ErrNotFound, whether or not anyone else holds it.
func (console *Console) Domain(
	ctx context.Context, host string,
) (Domain, error) {
	if host == "" {
		return Domain{}, fmt.Errorf("%w: no host", ErrBadRequest)
	}
	raw, err := console.domainCall(ctx, http.MethodGet,
		console.appPath("domains", host), nil, maxDomainBody)
	if err != nil {
		return Domain{}, err
	}
	var answer struct {
		Domain *domainItem `json:"domain"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil ||
		answer.Domain == nil {
		return Domain{}, &RetryError{
			Status: http.StatusOK, Message: message(raw), Err: err,
		}
	}
	item := answer.Domain
	return Domain{
		Host: item.Host, Route: item.Route, Verified: item.Verified,
		State: item.State, Reason: item.Reason,
		CheckedAt: unixTime(item.CheckedAt), DNS: item.DNS,
		CNAMETarget: item.CNAMETarget, Guidance: item.Guidance,
		Records: item.Records, CertExpiry: unixTime(item.CertExpiry),
	}, nil
}

// DetachDomain releases a hostname. Releasing one the app does not hold
// succeeds and changes nothing, so a retry after a lost answer is safe.
func (console *Console) DetachDomain(ctx context.Context, host string) error {
	if host == "" {
		return fmt.Errorf("%w: no host", ErrBadRequest)
	}
	_, err := console.domainCall(ctx, http.MethodDelete,
		console.appPath("domains", host), nil, maxBody)
	return err
}

// domainCall makes one attempt and classifies it as List does. It
// returns the body of a 200 only.
func (console *Console) domainCall(
	ctx context.Context, method, path string, body any, limit int64,
) ([]byte, error) {
	status, header, text, raw, err := console.callWithin(
		ctx, method, path, body, limit)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, contextErr
		}
		failure := retryError(status, header, text)
		failure.Err = err
		return nil, failure
	}
	switch {
	case status == http.StatusOK:
	case status == http.StatusTooManyRequests || status >= 500:
		return nil, retryError(status, header, text)
	default:
		return nil, &APIError{Status: status, Message: text}
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf(
			"provision: the domain answer is larger than %d bytes", limit)
	}
	return raw, nil
}

func unixTime(seconds int64) time.Time {
	if seconds <= 0 {
		return time.Time{}
	}
	return time.Unix(seconds, 0)
}
