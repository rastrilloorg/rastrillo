package calibrate

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"sync"
	"time"
)

// Instruction is what Paul is asked on the page and Astra in its
// prompt, word for word.
const Instruction = "Two pieces of code do the same job in two " +
	"different apps. Which would you rather read to understand or " +
	"change? Choose Left, Right, No difference or Not the same job."

// NotSameJobHint follows Instruction, on the page and to Astra, and
// says when a pair is not the same job.
const NotSameJobHint = "The apps were built separately, so names, " +
	"fields, rules and messages will differ; that alone does not make " +
	"it a different job. Choose \"Not the same job\" only when one " +
	"side leaves out or adds a large part of the job."

// pageCopy is every other word the page shows, in one place to edit.
var pageCopy = struct {
	Title, Progress, LeftHeading, RightHeading string
	LeftBetter, RightBetter, Same, NotSameJob  string
	FinishedTitle, Finished                    string
}{
	Title:         "Which reads better?",
	Progress:      "Pair %d of %d",
	LeftHeading:   "Left",
	RightHeading:  "Right",
	LeftBetter:    "Left reads better",
	RightBetter:   "Right reads better",
	Same:          "No difference",
	NotSameJob:    "Not the same job",
	FinishedTitle: "Done",
	Finished:      "That is every pair. Thank you: you can close this page.",
}

// Verdict is which side of a pair reads better.
type Verdict string

// The verdicts. NotSameJob says the two sides do not do the same job
// (one covers more than the other): like Same, it picks no side.
const (
	LeftBetter  Verdict = "left"
	RightBetter Verdict = "right"
	Same        Verdict = "same"
	NotSameJob  Verdict = "not-same-job"
)

// Decided is whether the verdict picks a side.
func (verdict Verdict) Decided() bool {
	return verdict == LeftBetter || verdict == RightBetter
}

func (verdict Verdict) known() bool {
	return verdict.Decided() || verdict == Same || verdict == NotSameJob
}

// Judgement is one verdict on one pair. Canonical names the pair
// whichever side each excerpt was on, so that `bench pairs -judged`
// never offers it again.
type Judgement struct {
	PairID    int
	Canonical string
	Verdict   Verdict
	At        time.Time
}

// Calibration is results/calibration-<date>.json: the pairs file
// judged, the seed its pairs were chosen with, the order they are
// shown in, and each judgement as it was made.
type Calibration struct {
	Pairs      string
	Seed       int64
	Order      []int `json:",omitempty"`
	Judgements []Judgement
}

// DecidedChecksNeeded is how many check pairs must have a side: until
// they do, spares are shown, and no layer can be trusted with fewer.
const DecidedChecksNeeded = 12

// Serve shows the pairs on a page at address, one at a time: the tune
// and check pairs in a shuffled order, then spares only while fewer
// than 12 check pairs have a side. It writes out after each judgement,
// so a reload, or serving again, resumes where it stopped; out must
// hold a Calibration already, or not exist. It returns when every pair
// it needs is judged, or with an error when ctx is done first. It binds
// only to 127.0.0.1 or a Tailscale address.
func Serve(
	ctx context.Context, address string, pairs []Pair, out string,
) error {
	if err := CheckAddress(address); err != nil {
		return err
	}
	page, err := newSession(pairs, out)
	if err != nil {
		return err
	}
	if page.finished() {
		return nil
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: page,
		ReadHeaderTimeout: 10 * time.Second}
	failed := make(chan error, 1)
	go func() { failed <- server.Serve(listener) }()
	select {
	case <-page.done:
	case err := <-failed:
		return err
	case <-ctx.Done():
	}
	stopping, cancel := context.WithTimeout(context.Background(),
		5*time.Second)
	defer cancel()
	server.Shutdown(stopping)
	if ctx.Err() != nil && !page.finished() {
		return fmt.Errorf("stopped before every pair was judged (%w); "+
			"the judgements so far are in %s, and serving again resumes",
			ctx.Err(), out)
	}
	return nil
}

// tailscale is the address range Tailscale gives machines.
var tailscale = []*net.IPNet{mustNet("100.64.0.0/10"),
	mustNet("fd7a:115c:a1e0::/48")}

func mustNet(cidr string) *net.IPNet {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	return network
}

// CheckAddress refuses every address but 127.0.0.1 and this machine's
// Tailscale IP: an empty or unspecified host listens on every
// interface, the office network included, and a name could resolve to
// anything.
func CheckAddress(address string) error {
	refusal := fmt.Errorf("serve on 127.0.0.1 or a Tailscale IP, "+
		"as ip:port: %q could listen beyond them", address)
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return refusal
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() && !slices.ContainsFunc(tailscale,
		func(network *net.IPNet) bool { return network.Contains(ip) }) {
		return refusal
	}
	return nil
}

//go:embed page.html
var pageHTML string

// The page's two views: a pair to judge, and the end.
var (
	pageTemplate     = template.Must(template.New("page").Parse(pageHTML))
	judgingTemplate  = pageTemplate.Lookup("judging")
	finishedTemplate = pageTemplate.Lookup("finished")
)

// session is one person judging the pairs: the page Serve serves.
type session struct {
	lock        sync.Mutex
	pairs       map[int]Pair
	calibration Calibration
	out         string
	done        chan struct{}
	finishing   sync.Once
}

// newSession reads the calibration already in out, if any, and checks
// that it was made on these pairs. One with no order yet gets one: the
// tune and check pairs shuffled, then the spares shuffled.
func newSession(pairs []Pair, out string) (*session, error) {
	page := &session{pairs: map[int]Pair{}, out: out,
		done: make(chan struct{})}
	for _, pair := range pairs {
		page.pairs[pair.ID] = pair
	}
	raw, err := os.ReadFile(out)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &page.calibration); err != nil {
			return nil, fmt.Errorf("%s: %w", out, err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}
	for _, judged := range page.calibration.Judgements {
		if page.pairs[judged.PairID].Canonical != judged.Canonical {
			return nil, fmt.Errorf("%s judged pair %d as %q, which these "+
				"pairs do not have: it was made on other pairs",
				out, judged.PairID, judged.Canonical)
		}
	}
	order := page.calibration.Order
	if len(order) == 0 {
		page.calibration.Order = shuffledOrder(pairs)
		return page, page.save()
	}
	if len(order) != len(pairs) || slices.ContainsFunc(order,
		func(id int) bool { _, known := page.pairs[id]; return !known }) {
		return nil, fmt.Errorf("%s shows pairs in an order that is not "+
			"these pairs': it was made on other pairs", out)
	}
	return page, nil
}

func shuffledOrder(pairs []Pair) []int {
	var calibrating, spares []int
	for _, pair := range pairs {
		if pair.Set == Spare {
			spares = append(spares, pair.ID)
		} else {
			calibrating = append(calibrating, pair.ID)
		}
	}
	rand.Shuffle(len(calibrating), func(i, j int) {
		calibrating[i], calibrating[j] = calibrating[j], calibrating[i]
	})
	rand.Shuffle(len(spares), func(i, j int) {
		spares[i], spares[j] = spares[j], spares[i]
	})
	return append(calibrating, spares...)
}

// next is the pair to show: the first not judged in order, a spare
// only once every tune and check pair is judged and fewer than 12
// check pairs have a side. It is false when none is left to show.
func (page *session) next() (Pair, bool) {
	judged := map[int]bool{}
	decidedChecks := 0
	for _, judgement := range page.calibration.Judgements {
		judged[judgement.PairID] = true
		set := page.pairs[judgement.PairID].Set
		if (set == Check || set == Spare) && judgement.Verdict.Decided() {
			decidedChecks++
		}
	}
	for _, id := range page.calibration.Order {
		pair := page.pairs[id]
		if judged[id] {
			continue
		}
		if pair.Set != Spare || decidedChecks < DecidedChecksNeeded {
			return pair, true
		}
	}
	return Pair{}, false
}

func (page *session) finished() bool {
	page.lock.Lock()
	defer page.lock.Unlock()
	_, left := page.next()
	return !left
}

// shownCount is how many pairs the page counts: the tune and check
// pairs, and the spares shown so far, the one showing included.
func (page *session) shownCount(showing Pair) int {
	count := 0
	for _, pair := range page.pairs {
		if pair.Set != Spare {
			count++
		}
	}
	for _, judged := range page.calibration.Judgements {
		if page.pairs[judged.PairID].Set == Spare {
			count++
		}
	}
	if showing.Set == Spare {
		count++
	}
	return count
}

func (page *session) save() error {
	encoded, err := json.MarshalIndent(page.calibration, "", "  ")
	if err != nil {
		return err
	}
	temporary := page.out + ".writing"
	if err := os.WriteFile(temporary, append(encoded, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, page.out)
}

func (page *session) ServeHTTP(
	response http.ResponseWriter, request *http.Request,
) {
	if request.URL.Path != "/" {
		http.NotFound(response, request)
		return
	}
	switch request.Method {
	case http.MethodGet, http.MethodHead:
		page.show(response)
	case http.MethodPost:
		page.judge(response, request)
	default:
		response.Header().Set("Allow", "GET, POST")
		http.Error(response, "method not allowed",
			http.StatusMethodNotAllowed)
	}
}

// show renders the pair to judge, or the end, after which the session
// is done.
func (page *session) show(response http.ResponseWriter) {
	page.lock.Lock()
	pair, left := page.next()
	view := struct {
		Copy                        any
		Instruction, Hint, Progress string
		Step                        int
		Left, Right                 string
		LeftBetter, RightBetter     Verdict
		Same, NotSameJob            Verdict
	}{Copy: pageCopy, Instruction: Instruction, Hint: NotSameJobHint,
		Step: len(page.calibration.Judgements),
		Progress: fmt.Sprintf(pageCopy.Progress,
			len(page.calibration.Judgements)+1, page.shownCount(pair)),
		Left: pair.Left.Text, Right: pair.Right.Text,
		LeftBetter: LeftBetter, RightBetter: RightBetter, Same: Same,
		NotSameJob: NotSameJob}
	page.lock.Unlock()

	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	if !left {
		finishedTemplate.Execute(response, view)
		page.finishing.Do(func() { close(page.done) })
		return
	}
	judgingTemplate.Execute(response, view)
}

// judge records the verdict on the pair showing and goes back to the
// page. The form posts its step, how many pairs were judged before it,
// never the pair: IDs are numbered tune, check, spare, so an ID would
// tell the set. A verdict from any other step, a stale page or a
// second click, is dropped: the page shows what is next either way.
func (page *session) judge(
	response http.ResponseWriter, request *http.Request,
) {
	request.Body = http.MaxBytesReader(response, request.Body, 1<<10)
	if err := request.ParseForm(); err != nil {
		http.Error(response, "unreadable form", http.StatusBadRequest)
		return
	}
	verdict := Verdict(request.PostForm.Get("verdict"))
	step, err := strconv.Atoi(request.PostForm.Get("step"))
	if err != nil || !verdict.known() {
		http.Error(response, "unknown pair or verdict", http.StatusBadRequest)
		return
	}
	page.lock.Lock()
	defer page.lock.Unlock()
	pair, left := page.next()
	if left && step == len(page.calibration.Judgements) {
		judgements := page.calibration.Judgements
		page.calibration.Judgements = append(judgements, Judgement{
			PairID: pair.ID, Canonical: pair.Canonical, Verdict: verdict,
			At: time.Now().UTC()})
		if err := page.save(); err != nil {
			page.calibration.Judgements = judgements
			http.Error(response, "could not save: "+err.Error(),
				http.StatusInternalServerError)
			return
		}
	}
	http.Redirect(response, request, "/", http.StatusSeeOther)
}
