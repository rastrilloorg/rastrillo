package op

import (
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

// EncodeCursor is the opaque next_after for a page that ends at the row
// (createdAt, id). It carries that row's own values rather than its id
// alone, so the next page starts in the right place in the list's
// (created_at, id) order, and still does after that row is deleted.
func EncodeCursor(createdAt string, id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(createdAt + "|" + strconv.FormatInt(id, 10)))
}

// DecodeCursor reverses EncodeCursor. The empty cursor is the first
// page: ("", 0, true). Anything EncodeCursor could not have made is
// ok == false, so a hand-edited cursor is refused rather than read as
// some other place in the list.
func DecodeCursor(s string) (createdAt string, id int64, ok bool) {
	if s == "" {
		return "", 0, true
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return "", 0, false
	}
	at, idText, found := strings.Cut(string(raw), "|")
	if !found {
		return "", 0, false
	}
	if _, err := time.Parse(time.RFC3339, at); err != nil {
		return "", 0, false
	}
	id, err = strconv.ParseInt(idText, 10, 64)
	if err != nil || id < 1 {
		return "", 0, false
	}
	return at, id, true
}
