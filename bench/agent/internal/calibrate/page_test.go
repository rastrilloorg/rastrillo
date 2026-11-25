package calibrate

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// judgingPairs is tune, check and spare pairs whose sides say which
// pair they are in their text, and whose run, job and composite are
// unlike anything else on the page.
func judgingPairs(tune, check, spare int) []Pair {
	var pairs []Pair
	for _, set := range []struct {
		name  string
		count int
	}{{Tune, tune}, {Check, check}, {Spare, spare}} {
		for range set.count {
			id := len(pairs) + 1
			side := func(which string) Excerpt {
				return Excerpt{
					Run:   fmt.Sprintf("zebra-round/groomer-carlos-%s%d", which, id),
					Job:   "trimming the zebra's mane",
					Text:  fmt.Sprintf("func %s%d() {\n\treturn \"<b>\"\n}\n", which, id),
					Funcs: excerptFuncs([5]int{4711, 0, 0, 0, 0}),
				}
			}
			left, right := side("Left"), side("Right")
			pairs = append(pairs, Pair{ID: id, Canonical: canonical(left, right),
				Job: left.Job, Left: left, Right: right, Set: set.name})
		}
	}
	return pairs
}

func startedCalibration(t *testing.T, seed int64) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "calibration.json")
	encoded, err := json.Marshal(Calibration{Pairs: "pairs.json", Seed: seed})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	return out
}

func get(t *testing.T, page http.Handler) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	page.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET / = %d: %s", recorder.Code, recorder.Body)
	}
	return recorder.Body.String()
}

// post answers the pair showing, as its page's form does.
func post(t *testing.T, page *session, verdict Verdict) {
	t.Helper()
	page.lock.Lock()
	step := len(page.calibration.Judgements)
	page.lock.Unlock()
	postStep(t, page, step, verdict)
}

func postStep(t *testing.T, page http.Handler, step int, verdict Verdict) {
	t.Helper()
	form := url.Values{"step": {fmt.Sprint(step)},
		"verdict": {string(verdict)}}
	request := httptest.NewRequest(http.MethodPost, "/",
		strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	page.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("POST step %d %s = %d: %s", step, verdict, recorder.Code,
			recorder.Body)
	}
}

// showing is the pair the page asks about, or 0 when it asks nothing.
func showing(t *testing.T, page *session) int {
	t.Helper()
	get(t, page)
	page.lock.Lock()
	defer page.lock.Unlock()
	pair, left := page.next()
	if !left {
		return 0
	}
	return pair.ID
}

func readCalibration(t *testing.T, out string) Calibration {
	t.Helper()
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var calibration Calibration
	if err := json.Unmarshal(raw, &calibration); err != nil {
		t.Fatal(err)
	}
	return calibration
}

func TestThePageShowsOnlyTheTwoExcerptsAndFourButtons(t *testing.T) {
	pairs := judgingPairs(2, 2, 1)
	page, err := newSession(pairs, startedCalibration(t, 7))
	if err != nil {
		t.Fatal(err)
	}

	body := get(t, page)

	id := showing(t, page)
	if id == 0 {
		t.Fatalf("the page shows no pair:\n%s", body)
	}
	pair := pairs[id-1]
	for _, want := range []string{
		html.EscapeString(pair.Left.Text), html.EscapeString(pair.Right.Text),
		html.EscapeString(Instruction), html.EscapeString(NotSameJobHint),
		"Left reads better", "Right reads better",
		"No difference", "Not the same job", "Pair 1 of 4",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q:\n%s", want, body)
		}
	}
	if strings.Index(body, html.EscapeString(pair.Left.Text)) >
		strings.Index(body, html.EscapeString(pair.Right.Text)) {
		t.Error("the left excerpt comes after the right one")
	}
	if buttons := strings.Count(body, "<button"); buttons != 4 {
		t.Errorf("%d buttons, want 4", buttons)
	}
	for _, hidden := range []string{"zebra-round", "groomer-carlos",
		"trimming", "4711", "carlos", Tune, Check, Spare} {
		if strings.Contains(body, hidden) {
			t.Errorf("the page shows %q", hidden)
		}
	}
	if !strings.Contains(body, `name="step" value="0"`) {
		t.Error("the form does not post the step")
	}
	// Pair IDs run tune, check, spare: an ID would give the set away.
	for _, pair := range pairs {
		for _, leak := range []string{fmt.Sprintf(`value="%d"`, pair.ID),
			pair.Canonical, html.EscapeString(pair.Canonical)} {
			if strings.Contains(body, leak) {
				t.Errorf("the page shows pair %d's %q", pair.ID, leak)
			}
		}
	}
	if strings.Contains(body, `name="pair"`) {
		t.Error("the form posts the pair")
	}
	if strings.Contains(body, "<script") {
		t.Error("the page has a script")
	}
}

func TestAJudgementIsWrittenAtOnce(t *testing.T) {
	pairs := judgingPairs(3, 3, 0)
	out := startedCalibration(t, 42)
	page, err := newSession(pairs, out)
	if err != nil {
		t.Fatal(err)
	}
	first := showing(t, page)

	post(t, page, LeftBetter)

	saved := readCalibration(t, out)
	if saved.Seed != 42 || saved.Pairs != "pairs.json" {
		t.Errorf("the seed or pairs file was lost: %+v", saved)
	}
	if len(saved.Judgements) != 1 {
		t.Fatalf("judgements = %+v, want one", saved.Judgements)
	}
	judged := saved.Judgements[0]
	if judged.PairID != first || judged.Verdict != LeftBetter ||
		judged.Canonical != pairs[first-1].Canonical || judged.At.IsZero() {
		t.Errorf("judgement = %+v, want pair %d left with its Canonical",
			judged, first)
	}

	// A reload, even by a new process, resumes: the next pair, in the
	// order first shuffled.
	second := showing(t, page)
	resumed, err := newSession(pairs, out)
	if err != nil {
		t.Fatal(err)
	}
	if again := showing(t, resumed); again != second || again == first {
		t.Errorf("resumed on pair %d, want %d (not %d)", again, second, first)
	}
	if !strings.Contains(get(t, resumed), "Pair 2 of 6") {
		t.Error("the resumed page does not count the judged pair")
	}

	// A second answer to a pair already judged, from a stale page or a
	// double click, changes nothing.
	postStep(t, resumed, 0, RightBetter)
	if judgements := readCalibration(t, out).Judgements; len(judgements) != 1 {
		t.Errorf("a stale post was recorded: %+v", judgements)
	}
}

func TestANotSameJobVerdictIsRecorded(t *testing.T) {
	pairs := judgingPairs(1, 0, 0)
	out := startedCalibration(t, 1)
	page, err := newSession(pairs, out)
	if err != nil {
		t.Fatal(err)
	}

	post(t, page, NotSameJob)

	judged := readCalibration(t, out).Judgements
	if len(judged) != 1 || judged[0].Verdict != NotSameJob {
		t.Errorf("judgements = %+v, want one not-same-job", judged)
	}
	if NotSameJob.Decided() || Same.Decided() || !LeftBetter.Decided() {
		t.Error("only left and right are decided")
	}
}

func TestAnUnknownVerdictIsRefused(t *testing.T) {
	page, err := newSession(judgingPairs(1, 0, 0),
		startedCalibration(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"step": {"0"}, "verdict": {"both"}}
	request := httptest.NewRequest(http.MethodPost, "/",
		strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()

	page.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("an unknown verdict got %d", recorder.Code)
	}
}

func TestSparesAreShownUntilTwelveCheckPairsHaveASide(t *testing.T) {
	pairs := judgingPairs(2, 13, 3)
	out := startedCalibration(t, 1)
	page, err := newSession(pairs, out)
	if err != nil {
		t.Fatal(err)
	}
	undecided := 0
	for range 15 {
		id := showing(t, page)
		if pairs[id-1].Set == Spare {
			t.Fatalf("spare %d shown before every tune and check pair", id)
		}
		verdict := LeftBetter
		if pairs[id-1].Set == Check && undecided < 2 {
			verdict = []Verdict{Same, NotSameJob}[undecided]
			undecided++
		}
		post(t, page, verdict)
	}

	// 11 check pairs have a side: spares, one at a time, until 12 do.
	spare := showing(t, page)
	if spare == 0 || pairs[spare-1].Set != Spare {
		t.Fatalf("showing %d, want a spare", spare)
	}
	post(t, page, Same)
	another := showing(t, page)
	if another == 0 || another == spare || pairs[another-1].Set != Spare {
		t.Fatalf("showing %d, want another spare", another)
	}
	post(t, page, RightBetter)

	if left := showing(t, page); left != 0 {
		t.Errorf("showing pair %d after 12 check pairs had a side", left)
	}
	if body := get(t, page); !strings.Contains(body, "every pair") {
		t.Errorf("the finished page does not say so:\n%s", body)
	}
	if judged := readCalibration(t, out).Judgements; len(judged) != 17 {
		t.Errorf("%d judgements, want 17", len(judged))
	}
}

func TestNoSpareIsShownWhenTwelveCheckPairsHaveASide(t *testing.T) {
	pairs := judgingPairs(1, 12, 2)
	page, err := newSession(pairs, startedCalibration(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	for range 13 {
		post(t, page, RightBetter)
	}
	if id := showing(t, page); id != 0 {
		t.Errorf("showing pair %d (%s)", id, pairs[id-1].Set)
	}
}

func TestACalibrationOfOtherPairsIsRefused(t *testing.T) {
	out := startedCalibration(t, 1)
	page, err := newSession(judgingPairs(2, 0, 0), out)
	if err != nil {
		t.Fatal(err)
	}
	post(t, page, LeftBetter)

	other := judgingPairs(2, 0, 0)
	other[0].Canonical, other[1].Canonical = "a | b", "c | d"
	if _, err := newSession(other, out); err == nil {
		t.Error("a calibration made on other pairs was resumed")
	}
}

func TestServeRefusesAnAllInterfacesAddress(t *testing.T) {
	out := startedCalibration(t, 1)
	for _, address := range []string{":8790", "0.0.0.0:8790", "[::]:8790",
		"192.168.1.20:8790", "localhost:8790", "8790"} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := Serve(ctx, address, judgingPairs(1, 0, 0), out)
		cancel()
		if err == nil || !strings.Contains(err.Error(), "127.0.0.1") {
			t.Errorf("Serve(%q) = %v, want a refusal naming 127.0.0.1",
				address, err)
		}
	}
}

func TestServeStopsWhenItsContextIsDone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(),
		200*time.Millisecond)
	defer cancel()
	err := Serve(ctx, "127.0.0.1:0", judgingPairs(1, 0, 0),
		startedCalibration(t, 1))
	if err == nil {
		t.Error("an interrupted Serve reported success")
	}
}

func TestServeReturnsWhenEveryPairIsJudged(t *testing.T) {
	pairs := judgingPairs(1, 0, 0)
	out := startedCalibration(t, 1)
	page, err := newSession(pairs, out)
	if err != nil {
		t.Fatal(err)
	}
	post(t, page, Same)

	done := make(chan error, 1)
	go func() {
		done <- Serve(context.Background(), "127.0.0.1:0", pairs, out)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve waited with nothing left to judge")
	}
}

func TestServeStopsAfterShowingTheFinishedPage(t *testing.T) {
	pairs := judgingPairs(1, 0, 0)
	out := startedCalibration(t, 1)
	page, err := newSession(pairs, out)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(page)
	defer server.Close()
	client := server.Client()

	response, err := client.PostForm(server.URL,
		url.Values{"step": {"0"}, "verdict": {"left"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()

	if !strings.Contains(string(body), "every pair") {
		t.Errorf("after the last pair the page says:\n%s", body)
	}
	select {
	case <-page.done:
	case <-time.After(time.Second):
		t.Error("the session is not done after showing the finished page")
	}
}
