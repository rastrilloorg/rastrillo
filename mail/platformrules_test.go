package mail

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	netmail "net/mail"
	"strings"
)

// A copy of the CARLOS platform's rules for a submitted message, so the
// tests hold what this package builds to what the platform accepts.
// Copied from github.com/carlosframework/platform at eeb5100a3:
// internal/mailsubmit/verb.go (readEnvelope), internal/mailer/hygiene.go
// (splitHeader, headerBlock), internal/mailer/policy.go (oneFrom) and
// internal/mailer/send.go (validRequest's recipients). Only names are
// changed, to this repository's rule against one-letter names. If the
// platform's rules change, copy them again.

const (
	platformMaxRaw        = 10 << 20
	platformMaxRecipients = 50
	platformMaxBody       = 14 << 20
)

// platformEnvelopeWire is mailsubmit.Envelope, tags and all. It is not
// the production submitEnvelope: sharing that would let a wrong tag on
// the client pass because the fake shares it.
type platformEnvelopeWire struct {
	From string   `json:"from"`
	To   []string `json:"to"`
	Raw  string   `json:"raw"`
}

var (
	errPlatformNoSeparator = errors.New("no header/body separator")
	errPlatformSyntax      = errors.New("malformed header block")
)

// platformEnvelope is readEnvelope.
func platformEnvelope(
	response http.ResponseWriter, request *http.Request,
) (platformEnvelopeWire, []byte, bool) {
	var env platformEnvelopeWire
	err := json.NewDecoder(http.MaxBytesReader(response, request.Body,
		platformMaxBody)).Decode(&env)
	if err != nil {
		return env, nil, false
	}
	if strings.TrimSpace(env.From) == "" || len(env.To) == 0 ||
		len(env.To) > platformMaxRecipients {
		return env, nil, false
	}
	for _, to := range env.To {
		if strings.TrimSpace(to) == "" {
			return env, nil, false
		}
	}
	if base64.StdEncoding.DecodedLen(len(env.Raw)) > platformMaxRaw+2 {
		return env, nil, false
	}
	raw, err := base64.StdEncoding.DecodeString(env.Raw)
	if err != nil || len(raw) == 0 || len(raw) > platformMaxRaw {
		return env, nil, false
	}
	return env, raw, true
}

// platformRecipients is validRequest's recipient rule: each one address
// with no display name.
func platformRecipients(to []string) bool {
	for _, raw := range to {
		address, err := netmail.ParseAddress(raw)
		if err != nil || address.Name != "" ||
			!strings.Contains(address.Address, "@") {
			return false
		}
	}
	return true
}

// platformSplitHeader is splitHeader.
func platformSplitHeader(raw []byte) (header, body []byte, ok bool) {
	for start := 0; start <= len(raw); {
		end := bytes.IndexByte(raw[start:], '\n')
		if end < 0 {
			return nil, nil, false
		}
		line := raw[start : start+end]
		if len(line) == 0 || (len(line) == 1 && line[0] == '\r') {
			return raw[:start], raw[start+end+1:], true
		}
		start += end + 1
	}
	return nil, nil, false
}

// platformHeaderBlock is headerBlock.
func platformHeaderBlock(raw []byte) ([]byte, error) {
	header, _, ok := platformSplitHeader(raw)
	if !ok {
		return nil, errPlatformNoSeparator
	}
	first := true
	for start := 0; start < len(header); {
		end := bytes.IndexByte(header[start:], '\n')
		line := bytes.TrimSuffix(header[start:start+end], []byte{'\r'})
		start += end + 1
		value := line
		if line[0] == ' ' || line[0] == '\t' {
			if first {
				return nil, errPlatformSyntax
			}
		} else {
			colon := bytes.IndexByte(line, ':')
			if colon <= 0 {
				return nil, errPlatformSyntax
			}
			for _, char := range line[:colon] {
				if char < 33 || char > 126 {
					return nil, errPlatformSyntax
				}
			}
			value = line[colon+1:]
		}
		for _, char := range value {
			if (char < 0x20 && char != '\t') || char == 0x7f {
				return nil, errPlatformSyntax
			}
		}
		first = false
	}
	return header, nil
}

// platformOneFrom is oneFrom.
func platformOneFrom(raw []byte, envelopeFrom string) (string, bool) {
	want, err := netmail.ParseAddress(envelopeFrom)
	if err != nil || !strings.Contains(want.Address, "@") {
		return "", false
	}
	header, err := platformHeaderBlock(raw)
	if err != nil {
		return "", false
	}
	msg, err := netmail.ReadMessage(bytes.NewReader(
		append(append([]byte(nil), header...), "\r\n"...)))
	if err != nil {
		return "", false
	}
	froms := msg.Header["From"]
	if len(froms) != 1 {
		return "", false
	}
	list, err := netmail.ParseAddressList(froms[0])
	if err != nil || len(list) != 1 ||
		!strings.EqualFold(list[0].Address, want.Address) {
		return "", false
	}
	addr := list[0].Address
	at := strings.LastIndexByte(addr, '@')
	if at <= 0 || at == len(addr)-1 {
		return "", false
	}
	return addr, true
}
