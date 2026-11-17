package mail

import (
	"bytes"
	"fmt"
	"mime"
	"mime/quotedprintable"
	netmail "net/mail"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// envelopeRecipient is to as the verb's envelope wants it: one address
// with no display name, as an addr-spec the platform parses back to the
// same mailbox. ParseAddress decodes a quoted local part ("a b"@x to
// a b@x), so its Address is re-quoted rather than sent as it comes.
func envelopeRecipient(to string) (string, error) {
	address, err := asciiAddress("recipient", to)
	if err != nil {
		return "", err
	}
	if address.Name != "" {
		return "", fmt.Errorf("rastrillo/mail: recipient %q has a display "+
			"name; send to the bare address (who@example.com)", to)
	}
	return addrSpec(address.Address), nil
}

// envelopeFrom is the From address as the verb's envelope wants it: the
// addr-spec alone, which the platform compares with the From: header.
func envelopeFrom(from string) (string, error) {
	address, err := asciiAddress("From", from)
	if err != nil {
		return "", err
	}
	return addrSpec(address.Address), nil
}

// asciiAddress parses one address whose mailbox is ASCII. A UTF-8
// mailbox needs SMTPUTF8, which nothing here can promise the platform
// or the recipient's server will speak.
func asciiAddress(field, value string) (*netmail.Address, error) {
	address, err := netmail.ParseAddress(value)
	if err != nil || !strings.Contains(address.Address, "@") {
		return nil, fmt.Errorf("rastrillo/mail: %s %q is not an email "+
			"address (want who@example.com)", field, value)
	}
	if !ascii(address.Address) {
		return nil, fmt.Errorf("rastrillo/mail: %s %q is not an ASCII "+
			"address; use its ASCII form", field, value)
	}
	return address, nil
}

// addrSpec quotes a mailbox's local part where it needs quoting, as
// net/mail's own formatter does, without the angle brackets.
func addrSpec(mailbox string) string {
	quoted := (&netmail.Address{Address: mailbox}).String()
	return strings.TrimSuffix(strings.TrimPrefix(quoted, "<"), ">")
}

// headerAddress is an address for a header, its mailbox quoted as
// needed. ParseAddress has already decoded any encoded words in the
// display name, so the decoded name is what is checked, and it is
// written out again by this package's own rule: an RFC 2047 word that
// decodes to a line break would otherwise pass every check as ASCII.
func headerAddress(field, value string) (string, error) {
	address, err := asciiAddress(field, value)
	if err != nil {
		return "", err
	}
	if address.Name == "" {
		return addrSpec(address.Address), nil
	}
	if err := decodedText(field+" display name", address.Name); err != nil {
		return "", err
	}
	if plainText(address.Name) {
		return address.String(), nil
	}
	mailbox := (&netmail.Address{Address: address.Address}).String()
	return encodedWords(address.Name) + foldPoint + mailbox, nil
}

func ascii(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// submitMessage is the raw message for the platform. SES takes a raw
// message as MIME, so a header that is not ASCII is RFC 2047 encoded and
// the body is quoted-printable. The inputs are checked before encoding,
// because encoding would hide a control byte from the platform's
// header validator rather than remove it.
func submitMessage(
	from string, list ListMessage, now time.Time,
) ([]byte, error) {
	if err := submitInputs(from, list); err != nil {
		return nil, err
	}
	encoded, headerFrom, err := encodedHeaders(from, list)
	if err != nil {
		return nil, err
	}
	raw, err := message(headerFrom, encoded, now)
	if err != nil {
		return nil, err
	}
	header, _, _ := bytes.Cut(raw, []byte("\r\n\r\n"))
	if header, err = foldHeader(header); err != nil {
		return nil, err
	}
	if err := headerControlBytes(header); err != nil {
		return nil, err
	}
	raw = slices.Concat(header,
		[]byte("\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n"),
		quotedPrintable([]byte(list.Body)))
	if len(raw) > maxSubmitRaw {
		return nil, fmt.Errorf("rastrillo/mail: the message is %d bytes "+
			"encoded, over the platform's %d; send a link instead",
			len(raw), maxSubmitRaw)
	}
	return raw, nil
}

// encodedHeaders is list with its To, Subject and Reply-To ready for a
// MIME header, and the From: header's value. message adds no encoding,
// and SMTP keeps sending what it always has. To is the bare mailbox, so
// a comment in it never reaches a reader's display.
func encodedHeaders(
	from string, list ListMessage,
) (ListMessage, string, error) {
	headerFrom, err := headerAddress("From", from)
	if err != nil {
		return list, "", err
	}
	if list.To, err = envelopeRecipient(list.To); err != nil {
		return list, "", err
	}
	if list.Subject, err = headerSubject(list.Subject); err != nil {
		return list, "", err
	}
	if list.ReplyTo != "" {
		list.ReplyTo, err = headerAddress("Reply-To", list.ReplyTo)
	}
	list.Body = ""
	return list, headerFrom, err
}

// headerSubject is subject decoded, checked and encoded again. A caller
// that hands in RFC 2047 words is decoded first, because the words are
// ASCII and would pass the input check whatever they decode to.
func headerSubject(subject string) (string, error) {
	var decoder mime.WordDecoder
	text, err := decoder.DecodeHeader(subject)
	if err != nil {
		return "", fmt.Errorf("rastrillo/mail: Subject %q has an encoded "+
			"word this package cannot read (%v); send it as plain text",
			subject, err)
	}
	if err := decodedText("Subject", text); err != nil {
		return "", err
	}
	if plainText(text) {
		return text, nil
	}
	return encodedWords(text), nil
}

// decodedText refuses text that is not UTF-8 or holds a control
// character, once any encoding is undone.
func decodedText(field, text string) error {
	if !utf8.ValidString(text) {
		return fmt.Errorf("rastrillo/mail: %s %q is not UTF-8", field, text)
	}
	if index := strings.IndexFunc(text, controlRune); index >= 0 {
		char, _ := utf8.DecodeRuneInString(text[index:])
		return fmt.Errorf("rastrillo/mail: %s %q holds control character "+
			"%U; remove it", field, text, char)
	}
	return nil
}

// submitInputs refuses, in the values as the caller gave them, what no
// encoding makes safe: a control byte or line break anywhere, text that
// is not UTF-8, and anything but ASCII where a header carries an
// identifier rather than prose.
func submitInputs(from string, list ListMessage) error {
	prose := map[string]string{
		"From": from, "Subject": list.Subject, "Reply-To": list.ReplyTo,
	}
	identifiers := map[string]string{
		"To": list.To, "List-Id": list.ListID,
		"Message-Id": list.MessageID, "List-Unsubscribe": list.Unsubscribe,
	}
	for name, value := range identifiers {
		if !ascii(value) {
			return fmt.Errorf("rastrillo/mail: %s %q is not ASCII; use "+
				"its ASCII form", name, value)
		}
		prose[name] = value
	}
	for name, value := range prose {
		if err := decodedText(name, value); err != nil {
			return err
		}
	}
	return nil
}

// controlRune is a line break, NUL, DEL or other C0 or C1 control
// character: none may reach a header, literally or encoded. A tab is
// whitespace.
func controlRune(char rune) bool {
	return char != '\t' && unicode.IsControl(char)
}

// headerControlBytes refuses a line break, NUL, DEL or other control
// byte in a header value; a tab is whitespace. The platform refuses them
// too, so a header SES might read differently never leaves. In a built
// header it is checked line by line, between the CRLFs message wrote.
func headerControlBytes(header []byte) error {
	for _, line := range bytes.Split(header, []byte("\r\n")) {
		for _, char := range line {
			if (char < 0x20 && char != '\t') || char == 0x7f {
				return fmt.Errorf("rastrillo/mail: header %q holds "+
					"control byte %#x; remove it", line, char)
			}
		}
	}
	return nil
}

// quotedPrintable is body as quoted-printable with CRLF line endings.
// Lines are made CRLF first: a bare CR would otherwise be encoded as a
// character, and a reader would see it.
func quotedPrintable(body []byte) []byte {
	var out bytes.Buffer
	writer := quotedprintable.NewWriter(&out)
	_, _ = writer.Write(crlf(body))
	_ = writer.Close()
	return out.Bytes()
}

// crlf turns every bare LF, and every bare CR, into CRLF.
func crlf(body []byte) []byte {
	out := make([]byte, 0, len(body)+len(body)/32)
	for index := 0; index < len(body); index++ {
		switch char := body[index]; {
		case char == '\r' && index+1 < len(body) && body[index+1] == '\n':
			out = append(out, '\r', '\n')
			index++
		case char == '\r' || char == '\n':
			out = append(out, '\r', '\n')
		default:
			out = append(out, char)
		}
	}
	return out
}
