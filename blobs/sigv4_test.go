package blobs

// Pins the hand-rolled signer against the official worked examples in the
// AWS docs ("Signature Calculations for the Authorization Header" — the
// examplebucket vectors, signing key AKIAIOSFODNN7EXAMPLE, 2013-05-24), so
// the signer is testable offline.

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSigV4Vectors(t *testing.T) {
	const (
		accessKey = "AKIAIOSFODNN7EXAMPLE"
		secretKey = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
		amzDate   = "20130524T000000Z"
	)
	ts := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name, method, url string
		headers           map[string]string
		wantSignedHeaders string
		wantSig           string
	}{
		{
			// GET Object: GET /test.txt with a Range header.
			name:   "GetObject",
			method: "GET",
			url:    "https://examplebucket.s3.amazonaws.com/test.txt",
			headers: map[string]string{
				"Range":                "bytes=0-9",
				"x-amz-content-sha256": emptyPayloadHash,
				"x-amz-date":           amzDate,
			},
			wantSignedHeaders: "host;range;x-amz-content-sha256;x-amz-date",
			wantSig:           "f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41",
		},
		{
			// GET Bucket Lifecycle: a valueless subresource must canonicalize
			// as "lifecycle=" — the same shape our ListObjectVersions uses.
			name:   "GetBucketLifecycle",
			method: "GET",
			url:    "https://examplebucket.s3.amazonaws.com/?lifecycle",
			headers: map[string]string{
				"x-amz-content-sha256": emptyPayloadHash,
				"x-amz-date":           amzDate,
			},
			wantSignedHeaders: "host;x-amz-content-sha256;x-amz-date",
			wantSig:           "fea454ca298b7da1c68078a5d1bdbfbbe0d65c699e0f91ac7a200a0136783543",
		},
		{
			// List Objects: multiple query params, sorted canonically.
			name:   "ListObjects",
			method: "GET",
			url:    "https://examplebucket.s3.amazonaws.com/?max-keys=2&prefix=J",
			headers: map[string]string{
				"x-amz-content-sha256": emptyPayloadHash,
				"x-amz-date":           amzDate,
			},
			wantSignedHeaders: "host;x-amz-content-sha256;x-amz-date",
			wantSig:           "34b48302e7b5fa45bde8084f4b7868a86f0a534bc59db6670ed5711ef69dc6f7",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, tc.url, nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			awsSignV4(req, accessKey, secretKey, "us-east-1", "s3", ts)
			auth := req.Header.Get("Authorization")
			if !strings.Contains(auth, "SignedHeaders="+tc.wantSignedHeaders+",") {
				t.Errorf("signed headers: want %q in %q", tc.wantSignedHeaders, auth)
			}
			if !strings.HasSuffix(auth, "Signature="+tc.wantSig) {
				t.Errorf("signature mismatch:\n got %q\nwant …Signature=%s", auth, tc.wantSig)
			}
		})
	}
}

// The SigV4 path/query encoders have their own rules (uppercase hex, '+' is
// %2B never a space, '/' survives only in paths); a wrong encoder signs
// requests S3 will reject, so pin the corners.
func TestAWSURIEncode(t *testing.T) {
	for _, tc := range []struct {
		in          string
		encodeSlash bool
		want        string
	}{
		{"blobs/u1", false, "blobs/u1"},
		{"blobs/u1", true, "blobs%2Fu1"},
		{"a b+c", true, "a%20b%2Bc"},
		{"ok-._~", true, "ok-._~"},
		{"é", true, "%C3%A9"},
	} {
		if got := awsURIEncode(tc.in, tc.encodeSlash); got != tc.want {
			t.Errorf("awsURIEncode(%q, %v) = %q, want %q", tc.in, tc.encodeSlash, got, tc.want)
		}
	}
}
