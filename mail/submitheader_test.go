package mail

import (
	"bytes"
	"context"
	"encoding/base64"
	"mime"
	netmail "net/mail"
	"strings"
	"testing"
)

// encodedAs wraps text as one RFC 2047 word in the given encoding,
// whatever it holds, as a caller trying to smuggle it would.
func encodedAs(encoding byte, text string) string {
	if encoding == 'b' {
		return "=?utf-8?b?" +
			base64.StdEncoding.EncodeToString([]byte(text)) + "?="
	}
	var out strings.Builder
	for _, octet := range []byte(text) {
		out.WriteString(qByte(octet))
	}
	return "=?utf-8?q?" + out.String() + "?="
}

func qByte(octet byte) string {
	const hex = "0123456789ABCDEF"
	return "=" + string(hex[octet>>4]) + string(hex[octet&15])
}

func TestEncodedControlsAreRefused(t *testing.T) {
	for _, control := range []string{
		"café\r\nBcc: victim@example.com", "a\x00b", "a\x7fb",
		"\x1b[31mred", "a\u0085b", "a\nb",
	} {
		for _, encoding := range []byte{'q', 'b'} {
			word := encodedAs(encoding, control)
			for field, message := range map[string]ListMessage{
				"Subject": {To: "who@example.com", Subject: word},
				"Reply-To": {To: "who@example.com",
					ReplyTo: word + " <r@x.example>"},
				"From": {To: "who@example.com"},
			} {
				from := testFrom
				if field == "From" {
					from = word + " <docs@app.example>"
				}
				name := field + "/" + string(encoding) + "/" + control
				t.Run(name, func(t *testing.T) {
					fake := newFakeControl(t)
					err := SendList(context.Background(), fake.sender(from),
						message)
					if err == nil || fake.requests() != 0 {
						t.Errorf("err = %v after %d requests; want a "+
							"local refusal", err, fake.requests())
					}
				})
			}
		}
	}
}

func TestAnEncodedWordInsideAnEncodedWordStaysText(t *testing.T) {
	inner := encodedAs('b', "x\r\nBcc: victim@example.com")
	outer := encodedAs('b', inner)
	raw := submittedRaw(t, "Team "+outer+" <docs@app.example>",
		ListMessage{To: "who@example.com", Subject: outer})
	// Not even inside a quoted string: many readers decode words there.
	if bytes.Contains(raw, []byte(inner)) {
		t.Errorf("the inner word is written literally:\n%s", raw)
	}
	parsed, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	var decoder mime.WordDecoder
	subject, err := decoder.DecodeHeader(parsed.Header.Get("Subject"))
	if err != nil || subject != inner {
		t.Errorf("Subject decodes once to %q, %v; want the literal %q",
			subject, err, inner)
	}
	from, err := parsed.Header.AddressList("From")
	if err != nil || len(from) != 1 || from[0].Name != "Team "+inner {
		t.Errorf("From decodes once to %v, %v", from, err)
	}
}

func TestLongHeadersFoldAndRoundTrip(t *testing.T) {
	words := strings.TrimSpace(strings.Repeat("plain word ", 75))
	for name, test := range map[string]struct{ name, subject string }{
		"accented subject": {"Docs", strings.Repeat("é", 160)},
		"long plain":       {"Docs", words},
		"too long plain":   {"Docs", words + " " + words},
		"no spaces":        {"Docs", strings.Repeat("x", 1200)},
		"long name":        {strings.Repeat("Páginas ", 60), "Hi"},
		"literal =?":       {"a =? b", "a =?utf-8?x b ?= c"},
	} {
		t.Run(name, func(t *testing.T) {
			from := (&netmail.Address{Name: test.name,
				Address: "docs@app.example"}).String()
			raw := submittedRaw(t, from, ListMessage{
				To: "who@example.com", Subject: test.subject})
			assertFoldedLines(t, raw)
			if _, ok := platformOneFrom(raw, "docs@app.example"); !ok {
				t.Errorf("the platform's From check fails")
			}
			parsed, err := netmail.ReadMessage(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			var decoder mime.WordDecoder
			subject, _ := decoder.DecodeHeader(parsed.Header.Get("Subject"))
			if subject != test.subject {
				t.Errorf("Subject round-trips to %q", subject)
			}
			address, err := parsed.Header.AddressList("From")
			if err != nil || len(address) != 1 || address[0].Name != test.name {
				t.Errorf("From round-trips to %v, %v", address, err)
			}
		})
	}
}

// assertFoldedLines wants every header line within RFC 5322's 998, no
// line of only white space, and the platform's validator satisfied.
func assertFoldedLines(t *testing.T, raw []byte) {
	t.Helper()
	header, err := platformHeaderBlock(raw)
	if err != nil {
		t.Fatalf("the platform refuses the folded header: %v", err)
	}
	for _, line := range strings.Split(string(header), "\r\n") {
		if len(line) > 998 {
			t.Errorf("a %d-character line: %.60q", len(line), line)
		}
		if line != "" && strings.TrimSpace(line) == "" {
			t.Errorf("a line of only white space")
		}
		for _, token := range strings.Fields(line) {
			if strings.HasPrefix(token, "=?") && len(token) > 75 {
				t.Errorf("a %d-character encoded word, over RFC 2047's "+
					"75: %.40q", len(token), token)
			}
		}
	}
}

func TestFoldingOnlyHappensBetweenEncodedWords(t *testing.T) {
	word := "=?utf-8?q?" + strings.Repeat("y", 63) + "?="
	line := "Subject: " + word + foldPoint + word + foldPoint + word
	physical := strings.Split(foldLine(line), "\r\n")
	if len(physical) != 3 || physical[0] != "Subject: "+word {
		t.Errorf("folded as %q", physical)
	}
	for _, each := range physical {
		if strings.TrimSpace(each) == "" {
			t.Errorf("a line of only white space in %q", physical)
		}
	}
	unfolded := strings.ReplaceAll(strings.Join(physical, "\r\n"), "\r\n",
		"")
	if unfolded != strings.ReplaceAll(line, foldPoint, " ") {
		t.Errorf("unfolding gives %q", unfolded)
	}
	plain := "Subject: " + strings.Repeat("x  ", 300)
	if folded := foldLine(plain); folded != plain {
		t.Errorf("a line with no encoded word was folded")
	}
}

// Folding at a space that belongs to the value loses it: unfolding
// joins the lines with one space, so two become one.
func TestSpacesInValuesSurviveLongHeaders(t *testing.T) {
	domain := strings.Repeat("d", 60) + ".example"
	mailbox := `"a  b"@` + domain
	for name, from := range map[string]string{
		"bare":         mailbox,
		"plain name":   "Docs <" + mailbox + ">",
		"encoded name": strings.Repeat("Páginas ", 12) + "<" + mailbox + ">",
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeControl(t)
			err := fake.sender(from).Send(context.Background(), mailbox,
				strings.Repeat("x", 50)+"  "+strings.Repeat("y", 30), "body")
			if err != nil {
				t.Fatalf("Send: %v", err)
			}
			raw := fake.raws[0]
			assertFoldedLines(t, raw)
			own := []byte("\r\n <" + mailbox + ">\r\n")
			if name == "encoded name" && !bytes.Contains(raw, own) {
				t.Errorf("the mailbox is not on a line of its own:\n%s", raw)
			}
			parsed, err := netmail.ReadMessage(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"From", "To"} {
				list, err := parsed.Header.AddressList(field)
				if err != nil || len(list) != 1 ||
					list[0].Address != "a  b@"+domain {
					t.Errorf("%s reads as %v, %v", field, list, err)
				}
			}
		})
	}
}

func TestSubjectWhitespaceRoundTrips(t *testing.T) {
	for _, subject := range []string{
		strings.Repeat("x", 50) + "  " + strings.Repeat("y", 30),
		"a   b\tc  d",
		strings.Repeat("word  ", 160) + "end",
		strings.Repeat("é  ", 100) + "end",
	} {
		raw := submittedRaw(t, testFrom,
			ListMessage{To: "who@example.com", Subject: subject})
		assertFoldedLines(t, raw)
		parsed, err := netmail.ReadMessage(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		var decoder mime.WordDecoder
		got, _ := decoder.DecodeHeader(parsed.Header.Get("Subject"))
		if got != subject {
			t.Errorf("Subject %.30q round-trips to %.30q", subject, got)
		}
	}
}

func TestAHeaderThatCannotFoldIsRefused(t *testing.T) {
	fake := newFakeControl(t)
	err := SendList(context.Background(), fake.sender(testFrom), ListMessage{
		To: "who@example.com", Subject: "Hi",
		Unsubscribe: "https://example.com/u/" + strings.Repeat("x", 1000)})
	if err == nil || fake.requests() != 0 {
		t.Errorf("err = %v after %d requests; want a local refusal", err,
			fake.requests())
	}
}
