package blobs

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// S3Config configures an S3 store. Bucket and Region are required;
// Endpoint is only for non-AWS backends (path-style addressing is used
// when it is set, virtual-hosted otherwise — the same convention the
// platform's own s3compat layer follows).
type S3Config struct {
	Bucket    string
	Region    string
	Endpoint  string // e.g. "https://minio.local:9000"; empty = AWS
	AccessKey string
	SecretKey string
	Prefix    string // key prefix inside the bucket; default "blobs/"
	HTTP      *http.Client
}

// S3 is a Store over one bucket, speaking the platform's delivered
// contract and nothing more: Put/Get/Delete with the delivered
// credentials against the delivered endpoint — no versioning, no
// conditional writes, no SDK. Presigned URLs are minted here (PresignGet
// /PresignPut) because the platform deliberately ships no signing
// service.
type S3 struct {
	cfg S3Config
}

// NewS3 validates cfg and returns the store.
func NewS3(cfg S3Config) (*S3, error) {
	if cfg.Bucket == "" || cfg.Region == "" {
		return nil, fmt.Errorf("rastrillo/blobs: S3 needs Bucket and Region (got %q, %q)", cfg.Bucket, cfg.Region)
	}
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("rastrillo/blobs: S3 needs credentials")
	}
	if cfg.Prefix == "" {
		cfg.Prefix = "blobs/"
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 60 * time.Second}
	}
	return &S3{cfg}, nil
}

// S3FromEnv builds an S3 store from the platform's object-storage
// primitive env surface: CARLOS_STORE_{BUCKET, REGION, ENDPOINT,
// ACCESS_KEY, SECRET_KEY} — the vars `carlos store grant` materializes
// into the app's config env. No bucket configured (BUCKET unset) is not
// an error, it is the answer "this app has no store": (nil, nil), so
// callers can fall back to Inline or Dir without special-casing.
func S3FromEnv() (*S3, error) {
	bucket := os.Getenv("CARLOS_STORE_BUCKET")
	if bucket == "" {
		return nil, nil
	}
	return NewS3(S3Config{
		Bucket:    bucket,
		Region:    os.Getenv("CARLOS_STORE_REGION"),
		Endpoint:  os.Getenv("CARLOS_STORE_ENDPOINT"),
		AccessKey: os.Getenv("CARLOS_STORE_ACCESS_KEY"),
		SecretKey: os.Getenv("CARLOS_STORE_SECRET_KEY"),
	})
}

// baseURL returns scheme+host for requests, and the path prefix that
// addressing style puts before the key.
func (s *S3) baseURL() (host string, pathPrefix string) {
	if s.cfg.Endpoint != "" {
		return strings.TrimSuffix(s.cfg.Endpoint, "/"), "/" + s.cfg.Bucket
	}
	return "https://" + s.cfg.Bucket + ".s3." + s.cfg.Region + ".amazonaws.com", ""
}

func (s *S3) key(hash string) string { return s.cfg.Prefix + hash }

func (s *S3) newRequest(ctx context.Context, method, hash string, body []byte, contentType string) (*http.Request, error) {
	base, prefix := s.baseURL()
	u := base + prefix + "/" + s.key(hash)
	req, err := http.NewRequestWithContext(ctx, method, u, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	payloadHash := emptyPayloadHash
	if len(body) > 0 {
		payloadHash = hexSHA256(body)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("x-amz-content-sha256", payloadHash)
	req.Header.Set("x-amz-date", time.Now().UTC().Format("20060102T150405Z"))
	awsSignV4(req, s.cfg.AccessKey, s.cfg.SecretKey, s.cfg.Region, "s3", time.Now())
	return req, nil
}

func (s *S3) Put(ctx context.Context, r io.Reader, contentType string) (Ref, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return Ref{}, err
	}
	ref := Ref{Hash: hashOf(data), Size: int64(len(data)), ContentType: contentType}
	req, err := s.newRequest(ctx, http.MethodPut, ref.Hash, data, contentType)
	if err != nil {
		return Ref{}, err
	}
	resp, err := s.cfg.HTTP.Do(req)
	if err != nil {
		return Ref{}, fmt.Errorf("rastrillo/blobs: s3 put: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return Ref{}, s3Error("put", resp)
	}
	return ref, nil
}

func (s *S3) Get(ctx context.Context, hash string) (io.ReadCloser, error) {
	if err := checkHash(hash); err != nil {
		return nil, err
	}
	req, err := s.newRequest(ctx, http.MethodGet, hash, nil, "")
	if err != nil {
		return nil, err
	}
	resp, err := s.cfg.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rastrillo/blobs: s3 get: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, ErrNotFound
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		return nil, s3Error("get", resp)
	}
	return resp.Body, nil
}

func (s *S3) Delete(ctx context.Context, hash string) error {
	if err := checkHash(hash); err != nil {
		return err
	}
	req, err := s.newRequest(ctx, http.MethodDelete, hash, nil, "")
	if err != nil {
		return err
	}
	resp, err := s.cfg.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("rastrillo/blobs: s3 delete: %w", err)
	}
	defer resp.Body.Close()
	// Deleting what is not there is not an error — S3 answers 204 either way.
	if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusNotFound {
		return s3Error("delete", resp)
	}
	return nil
}

func s3Error(op string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("rastrillo/blobs: s3 %s: %s: %s", op, resp.Status, strings.TrimSpace(string(body)))
}

// PresignGet mints a URL that downloads the blob directly from the
// bucket for ttl — the §5 "download straight to the bucket" path, bytes
// never proxied through the app process.
func (s *S3) PresignGet(hash string, ttl time.Duration) (string, error) {
	if err := checkHash(hash); err != nil {
		return "", err
	}
	return s.presign(http.MethodGet, hash, ttl)
}

// PresignPut mints a URL that uploads directly to the blob's key for
// ttl. The caller must already know the content hash (the client
// computes it before asking) — that is what makes the key content-
// addressed; note that without conditional-write support in the
// platform contract, the server should verify the uploaded bytes hash
// to the key before trusting the blob (fetch-and-check, or size/etag
// comparison).
func (s *S3) PresignPut(hash string, ttl time.Duration) (string, error) {
	if err := checkHash(hash); err != nil {
		return "", err
	}
	return s.presign(http.MethodPut, hash, ttl)
}

// presign implements SigV4 query-parameter signing ("Authenticating
// Requests: Using Query Parameters"): credentials ride the query,
// only host is signed, and the payload is UNSIGNED-PAYLOAD (the bytes
// aren't known when the URL is minted). Pinned against the official
// worked example in sigv4_test.go.
func (s *S3) presign(method, hash string, ttl time.Duration) (string, error) {
	base, prefix := s.baseURL()
	return presignURL(method, base+prefix+"/"+s.key(hash),
		s.cfg.Region, s.cfg.AccessKey, s.cfg.SecretKey, ttl, time.Now())
}

// presignURL signs one URL with query-parameter SigV4. Split from the
// store so the algorithm is pinnable against the official AWS worked
// example verbatim, legacy global host and all.
func presignURL(method, rawURL, region, accessKey, secretKey string, ttl time.Duration, now time.Time) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	amzDate := now.UTC().Format("20060102T150405Z")
	scope := now.UTC().Format("20060102") + "/" + region + "/s3/aws4_request"

	q := url.Values{}
	q.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	q.Set("X-Amz-Credential", accessKey+"/"+scope)
	q.Set("X-Amz-Date", amzDate)
	q.Set("X-Amz-Expires", strconv.Itoa(int(ttl.Seconds())))
	q.Set("X-Amz-SignedHeaders", "host")

	canonical := method + "\n" +
		u.EscapedPath() + "\n" +
		canonicalQuery(q) + "\n" +
		"host:" + u.Host + "\n" +
		"\n" +
		"host\n" +
		"UNSIGNED-PAYLOAD"

	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hexSHA256([]byte(canonical))
	kSig := hmacSHA256([]byte("AWS4"+secretKey), now.UTC().Format("20060102"))
	kSig = hmacSHA256(kSig, region)
	kSig = hmacSHA256(kSig, "s3")
	kSig = hmacSHA256(kSig, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(kSig, stringToSign))

	u.RawQuery = canonicalQuery(q) + "&X-Amz-Signature=" + sig
	return u.String(), nil
}
