// Package cldrsrc fetches and reads the pinned cldr-json files the
// generator and the harness use.
//
// The CLDR files are fetched one by one from the tagged cldr-json tree
// rather than as the release zip: the zip carries every CLDR package
// (about 100 MB) and these files are about 50 MB. The hash manifest
// sources/inputs.sha256 covers them and the ISO 4217 editions, and is
// what makes a run reproducible. A file whose bytes differ
// from the manifest stops the run, because a silently different input
// would make every number this harness reports unrepeatable.
package cldrsrc

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Version is the cldr-json tag every input comes from.
const Version = "48.0.0"

const base = "https://raw.githubusercontent.com/unicode-org/cldr-json/" + Version + "/cldr-json/"

// Core is the supplemental and index data, independent of locale.
var Core = []string{
	"cldr-core/availableLocales.json",
	"cldr-core/coverageLevels.json",
	"cldr-core/supplemental/aliases.json",
	"cldr-core/supplemental/currencyData.json",
	"cldr-core/supplemental/likelySubtags.json",
	"cldr-core/supplemental/numberingSystems.json",
	"cldr-core/supplemental/parentLocales.json",
}

// ISOEditions are archived editions of ISO 4217 list one, the only
// published source of each code's minor units. The current list drops a
// code when it is withdrawn, and the historic list (list three) carries
// no minor units at all, so a withdrawn code's exponent is verified
// against the last archived edition that still listed it. The Wayback
// Machine's id_ URLs return the archived bytes unchanged, which is what
// lets the hash manifest pin them.
var ISOEditions = []struct{ Timestamp, URL string }{
	{"20150810014414", "www.currency-iso.org/dam/downloads/lists/list_one.xml"},
	{"20160308100106", "www.currency-iso.org/dam/downloads/lists/list_one.xml"},
	{"20160706233149", "www.currency-iso.org/dam/downloads/lists/list_one.xml"},
	{"20170422215735", "www.currency-iso.org/dam/downloads/lists/list_one.xml"},
	{"20170719175725", "www.currency-iso.org/dam/downloads/lists/list_one.xml"},
	{"20180511154730", "www.currency-iso.org/dam/downloads/lists/list_one.xml"},
	{"20190226050204", "www.currency-iso.org/dam/downloads/lists/list_one.xml"},
	{"20200309154814", "www.currency-iso.org/dam/downloads/lists/list_one.xml"},
	{"20200520104332", "www.currency-iso.org/dam/downloads/lists/list_one.xml"},
	{"20200725143441", "www.currency-iso.org/dam/downloads/lists/list_one.xml"},
	{"20200805105057", "www.currency-iso.org/dam/downloads/lists/list_one.xml"},
	{"20201102111603", "www.currency-iso.org/dam/downloads/lists/list_one.xml"},
	{"20201217070440", "www.currency-iso.org/dam/downloads/lists/list_one.xml"},
	{"20221007084802", "www.six-group.com/dam/download/financial-information/data-center/iso-currrency/lists/list-one.xml"},
	{"20221008231436", "www.six-group.com/dam/download/financial-information/data-center/iso-currrency/lists/list-one.xml"},
	{"20230209054101", "www.six-group.com/dam/download/financial-information/data-center/iso-currrency/lists/list-one.xml"},
	{"20240722165054", "www.six-group.com/dam/download/financial-information/data-center/iso-currrency/lists/list-one.xml"},
	{"20241007232434", "www.six-group.com/dam/download/financial-information/data-center/iso-currrency/lists/list-one.xml"},
	{"20241016034252", "www.six-group.com/dam/download/financial-information/data-center/iso-currrency/lists/list-one.xml"},
	{"20250217165800", "www.six-group.com/dam/download/financial-information/data-center/iso-currrency/lists/list-one.xml"},
	{"20250224211816", "www.six-group.com/dam/download/financial-information/data-center/iso-currrency/lists/list-one.xml"},
	{"20250401010757", "www.six-group.com/dam/download/financial-information/data-center/iso-currrency/lists/list-one.xml"},
	{"20250512062221", "www.six-group.com/dam/download/financial-information/data-center/iso-currrency/lists/list-one.xml"},
	{"20250618204402", "www.six-group.com/dam/download/financial-information/data-center/iso-currrency/lists/list-one.xml"},
	{"20260130033309", "www.six-group.com/dam/download/financial-information/data-center/iso-currrency/lists/list-one.xml"},
	{"20260929233225", "www.six-group.com/dam/download/financial-information/data-center/iso-currrency/lists/list-one.xml"},
	{"20130730045120", "www.currency-iso.org/dam/downloads/table_a1.xml"},
	{"20130831210042", "www.currency-iso.org/dam/downloads/table_a1.xml"},
	{"20131205163407", "www.currency-iso.org/dam/downloads/table_a1.xml"},
	{"20140108132312", "www.currency-iso.org/dam/downloads/table_a1.xml"},
	{"20140407090907", "www.currency-iso.org/dam/downloads/table_a1.xml"},
	{"20140819141304", "www.currency-iso.org/dam/downloads/table_a1.xml"},
	{"20150106151146", "www.currency-iso.org/dam/downloads/table_a1.xml"},
	{"20150706053109", "www.currency-iso.org/dam/downloads/table_a1.xml"},
	{"20130422110916", "www.currency-iso.org/dam/downloads/dl_iso_table_a1.xml"},
}

// ISOPath is where an edition is stored under the cache directory.
func ISOPath(timestamp string) string { return "iso4217/" + timestamp + ".xml" }

// LocaleFiles are the two per-locale files the table reads.
func LocaleFiles(locale string) []string {
	return []string{
		"cldr-numbers-full/main/" + locale + "/numbers.json",
		"cldr-numbers-full/main/" + locale + "/currencies.json",
	}
}

// DefaultDir is where fetched files live unless -dir says otherwise.
func DefaultDir() string {
	d, err := os.UserCacheDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "rastrillo-cldr", Version)
}

// Fetch downloads every file the table needs into dir, skipping files
// already present, and returns the relative paths fetched or found.
func Fetch(dir string) ([]string, error) {
	if err := fetchAll(dir, Core); err != nil {
		return nil, err
	}
	var avail struct {
		AvailableLocales struct {
			Full []string `json:"full"`
		} `json:"availableLocales"`
	}
	if err := Read(dir, "cldr-core/availableLocales.json", &avail); err != nil {
		return nil, err
	}
	files := append([]string(nil), Core...)
	for _, l := range avail.AvailableLocales.Full {
		files = append(files, LocaleFiles(l)...)
	}
	if err := fetchAll(dir, files); err != nil {
		return nil, err
	}
	for _, e := range ISOEditions {
		dst := filepath.Join(dir, ISOPath(e.Timestamp))
		files = append(files, ISOPath(e.Timestamp))
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if err := fetchEdition(e.Timestamp, e.URL, dst); err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	return files, nil
}

func fetchAll(dir string, files []string) error {
	client := &http.Client{Timeout: 60 * time.Second}
	sem := make(chan struct{}, 16)
	var mu sync.Mutex
	var firstErr error
	var wg sync.WaitGroup
	for _, f := range files {
		dst := filepath.Join(dir, f)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(f, dst string) {
			defer wg.Done()
			defer func() { <-sem }()
			err := fetchOne(client, base+f, dst)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}(f, dst)
	}
	wg.Wait()
	return firstErr
}

// fetchEdition gets one archived ISO edition, exactly the capture its
// timestamp names. The Wayback Machine answers a capture it cannot serve
// under one scheme by redirecting to a different capture, so a redirect
// that changes the timestamp is refused, and the other scheme is tried.
// Following such a redirect silently fetched the wrong edition once;
// the hash manifest caught it.
func fetchEdition(timestamp, url, dst string) error {
	marker := "/web/" + timestamp + "id_/"
	client := &http.Client{
		Timeout: 120 * time.Second,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if !strings.Contains(req.URL.Path, marker) {
				return fmt.Errorf("redirected to another capture: %s", req.URL)
			}
			return nil
		},
	}
	var errs []string
	for _, scheme := range []string{"https://", "http://"} {
		err := fetchOne(client, "https://web.archive.org"+marker+scheme+url, dst)
		if err == nil {
			return nil
		}
		errs = append(errs, err.Error())
	}
	return fmt.Errorf("ISO edition %s: %s", timestamp, strings.Join(errs, "; "))
}

func fetchOne(client *http.Client, url, dst string) error {
	// The Wayback Machine answers 498 and 429 to bursts and to clients
	// without a User-Agent; a short backoff is enough for 35 small files.
	var resp *http.Response
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "rastrillo-moneycldr (+https://amadan.net/rastrillo/rastrillo)")
		resp, err = client.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusOK {
			break
		}
		resp.Body.Close()
		if attempt == 4 {
			return fmt.Errorf("%s: %s", url, resp.Status)
		}
		time.Sleep(time.Duration(2<<attempt) * time.Second)
	}
	defer resp.Body.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// Manifest returns "sha256  path" lines for files, sorted by path.
func Manifest(dir string, files []string) ([]byte, error) {
	var b strings.Builder
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), f)
	}
	return []byte(b.String()), nil
}

// Verify checks dir against a manifest written by Manifest.
func Verify(dir, manifestPath string) ([]string, error) {
	f, err := os.Open(manifestPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var files []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		sum, path, ok := strings.Cut(sc.Text(), "  ")
		if !ok {
			return nil, fmt.Errorf("%s: bad line %q", manifestPath, sc.Text())
		}
		data, err := os.ReadFile(filepath.Join(dir, path))
		if err != nil {
			return nil, err
		}
		got := sha256.Sum256(data)
		if hex.EncodeToString(got[:]) != sum {
			return nil, fmt.Errorf("%s: hash differs from %s", path, manifestPath)
		}
		files = append(files, path)
	}
	return files, sc.Err()
}

// Read decodes one fetched JSON file.
func Read(dir, rel string, v any) error {
	data, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
