package objrec

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/internal/sigv4"
)

// S3Config configures an S3 store. Bucket, Region and the credentials
// are required; Endpoint is only for non-AWS backends and switches to
// path-style addressing, the convention blobs and the platform's own
// s3compat layer follow.
type S3Config struct {
	Bucket    string
	Region    string
	Endpoint  string
	AccessKey string
	SecretKey string
	Prefix    string // default "records/", kept apart from blobs' "blobs/"
	HTTP      *http.Client
}

// S3Store is a Store over one bucket prefix.
type S3Store struct{ cfg S3Config }

// NewS3 validates cfg and returns the store.
func NewS3(cfg S3Config) (*S3Store, error) {
	if cfg.Bucket == "" || cfg.Region == "" {
		return nil, fmt.Errorf("objrec: S3 needs Bucket and Region (got %q, %q)", cfg.Bucket, cfg.Region)
	}
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("objrec: S3 needs credentials")
	}
	if cfg.Prefix == "" {
		cfg.Prefix = "records/"
	}
	// The prefix is joined to keys unescaped, so it must be whole
	// segments of the key alphabet: "records" (no slash) would silently
	// write "recordsusers/...", and ".." would leave the namespace.
	if err := ValidPrefix(cfg.Prefix); err != nil {
		return nil, err
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	return &S3Store{cfg}, nil
}

// S3FromEnv binds the platform's CARLOS_STORE_* variables. An unset
// bucket is the answer "this app has no store": (nil, nil), exactly as
// blobs.S3FromEnv, so callers fall back to Dir in dev without a special
// case. Checking err alone and using the nil store panics later.
func S3FromEnv() (*S3Store, error) {
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

func (s *S3Store) base() (string, string) {
	if s.cfg.Endpoint != "" {
		return strings.TrimSuffix(s.cfg.Endpoint, "/"), "/" + s.cfg.Bucket
	}
	return "https://" + s.cfg.Bucket + ".s3." + s.cfg.Region + ".amazonaws.com", ""
}

// do signs and sends one request. Keys are ValidKey-shaped, so the path
// needs no escaping. Conditional headers ride unsigned, which S3
// permits (only x-amz-* headers must be signed).
func (s *S3Store) do(ctx context.Context, method, path string, q url.Values, body []byte, hdr map[string]string) (*http.Response, error) {
	host, prefix := s.base()
	u := host + prefix + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	payload := sigv4.EmptyPayloadHash
	if len(body) > 0 {
		payload = sigv4.HexSHA256(body)
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	now := time.Now()
	req.Header.Set("x-amz-content-sha256", payload)
	req.Header.Set("x-amz-date", now.UTC().Format("20060102T150405Z"))
	sigv4.Sign(req, s.cfg.AccessKey, s.cfg.SecretKey, s.cfg.Region, "s3", now)
	return s.cfg.HTTP.Do(req)
}

func s3Err(op string, resp *http.Response) error {
	return s3ErrBody(op, resp, readErrBody(resp))
}

func readErrBody(resp *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return strings.TrimSpace(string(b))
}

func s3ErrBody(op string, resp *http.Response, body string) error {
	return fmt.Errorf("objrec: s3 %s: %s: %s", op, resp.Status, body)
}

// noSuchKey reports whether a 404 body is S3's NoSuchKey. Only that one
// means "no record": NoSuchBucket, or a 404 from a misaddressed
// endpoint, read as a missing record would send Mutate round its loop
// and bury the real fault.
func noSuchKey(body string) bool { return strings.Contains(body, "<Code>NoSuchKey</Code>") }

func (s *S3Store) Get(ctx context.Context, key string) ([]byte, Version, error) {
	if err := ValidKey(key); err != nil {
		return nil, "", err
	}
	resp, err := s.do(ctx, http.MethodGet, "/"+s.cfg.Prefix+key, nil, nil, nil)
	if err != nil {
		return nil, "", fmt.Errorf("objrec: s3 get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		body := readErrBody(resp)
		if noSuchKey(body) {
			return nil, "", ErrNotFound
		}
		return nil, "", s3ErrBody("get", resp, body)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", s3Err("get", resp)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, MaxRecord+1))
	if err != nil {
		return nil, "", err
	}
	if len(b) > MaxRecord {
		return nil, "", ErrTooLarge
	}
	return b, Version(resp.Header.Get("ETag")), nil
}

// conflictAttempts and conflictBackoff govern retries on 409. S3 sends
// 409 ConditionalRequestConflict while another conditional write to the
// same key is still in flight, and it does not say who won, so a 409 is
// "try again", not "you lost": read as a loss, two racers could both be
// told they lost with nothing written.
const (
	conflictAttempts = 5
	conflictBackoff  = 20 * time.Millisecond
)

// put maps S3's conditional answers. 412 is a failed condition, the
// only "you lost". 404 with NoSuchKey answers If-Match on a key that no
// longer exists; any other 404 is a fault. A 2xx must carry an ETag,
// because the next Update sends it as If-Match, and an empty If-Match
// is unconditional on some backends.
func (s *S3Store) put(ctx context.Context, key string, body []byte, hdr map[string]string, lost error) (Version, error) {
	if err := checkWrite(key, body); err != nil {
		return "", err
	}
	for attempt := 1; ; attempt++ {
		resp, err := s.do(ctx, http.MethodPut, "/"+s.cfg.Prefix+key, nil, body, hdr)
		if err != nil {
			return "", fmt.Errorf("objrec: s3 put %s: %w", key, err)
		}
		status := resp.StatusCode
		switch {
		case status/100 == 2:
			etag := resp.Header.Get("ETag")
			resp.Body.Close()
			if etag == "" {
				return "", fmt.Errorf("objrec: s3 put %s: %s with no ETag", key, resp.Status)
			}
			return Version(etag), nil
		case status == http.StatusPreconditionFailed:
			resp.Body.Close()
			return "", lost
		case status == http.StatusConflict:
			resp.Body.Close()
			if attempt >= conflictAttempts {
				return "", lost
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(attempt) * conflictBackoff):
			}
		case status == http.StatusNotFound && lost == ErrChanged:
			b := readErrBody(resp)
			resp.Body.Close()
			if noSuchKey(b) {
				return "", ErrNotFound
			}
			return "", s3ErrBody("put "+key, resp, b)
		default:
			err := s3Err("put "+key, resp)
			resp.Body.Close()
			return "", err
		}
	}
}

func (s *S3Store) Create(ctx context.Context, key string, body []byte) (Version, error) {
	return s.put(ctx, key, body, map[string]string{"If-None-Match": "*"}, ErrExists)
}

func (s *S3Store) Update(ctx context.Context, key string, body []byte, v Version) (Version, error) {
	return s.put(ctx, key, body, map[string]string{"If-Match": string(v)}, ErrChanged)
}

func (s *S3Store) Delete(ctx context.Context, key string) error {
	if err := ValidKey(key); err != nil {
		return err
	}
	resp, err := s.do(ctx, http.MethodDelete, "/"+s.cfg.Prefix+key, nil, nil, nil)
	if err != nil {
		return fmt.Errorf("objrec: s3 delete: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusNotFound {
		return s3Err("delete", resp)
	}
	return nil
}

type listResult struct {
	Contents []struct {
		Key string `xml:"Key"`
	} `xml:"Contents"`
	IsTruncated           bool   `xml:"IsTruncated"`
	NextContinuationToken string `xml:"NextContinuationToken"`
}

func (s *S3Store) List(ctx context.Context, prefix, cursor string, limit int) ([]string, string, error) {
	if err := ValidPrefix(prefix); err != nil {
		return nil, "", err
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	q := url.Values{"list-type": {"2"}, "prefix": {s.cfg.Prefix + prefix}, "max-keys": {strconv.Itoa(limit)}}
	if cursor != "" {
		q.Set("continuation-token", cursor)
	}
	resp, err := s.do(ctx, http.MethodGet, "/", q, nil, nil)
	if err != nil {
		return nil, "", fmt.Errorf("objrec: s3 list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", s3Err("list", resp)
	}
	var res listResult
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&res); err != nil {
		return nil, "", fmt.Errorf("objrec: s3 list: %w", err)
	}
	keys := make([]string, 0, len(res.Contents))
	for _, c := range res.Contents {
		keys = append(keys, strings.TrimPrefix(c.Key, s.cfg.Prefix))
	}
	next := ""
	if res.IsTruncated {
		next = res.NextContinuationToken
	}
	return keys, next, nil
}
