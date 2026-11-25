package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/brief"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/classify"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/cleanenv"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/measure"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/round"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/runner"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/source"
)

const carlokuAccount = `the Carloku account "demo" (short id bdf)`

// goplsArmNote is the carlos-gopls arm's definition, in the round's
// header.
const goplsArmNote = "carlos-gopls is carlos with gopls over MCP, set up " +
	"as gopls's own docs set it up: `gopls mcp` runs behind the bench's " +
	"goplsmcp wrapper, which starts it again in the app's module once " +
	"go.mod appears (Claude Code's MCP never lets gopls find a module " +
	"made after it starts), and the arm's system prompt ends with " +
	"`gopls mcp -instructions` after the environment notes. The prompt " +
	"itself is carlos's, word for word."

type job struct {
	brief brief.Brief
	arm   brief.Arm
	n     int
}

func runRound(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("run", flag.ExitOnError)
	label := flags.String("round", "", "the round's label, e.g. 1")
	briefList := flags.String("briefs", "groomer", "comma-separated briefs")
	armList := flags.String("arms", "carlos,control", "comma-separated arms")
	runs := flags.Int("n", 3, "runs per arm per brief")
	rastrillo := flags.String("rastrillo", "latest", `"latest", or a commit to serve as an unreleased version`)
	listenIP := flags.String("listen-ip", "", "address the agent's sandbox can reach, for a commit: $(tailscale ip -4)")
	platform := flags.String("platform", "", "the control's platform, from the poll")
	controlAccount := flags.String("control-account", "", "the control's deploy account, as the env notes should name it")
	controlHosts := flags.String("control-hosts", "", "comma-separated hosts the control's sandbox may reach")
	reuseControl := flags.String("reuse-control", "", "reuse this round's control runs")
	poll := flags.String("poll", "", "the poll file the platform came from")
	parallel := flags.Int("j", 2, "runs at once")
	model := flags.String("model", "claude-opus-5-5", "the model both arms use")
	claude := flags.String("claude", "claude", "the claude binary")
	chrome := flags.String("chrome", defaultChrome(), "Chromium, for measuring")
	lighthouse := flags.String("lighthouse", defaultLighthouse(), "the lighthouse binary")
	timeout := flags.Duration("timeout", time.Hour, "per prompt")
	carlokuTokenFile := flags.String("carloku-token-file", "",
		"a service credential for the carlos arm; none by default, since one cannot enable instances")
	carlokuConsole := flags.String("carloku-console", "https://console.carloku.com",
		"the console that credential belongs to")
	goplsFlag := flags.String("gopls", "",
		"the gopls binary the carlos-gopls arm's MCP server runs; needed with that arm")
	tokenFile := flags.String("token-file", defaultTokenFile(), "the token from claude setup-token, if CLAUDE_CODE_OAUTH_TOKEN is not set")
	flags.Parse(args)
	authToken, err := token(*tokenFile)
	if err != nil {
		return err
	}

	if err := checkLabel(*label); err != nil {
		return err
	}
	arms := armsOf(*armList)
	carlokuToken, err := carlokuCredential(*carlokuTokenFile)
	if err != nil {
		return err
	}
	if (contains(arms, brief.Control) || contains(arms, brief.Spec)) &&
		(*platform == "" || *controlAccount == "") {
		return errors.New("the control and spec arms need -platform and -control-account")
	}
	root, err := benchRoot()
	if err != nil {
		return err
	}
	cache, err := runsRoot()
	if err != nil {
		return err
	}
	var roundGopls string
	if previous, err := loadRound(root, *label); err == nil {
		roundGopls = previous.Gopls
	}
	gopls, err := goplsFor(arms, *goplsFlag, roundGopls)
	if err != nil {
		return err
	}
	version, err := claudeVersion(*claude)
	if err != nil {
		return err
	}
	briefs, err := pickBriefs(root, *briefList)
	if err != nil {
		return err
	}
	axeJS, err := os.ReadFile(filepath.Join(root, "../../ui/testdata/axe/axe.min.js"))
	if err != nil {
		return err
	}

	rastrilloSource := source.Latest()
	if *rastrillo != "latest" {
		if *listenIP == "" {
			return errors.New("a commit needs -listen-ip")
		}
		proxy := filepath.Join(cache, *label, "proxy")
		var stop func()
		rastrilloSource, stop, err = source.Local(
			filepath.Join(root, "../.."), *rastrillo, proxy, *listenIP)
		if err != nil {
			return err
		}
		defer stop()
	}

	unlock, err := lockRound(root, *label)
	if err != nil {
		return err
	}
	defer unlock()
	carlosAuth := "operator"
	if carlokuToken != "" {
		carlosAuth = "service " + *carlokuConsole
	}
	settings := map[string]string{
		"carlos-auth": carlosAuth,
		"rastrillo":   rastrilloSource.Label, "platform": *platform,
		"control-account": *controlAccount, "control-hosts": *controlHosts,
		"timeout": timeout.String(), "model": *model, "claude": version,
	}
	var goplsWrapper, goplsInstructions string
	if gopls.version != "" {
		// Only when gopls runs, so a round without it keeps the
		// fingerprint it always had.
		settings["gopls"] = gopls.version
		settings["gopls-instructions"] = gopls.instructions
		if settings["gopls-proxy"], err = sourceHash(root, goplsProxySource...); err != nil {
			return err
		}
		if goplsWrapper, err = buildGoplsProxy(root,
			filepath.Join(cache, *label, "bin")); err != nil {
			return err
		}
		if goplsInstructions, err = pinGoplsInstructions(
			filepath.Join(cache, *label), gopls.instructions); err != nil {
			return err
		}
	}
	if contains(arms, brief.Spec) {
		// The spec arm's requirements are a live page; pin what it said
		// when the round began, so a revised spec is a new round.
		specHash, err := pinSpec(filepath.Join(cache, *label))
		if err != nil {
			return err
		}
		settings[specURL] = specHash
	}
	config, controlConfig, err := fingerprints(root, briefs, settings)
	if err != nil {
		return err
	}

	r, err := loadRound(root, *label)
	if errors.Is(err, os.ErrNotExist) {
		r = round.Round{
			Label: *label, Date: time.Now().UTC().Format("2006-01-02"),
			Model: *model, ClaudeVersion: version,
			Rastrillo:            describe(rastrilloSource),
			Gopls:                gopls.version,
			GoplsInstructions:    goplsInstructions,
			Platform:             *platform,
			Poll:                 *poll,
			GettingStartedCommit: gettingStartedCommit(),
			PolicyHash:           policyHash(),
			Config:               config,
			ControlConfig:        controlConfig,
		}
	} else if err != nil {
		return err
	}
	if gopls.version != "" && len(r.Runs) == 0 && !slices.Contains(r.Notes, goplsArmNote) {
		r.Notes = append(r.Notes, goplsArmNote)
	}
	if r.Config != config {
		return fmt.Errorf("round %s was started with a different Rastrillo, "+
			"gopls, framing, brief, platform or claude: start a new round", r.Label)
	}
	if *reuseControl != "" {
		if err := reuseControlRuns(root, &r, *reuseControl, briefs, *runs,
			settings); err != nil {
			return err
		}
		arms = without(arms, brief.Control)
	}

	byName := map[string]brief.Brief{}
	var names []string
	for _, b := range briefs {
		byName[b.Name] = b
		names = append(names, b.Name)
	}
	r.Schedule = extendSchedule(r.Schedule, names, arms, *runs)
	var jobs []job
	for _, slot := range pendingJobs(r, names, arms, *runs) {
		jobs = append(jobs, job{byName[slot.Brief], slot.Arm, slot.N})
	}
	if err := saveRound(root, r); err != nil {
		return err
	}
	fmt.Printf("round %s: %d runs to do, %d at a time\n", r.Label, len(jobs), *parallel)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mutex sync.Mutex
	var firstErr error
	queue := make(chan job)
	var workers sync.WaitGroup
	for range *parallel {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := range queue {
				spec := runner.Spec{
					Round: r.Label, Model: *model, BenchRoot: root,
					Claude: *claude, ClaudeVersion: version,
					Brief: j.brief, Arm: j.arm, N: j.n,
					Source:    rastrilloSource,
					Measure:   measure.Options{Chrome: *chrome, Lighthouse: *lighthouse, AxeJS: axeJS},
					Timeout:   *timeout,
					Token:     authToken,
					TokenFile: *tokenFile,
					Dir: filepath.Join(cache, r.Label,
						fmt.Sprintf("%s-%s-%d", j.brief.Name, j.arm, j.n)),
				}
				spec.Vars = brief.Vars{
					App:      appName(j.brief.Name, r.Label, j.arm, j.n, 0),
					SkillURL: rastrilloSource.SkillURL,
				}
				deploy := deployVars(j.arm, *platform, *controlAccount, *controlHosts)
				deploy.withCarloku(carlokuToken, *carlokuConsole)
				spec.Vars.Platform = deploy.platform
				spec.Vars.DeployAccount = deploy.account
				spec.ExtraHosts = deploy.hosts
				spec.DeployEnv = deploy.env
				if j.arm == brief.CarlosGopls {
					spec.MCPConfig = goplsServer(goplsWrapper, gopls.path,
						filepath.Join(spec.Dir, "app"),
						filepath.Join(spec.Dir, "rec", "goplsmcp.log"),
						rastrilloSource.GoEnv)
					spec.SystemPromptExtra = gopls.instructions
					spec.MCPLog = filepath.Join(spec.Dir, "rec", "goplsmcp.log")
				}
				if *carlokuTokenFile != "" {
					spec.DeniedFiles = []string{*carlokuTokenFile}
				}
				var record round.RunRecord
				err := error(nil)
				if j.arm == brief.Spec {
					err = specStillMatches(specURL, settings[specURL])
				}
				if err == nil {
					record, err = runWithRetries(ctx, spec)
				}
				mutex.Lock()
				// Any run that stops the harness stops the round: the
				// next run would meet the same API limit, bad config or
				// full disk, and spend money finding out.
				if err == nil {
					r.Runs = append(r.Runs, record)
					sortRuns(r.Runs)
					err = saveRound(root, r)
				}
				if err != nil {
					if firstErr == nil {
						firstErr = err
					}
					cancel()
				}
				fmt.Printf("%s %s %d: %s\n", j.brief.Name, j.arm, j.n, outcomeLine(record, err))
				mutex.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		select {
		case queue <- j:
		case <-ctx.Done():
		}
	}
	close(queue)
	workers.Wait()
	return firstErr
}

// runWithRetries reruns a harness failure twice, keeping each failed
// attempt's directory beside the next for a person to read. An unclean
// config is not retried: the next run would be just as unclean.
func runWithRetries(ctx context.Context, spec runner.Spec) (round.RunRecord, error) {
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		if ctx.Err() != nil {
			return round.RunRecord{}, ctx.Err()
		}
		earlier, asideErr := setAside(spec.Dir)
		if asideErr != nil {
			return round.RunRecord{}, asideErr
		}
		spec.Vars.App = appName(spec.Brief.Name, spec.Round, spec.Arm, spec.N,
			earlier)
		var outcome runner.Outcome
		outcome, err = runner.Run(ctx, spec)
		if err == nil {
			return recordOf(spec, outcome), nil
		}
		if errors.Is(err, runner.ErrUnclean) || !errors.Is(err, runner.ErrHarness) {
			return round.RunRecord{}, err
		}
	}
	return round.RunRecord{}, err
}

const specURL = "https://spec.carlosframework.com"

// pinSpec saves the specification as it reads now beside the round's
// runs, and returns its sha256, which goes into the round's fingerprint.
func pinSpec(dir string) (string, error) {
	page, err := fetchSpecPage(specURL)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	sum := fmt.Sprintf("%x", sha256.Sum256(page))
	name := filepath.Join(dir, "spec-"+sum[:12]+".html")
	return sum, os.WriteFile(name, page, 0o644)
}

// specClient bounds the fetch: a stalled server would otherwise hold
// the round's lock for ever.
var specClient = &http.Client{Timeout: 30 * time.Second}

func fetchSpecPage(address string) ([]byte, error) {
	response, err := specClient.Get(address)
	if err != nil {
		return nil, fmt.Errorf("reading the spec: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("reading the spec: %s", response.Status)
	}
	return io.ReadAll(io.LimitReader(response.Body, 5<<20))
}

func fetchSpec(address string) (string, error) {
	page, err := fetchSpecPage(address)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(page)), nil
}

// specStillMatches stops the round when the live spec no longer reads
// as it did at the start: the agents fetch it live, so a revised spec
// would hand later runs different requirements under one fingerprint.
func specStillMatches(address, pinned string) error {
	now, err := fetchSpec(address)
	if err != nil {
		return fmt.Errorf("%w: %v", runner.ErrHarness, err)
	}
	if now != pinned {
		return fmt.Errorf("%w: %w: the spec changed since the round "+
			"began; start a new round", runner.ErrHarness, runner.ErrUnclean)
	}
	return nil
}

type deployment struct {
	arm               brief.Arm
	platform, account string
	hosts             []string
	env               map[string]string
}

// withCarloku gives the Rastrillo arms the demo account's service
// credential: it draws on its own creation budget rather than the
// operator's ten a day, shared by every agent on the machine, and it
// reaches the demo account only. Other arms deploy elsewhere and never
// see it.
func (d *deployment) withCarloku(token, console string) {
	if !d.arm.BuildsOnRastrillo() || token == "" {
		return
	}
	d.env = map[string]string{
		"CARLOS_SERVICE_TOKEN": token, "CARLOS_CONSOLE": console,
	}
}

// deployVars is where an arm deploys. The spec arm goes where the
// control goes, so the only difference between the two is the bar.
func deployVars(arm brief.Arm, platform, account, hosts string) deployment {
	switch arm {
	case brief.Carlos, brief.CarlosGopls:
		return deployment{arm: arm, account: carlokuAccount}
	case brief.Spec:
		// Only the spec arm may fetch the spec: the shared settings stay
		// as round 1 had them, so its control runs remain reusable.
		return deployment{arm: arm, platform: platform, account: account,
			hosts: append(splitList(hosts), "spec.carlosframework.com")}
	}
	return deployment{arm: arm, platform: platform, account: account,
		hosts: splitList(hosts)}
}

// setAside moves an earlier attempt out of the way under a name no
// other attempt has, and refuses to go on if it cannot: reusing the
// directory would truncate that attempt's transcripts. It returns how
// many attempts came before, which the next one's app name carries.
func setAside(dir string) (int, error) {
	if _, err := os.Stat(dir); err == nil {
		for attempt := 1; ; attempt++ {
			aside := fmt.Sprintf("%s-attempt-%d", dir, attempt)
			if _, err := os.Stat(aside); errors.Is(err, os.ErrNotExist) {
				if err := os.Rename(dir, aside); err != nil {
					return 0, err
				}
				break
			}
		}
	}
	earlier, err := filepath.Glob(dir + "-attempt-*")
	return len(earlier), err
}

// appName is the name a run deploys under. The spec arm deploys where
// the control does, and carlos-gopls where carlos does, so their names
// say so: two arms would otherwise build on, or overwrite, each other's
// app. (Carlos and control keep round 1's names.) A rerun gets a
// suffix: the earlier attempt's app may be live, and the agent would
// reuse it.
func appName(briefName, label string, arm brief.Arm, n, earlier int) string {
	name := fmt.Sprintf("%s-r%s-%d", briefName, label, n)
	switch arm {
	case brief.Spec:
		name = fmt.Sprintf("%s-spec-r%s-%d", briefName, label, n)
	case brief.CarlosGopls:
		name = fmt.Sprintf("%s-gopls-r%s-%d", briefName, label, n)
	}
	if earlier > 0 {
		name += fmt.Sprintf("-a%d", earlier+1)
	}
	return name
}

// fingerprints hashes every input that defines a round, and the subset
// the control arm depends on. Briefs are hashed whole, so an edited
// brief is a different experiment.
func fingerprints(
	root string, briefs []brief.Brief, settings map[string]string,
) (config, control string, err error) {
	inputs := map[string]string{}
	for name, value := range settings {
		inputs[name] = value
	}
	files := []string{"arms/carlos.txt", "arms/control.txt", "arms/spec.txt", "env.txt",
		"mapping.txt", "settings.json"}
	for _, b := range briefs {
		paths, err := filepath.Glob(filepath.Join(root, "briefs", b.Name, "*"))
		if err != nil {
			return "", "", err
		}
		for _, path := range paths {
			relative, _ := filepath.Rel(root, path)
			files = append(files, relative)
		}
	}
	for _, file := range files {
		raw, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			return "", "", err
		}
		inputs[file] = string(raw)
	}
	config = round.Fingerprint(inputs)
	delete(inputs, "arms/carlos.txt")
	delete(inputs, "arms/spec.txt")
	delete(inputs, specURL)
	delete(inputs, "carlos-auth")
	delete(inputs, "rastrillo")
	delete(inputs, "gopls")
	delete(inputs, "gopls-instructions")
	delete(inputs, "gopls-proxy")
	return config, round.Fingerprint(inputs), nil
}

func recordOf(spec runner.Spec, outcome runner.Outcome) round.RunRecord {
	where := func(phase runner.Phase) classify.Where {
		var hosts []string
		for _, address := range phase.Mapping.Pages {
			if parsed, err := url.Parse(address); err == nil {
				hosts = append(hosts, parsed.Hostname())
			}
		}
		return classify.Where{AppDir: filepath.Join(spec.Dir, "app"), LiveHosts: hosts}
	}
	record := round.RunRecord{
		Brief: spec.Brief.Name, Arm: spec.Arm, N: spec.N, App: spec.Vars.App,
		Build:         round.CostOf(outcome.Build.Transcript, where(outcome.Build)),
		Followup:      round.CostOf(outcome.Followup.Transcript, where(outcome.Followup)),
		Measured:      [2]measure.Measurement{outcome.Build.Measured, outcome.Followup.Measured},
		AgentFailures: outcome.AgentFailures,
	}
	if spec.Arm.BuildsOnRastrillo() {
		record.RastrilloVersion = requiredRastrillo(filepath.Join(spec.Dir, "app"))
	}
	return record
}

var requirePattern = regexp.MustCompile(`amadan\.net/rastrillo/rastrillo (v\S+)`)

// requiredRastrillo reads the version the agent's app ended up on,
// which for "latest" is the only record of what the agent installed.
func requiredRastrillo(app string) string {
	found := ""
	filepath.WalkDir(app, func(path string, entry os.DirEntry, err error) error {
		if err != nil || found != "" {
			return filepath.SkipDir
		}
		if entry.IsDir() && strings.Count(strings.TrimPrefix(path, app), "/") > 3 {
			return filepath.SkipDir
		}
		if entry.Name() == "go.mod" {
			raw, _ := os.ReadFile(path)
			if match := requirePattern.FindSubmatch(raw); match != nil {
				found = string(match[1])
			}
		}
		return nil
	})
	return found
}

// reuseControlRuns copies an earlier round's control runs only when
// everything the control depends on matches, and only when that round
// has every run this one asks for: round 0, run on another config,
// never qualifies.
func reuseControlRuns(
	root string, r *round.Round, from string, briefs []brief.Brief,
	runs int, settings map[string]string,
) error {
	earlier, err := loadRound(root, from)
	if err != nil {
		return err
	}
	// The earlier fingerprint covers the earlier round's briefs, so it is
	// recomputed on those: a groomer-only round may reuse a three-brief
	// round's groomer controls when nothing they depend on has changed.
	var earlierNames []string
	for _, run := range earlier.Runs {
		if !slices.Contains(earlierNames, run.Brief) {
			earlierNames = append(earlierNames, run.Brief)
		}
	}
	earlierBriefs, err := pickBriefs(root, strings.Join(earlierNames, ","))
	if err != nil {
		return err
	}
	_, control, err := fingerprints(root, earlierBriefs, settings)
	if err != nil {
		return err
	}
	if earlier.ControlConfig == "" || earlier.ControlConfig != control {
		return fmt.Errorf("round %s's control ran with a different model, "+
			"claude, framing, brief or platform; rerun the control", from)
	}
	for _, b := range briefs {
		for n := 1; n <= runs; n++ {
			if hasRun(*r, b.Name, brief.Control, n) {
				continue
			}
			if !hasRun(earlier, b.Name, brief.Control, n) {
				return fmt.Errorf("round %s has no control run %d of %s",
					from, n, b.Name)
			}
			for _, run := range earlier.Runs {
				if run.Brief == b.Name && run.Arm == brief.Control && run.N == n {
					run.ReusedFrom = from
					r.Runs = append(r.Runs, run)
				}
			}
		}
	}
	r.Notes = append(r.Notes, "Control runs reused from round "+from+".")
	sortRuns(r.Runs)
	return nil
}

func describe(s source.Source) string {
	if s.Version == "" {
		return s.Label
	}
	return fmt.Sprintf("%s (%s)", s.Label[:12], s.Version)
}

func outcomeLine(record round.RunRecord, err error) string {
	if err != nil {
		return "stopped: " + err.Error()
	}
	return fmt.Sprintf("$%.2f build, $%.2f follow-up, %d agent failures",
		record.Build.CostUSD, record.Followup.CostUSD, len(record.AgentFailures))
}

func pickBriefs(root, list string) ([]brief.Brief, error) {
	var briefs []brief.Brief
	for _, name := range splitList(list) {
		b, err := brief.Load(filepath.Join(root, "briefs", name))
		if err != nil {
			return nil, fmt.Errorf("brief %s: %w", name, err)
		}
		briefs = append(briefs, b)
	}
	return briefs, nil
}

func hasRun(r round.Round, name string, arm brief.Arm, n int) bool {
	for _, run := range r.Runs {
		if run.Brief == name && run.Arm == arm && run.N == n {
			return true
		}
	}
	return false
}

func sortRuns(runs []round.RunRecord) {
	sort.Slice(runs, func(a, b int) bool {
		if runs[a].Brief != runs[b].Brief {
			return runs[a].Brief < runs[b].Brief
		}
		if runs[a].Arm != runs[b].Arm {
			return runs[a].Arm < runs[b].Arm
		}
		return runs[a].N < runs[b].N
	})
}

func armsOf(list string) []brief.Arm {
	var arms []brief.Arm
	for _, name := range splitList(list) {
		arms = append(arms, brief.Arm(name))
	}
	return arms
}

func contains(arms []brief.Arm, arm brief.Arm) bool {
	for _, each := range arms {
		if each == arm {
			return true
		}
	}
	return false
}

func without(arms []brief.Arm, arm brief.Arm) []brief.Arm {
	var kept []brief.Arm
	for _, each := range arms {
		if each != arm {
			kept = append(kept, each)
		}
	}
	return kept
}

func splitList(list string) []string {
	var items []string
	for _, item := range strings.Split(list, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func defaultChrome() string {
	if chrome := os.Getenv("RASTRILLO_CHROME"); chrome != "" {
		return chrome
	}
	return "/usr/bin/chromium"
}

func defaultLighthouse() string {
	cache, err := runsRoot()
	if err != nil {
		return ""
	}
	installed := filepath.Join(cache, "tools", "node_modules", ".bin", "lighthouse")
	if _, err := os.Stat(installed); err != nil {
		return ""
	}
	return installed
}

// carlokuCredential reads the carlos arm's service credential only when
// one is named, and fails if the named file cannot be read: silently
// deploying as the operator instead would change the round unseen.
func carlokuCredential(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	return cleanenv.ReadTokenFile(path)
}

// gopls is what the carlos-gopls arm runs: the binary, by absolute
// path, its version, and its own instructions for agents.
type gopls struct{ path, version, instructions string }

// goplsFor finds the gopls the carlos-gopls arm runs and reads its
// version and instructions, which the round records and fingerprints.
// roundGopls is the version a resumed round recorded: such a round
// needs gopls whatever -arms says, or its fingerprint cannot match.
func goplsFor(arms []brief.Arm, path, roundGopls string) (gopls, error) {
	if !contains(arms, brief.CarlosGopls) && roundGopls == "" {
		return gopls{}, nil
	}
	if path == "" && roundGopls != "" {
		return gopls{}, fmt.Errorf("this round ran gopls (%s): pass -gopls "+
			"to resume it", roundGopls)
	}
	if path == "" {
		return gopls{}, errors.New("the carlos-gopls arm needs -gopls")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return gopls{}, err
	}
	found := gopls{path: absolute}
	for _, read := range []struct {
		into *string
		args []string
	}{
		{&found.version, []string{"version"}},
		{&found.instructions, []string{"mcp", "-instructions"}},
	} {
		out, err := exec.Command(absolute, read.args...).Output()
		if err != nil {
			return gopls{}, fmt.Errorf("%s %s: %w", absolute,
				strings.Join(read.args, " "), err)
		}
		*read.into = strings.TrimSpace(string(out))
		if *read.into == "" {
			return gopls{}, fmt.Errorf("%s %s printed nothing", absolute,
				strings.Join(read.args, " "))
		}
	}
	found.version, _, _ = strings.Cut(found.version, "\n")
	return found, nil
}

// goplsServer is the carlos-gopls arm's MCP config: one server, the
// bench's goplsmcp wrapper around `gopls mcp`, which starts gopls again
// in the app's module once its go.mod appears. It gets the Go settings
// the agent's own commands get, so gopls resolves the same Rastrillo.
func goplsServer(wrapper, goplsPath, app, logFile string, goEnv map[string]string) string {
	server := map[string]any{
		"type": "stdio", "command": wrapper,
		"args": []string{"-gopls", goplsPath, "-dir", app, "-log", logFile},
	}
	if len(goEnv) > 0 {
		server["env"] = goEnv
	}
	config, _ := json.Marshal(map[string]any{
		"mcpServers": map[string]any{"gopls": server},
	})
	return string(config)
}

// goplsProxySource is what the wrapper is built from.
var goplsProxySource = []string{"cmd/goplsmcp", "internal/mcpproxy"}

// sourceHash is the sha256 of the Go files (tests aside) in dirs, so a
// changed wrapper is a different round.
func sourceHash(root string, dirs ...string) (string, error) {
	hash := sha256.New()
	for _, dir := range dirs {
		files, err := filepath.Glob(filepath.Join(root, dir, "*.go"))
		if err != nil {
			return "", err
		}
		sort.Strings(files)
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			raw, err := os.ReadFile(file)
			if err != nil {
				return "", err
			}
			relative, _ := filepath.Rel(root, file)
			fmt.Fprintf(hash, "%d:%s%d:", len(relative), relative, len(raw))
			hash.Write(raw)
		}
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// buildGoplsProxy builds the wrapper into dir and returns its path.
func buildGoplsProxy(root, dir string) (string, error) {
	binary := filepath.Join(dir, "goplsmcp")
	build := exec.Command("go", "build", "-o", binary, "./cmd/goplsmcp")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		return "", fmt.Errorf("building goplsmcp: %v: %s", err, out)
	}
	return binary, nil
}

// pinGoplsInstructions saves the instructions the arm's prompt ends
// with beside the round's runs, and returns their sha256.
func pinGoplsInstructions(dir, instructions string) (string, error) {
	sum := fmt.Sprintf("%x", sha256.Sum256([]byte(instructions)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := filepath.Join(dir, "gopls-instructions-"+sum[:12]+".md")
	return sum[:12], os.WriteFile(name, []byte(instructions+"\n"), 0o644)
}

// extendSchedule adds the runs this command asks for that the saved
// schedule lacks: run index first, then brief, then arm, so the arms
// alternate (carlos 1, carlos-gopls 1, carlos 2, ...) and a drift over
// the round's days falls on every arm alike. The saved order is never
// changed, so a resume runs in the order the round began.
func extendSchedule(
	saved []round.Slot, briefs []string, arms []brief.Arm, runs int,
) []round.Slot {
	schedule := slices.Clone(saved)
	for n := 1; n <= runs; n++ {
		for _, name := range briefs {
			for _, arm := range armOrder(saved, arms) {
				slot := round.Slot{Brief: name, Arm: arm, N: n}
				if !slices.Contains(schedule, slot) {
					schedule = append(schedule, slot)
				}
			}
		}
	}
	return schedule
}

// armOrder puts the arms in the order the saved schedule first runs
// them, then any it has never run, so new slots alternate as the old.
func armOrder(saved []round.Slot, arms []brief.Arm) []brief.Arm {
	var ordered []brief.Arm
	for _, slot := range saved {
		if contains(arms, slot.Arm) && !contains(ordered, slot.Arm) {
			ordered = append(ordered, slot.Arm)
		}
	}
	for _, arm := range arms {
		if !contains(ordered, arm) {
			ordered = append(ordered, arm)
		}
	}
	return ordered
}

// pendingJobs are the scheduled runs this command asks for that have
// not run yet, in the schedule's order.
func pendingJobs(
	r round.Round, briefs []string, arms []brief.Arm, runs int,
) []round.Slot {
	var pending []round.Slot
	for _, slot := range r.Schedule {
		asked := slices.Contains(briefs, slot.Brief) &&
			contains(arms, slot.Arm) && slot.N <= runs
		if asked && !hasRun(r, slot.Brief, slot.Arm, slot.N) {
			pending = append(pending, slot)
		}
	}
	return pending
}

// cleanupCommands are the commands that delete a round's deployed apps,
// for a person to read and run.
func cleanupCommands(r round.Round) []string {
	commands := []string{
		"# Nothing is deleted for you. Read, then run what you mean to.",
	}
	for _, run := range r.Runs {
		if run.App == "" || run.ReusedFrom != "" {
			continue
		}
		if run.Arm.BuildsOnRastrillo() {
			commands = append(commands,
				"carlos apps delete --account-id bdf --app "+run.App)
		} else {
			commands = append(commands, fmt.Sprintf(
				"# on %s: delete the app %s", r.Platform, run.App))
		}
	}
	return commands
}
