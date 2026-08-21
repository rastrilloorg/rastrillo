package flash

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSetThenTake(t *testing.T) {
	// Set(w, "notice", "Note created.") on a recorder; carry the
	// Set-Cookie into a new request; Take returns
	// Flash{Kind: "notice", Message: "Note created."}, ok=true, and
	// writes a clearing Set-Cookie (MaxAge < 0) — one-shot.

	// Step 1: Set a flash cookie
	w := httptest.NewRecorder()
	Set(w, "notice", "Note created.")

	// Verify that a Set-Cookie was written
	if w.Header().Get("Set-Cookie") == "" {
		t.Error("Set-Cookie header not written")
	}

	// Step 2: Extract the cookie from the response and put it in a new request
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("No cookies in response")
	}

	// Create a new request with the cookie
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookies[0])

	// Step 3: Take the flash
	w2 := httptest.NewRecorder()
	flash, ok := Take(w2, req)

	// Verify the flash was returned correctly
	if !ok {
		t.Error("Take returned ok=false, expected true")
	}
	if flash.Kind != "notice" {
		t.Errorf("Kind = %q, want %q", flash.Kind, "notice")
	}
	if flash.Message != "Note created." {
		t.Errorf("Message = %q, want %q", flash.Message, "Note created.")
	}

	// Verify a clearing cookie was written
	clearCookies := w2.Result().Cookies()
	if len(clearCookies) == 0 {
		t.Fatal("No clearing cookie written")
	}
	clearCookie := clearCookies[0]
	if clearCookie.MaxAge >= 0 {
		t.Errorf("Clearing cookie MaxAge = %d, expected < 0", clearCookie.MaxAge)
	}
}

func TestTakeWithoutFlash(t *testing.T) {
	// No cookie → ok=false, and no Set-Cookie written.

	// Create a request without any cookie
	req := httptest.NewRequest("GET", "/", nil)

	w := httptest.NewRecorder()
	flash, ok := Take(w, req)

	// Verify ok is false
	if ok {
		t.Error("Take returned ok=true, expected false")
	}

	// Verify no Flash fields are set
	if flash.Kind != "" {
		t.Errorf("Kind = %q, expected empty", flash.Kind)
	}
	if flash.Message != "" {
		t.Errorf("Message = %q, expected empty", flash.Message)
	}

	// Verify no Set-Cookie was written
	if w.Header().Get("Set-Cookie") != "" {
		t.Error("Set-Cookie header was written, expected none")
	}
}

func TestTakeGarbageCookie(t *testing.T) {
	// Cookie value "not!base64" → ok=false, cookie still cleared.

	// Create a request with a garbage cookie
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{
		Name:  "rastrillo_flash",
		Value: "not!base64",
	})

	w := httptest.NewRecorder()
	flash, ok := Take(w, req)

	// Verify ok is false
	if ok {
		t.Error("Take returned ok=true for garbage cookie, expected false")
	}

	// Verify no Flash fields are set
	if flash.Kind != "" {
		t.Errorf("Kind = %q, expected empty", flash.Kind)
	}
	if flash.Message != "" {
		t.Errorf("Message = %q, expected empty", flash.Message)
	}

	// Verify a clearing cookie was still written
	clearCookies := w.Result().Cookies()
	if len(clearCookies) == 0 {
		t.Fatal("No clearing cookie written")
	}
	clearCookie := clearCookies[0]
	if clearCookie.MaxAge >= 0 {
		t.Errorf("Clearing cookie MaxAge = %d, expected < 0", clearCookie.MaxAge)
	}
}

func TestMessageSurvivesEncoding(t *testing.T) {
	// Message with spaces, unicode and a newline round-trips intact
	// (cookie values can't carry these raw — encoding is the point).

	message := "Hello 世界\nWith spaces and unicode!🎉"
	kind := "info"

	// Set a flash with complex message
	w := httptest.NewRecorder()
	Set(w, kind, message)

	// Extract and carry the cookie to a new request
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("No cookies in response")
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookies[0])

	// Take the flash
	w2 := httptest.NewRecorder()
	flash, ok := Take(w2, req)

	// Verify the message survived intact
	if !ok {
		t.Error("Take returned ok=false, expected true")
	}
	if flash.Kind != kind {
		t.Errorf("Kind = %q, want %q", flash.Kind, kind)
	}
	if flash.Message != message {
		t.Errorf("Message = %q, want %q", flash.Message, message)
	}
}
