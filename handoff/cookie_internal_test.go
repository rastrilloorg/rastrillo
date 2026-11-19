package handoff

import "testing"

func TestNonceCookieRoundTrips(t *testing.T) {
	value := encodeNonceCookie("abc", "/events/1?x=2")
	nonce, next, ok := decodeNonceCookie(value)
	if !ok || nonce != "abc" || next != "/events/1?x=2" {
		t.Fatalf("got %q %q %v", nonce, next, ok)
	}
}

func TestNonceCookieRefusesAMalformedValue(t *testing.T) {
	values := []string{"", "abc", ".L2E", "abc.not base64!", "abc.L2E="}
	for _, value := range values {
		if _, _, ok := decodeNonceCookie(value); ok {
			t.Errorf("accepted %q", value)
		}
	}
}

func TestNonceCookieNextIsAlwaysLocal(t *testing.T) {
	value := encodeNonceCookie("abc", "https://evil.example/")
	_, next, ok := decodeNonceCookie(value)
	if !ok || next != "/" {
		t.Fatalf("got %q %v, want /", next, ok)
	}
}
