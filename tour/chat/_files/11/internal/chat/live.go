package chat

import (
	"net/http"
	"strconv"

	"amadan.net/rastrillo/rastrillo/auth"
)

// anyoneSignedIn may follow any room: every room is open to everyone
// who has signed in.
func anyoneSignedIn(request *http.Request, _ string) bool {
	_, signedIn := auth.From(request)
	return signedIn
}

// roomTopic names one room's messages. The page that shows them follows
// it, and posting a message moves it.
func roomTopic(id int64) string {
	return "room/" + strconv.FormatInt(id, 10)
}
