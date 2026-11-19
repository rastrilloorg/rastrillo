package codelimits

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestARaiseWithoutAReasonFailsAgainstTheBase(t *testing.T) {
	base := &File{Records: []Record{recordOf(FuncLines, "a.go", "Serve", 45)}}
	current := &File{Records: []Record{
		{Kind: FuncLines, Path: "a.go", Key: "Serve", Count: 50, Line: 8},
	}}
	wantOne(t, CompareWithBase(current, base),
		"a.go", "Serve", "line 8", "45", "50", "reason")
}

func TestALowerRecordNeedsNoReason(t *testing.T) {
	base := &File{Records: []Record{recordOf(FuncLines, "a.go", "Serve", 45)}}
	current := &File{Records: []Record{
		recordOf(FuncLines, "a.go", "Serve", 41),
	}}
	if messages := CompareWithBase(current, base); len(messages) != 0 {
		t.Fatalf("lowering a record failed: %q", messages)
	}
}

func TestARaiseWithAReasonPasses(t *testing.T) {
	base := &File{Records: []Record{recordOf(FuncLines, "a.go", "Serve", 45)}}
	current := &File{Records: []Record{{
		Kind: FuncLines, Path: "a.go", Key: "Serve", Count: 50,
		Reason: "a switch over every route",
	}}}
	if messages := CompareWithBase(current, base); len(messages) != 0 {
		t.Fatalf("a raise with a reason failed: %q", messages)
	}
}

func TestAnAddedRecordWithoutAReasonFails(t *testing.T) {
	base := &File{}
	current := &File{Records: []Record{
		{Kind: Names, Path: "a.go", Key: "-/x", Count: 1, Line: 5},
	}}
	wantOne(t, CompareWithBase(current, base), "a.go", "line 5", "reason")
}

func TestAnAddedRecordWithAReasonPasses(t *testing.T) {
	current := &File{Records: []Record{{
		Kind: Names, Path: "a.go", Key: "-/x", Count: 1, Reason: "a loop index",
	}}}
	if messages := CompareWithBase(current, &File{}); len(messages) != 0 {
		t.Fatalf("an added record with a reason failed: %q", messages)
	}
}

// git runs a command in a scratch repository. Setting the identity and
// turning signing off keeps the test from depending on the machine's
// git configuration.
func git(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	arguments = append([]string{
		"-c", "user.name=test", "-c", "user.email=test@example.com",
		"-c", "commit.gpgsign=false",
	}, arguments...)
	command := exec.Command("git", arguments...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
	return strings.TrimSpace(string(output))
}

func scratchRepository(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	git(t, directory, "init", "-q")
	return directory
}

func writeRecords(t *testing.T, module, text string) {
	t.Helper()
	path := filepath.Join(module, FileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitAll(t *testing.T, repository string) {
	t.Helper()
	git(t, repository, "add", "-A")
	git(t, repository, "commit", "-q", "-m", "records")
}

func TestNoBaseSkipsTheComparison(t *testing.T) {
	repository := scratchRepository(t)
	writeRecords(t, repository, "func a.go Serve 45\n")
	commitAll(t, repository)
	base, skipped, err := LoadBase(repository)
	if err != nil || base != nil || skipped != NoOrigin {
		t.Fatalf("got %+v, %q, %v with no origin/main", base, skipped, err)
	}
}

func TestTheBaseIsTheFileWhereTheBranchLeftMain(t *testing.T) {
	repository := scratchRepository(t)
	writeRecords(t, repository, "func a.go Serve 45\n")
	commitAll(t, repository)
	git(t, repository, "update-ref", "refs/remotes/origin/main", "HEAD")
	writeRecords(t, repository, "func a.go Serve 50\n")
	commitAll(t, repository)

	base, skipped, err := LoadBase(repository)
	if err != nil || skipped != "" {
		t.Fatalf("got %+v, %q, %v", base, skipped, err)
	}
	if len(base.Records) != 1 || base.Records[0].Count != 45 {
		t.Fatalf("base is %+v, want the 45 main had", base.Records)
	}
}

func TestANestedModuleFindsItsFileFromTheRepositoryRoot(t *testing.T) {
	repository := scratchRepository(t)
	module := filepath.Join(repository, "money")
	writeRecords(t, module, "func money.go Serve 45\n")
	commitAll(t, repository)
	git(t, repository, "update-ref", "refs/remotes/origin/main", "HEAD")
	writeRecords(t, module, "func money.go Serve 50\n")
	commitAll(t, repository)

	base, skipped, err := LoadBase(module)
	if err != nil || skipped != "" {
		t.Fatalf("got %+v, %q, %v", base, skipped, err)
	}
	if len(base.Records) != 1 || base.Records[0].Count != 45 {
		t.Fatalf("base is %+v, want the 45 main had", base.Records)
	}
}

func TestAFileNewSinceTheBaseHasNoBase(t *testing.T) {
	repository := scratchRepository(t)
	writeRecords(t, filepath.Join(repository, "other"), "# elsewhere\n")
	commitAll(t, repository)
	git(t, repository, "update-ref", "refs/remotes/origin/main", "HEAD")
	writeRecords(t, repository, "func a.go Serve 45\n")
	commitAll(t, repository)

	base, skipped, err := LoadBase(repository)
	if err != nil || base != nil || skipped != NoRecordsAtBase {
		t.Fatalf("got %+v, %q, %v with no file at the base",
			base, skipped, err)
	}
}

// A new app runs make ci before anyone types git init. There is no
// base to compare with, and that is no fault to stop the gate for.
func TestOutsideARepositorySkipsTheComparison(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(directory))
	base, skipped, err := LoadBase(directory)
	if err != nil || base != nil || skipped != NotARepository {
		t.Fatalf("got %+v, %q, %v outside a repository", base, skipped, err)
	}
}

// Git exits 128 for a broken repository too, with other words. Taking
// that for "no repository" would turn the ratchet off unseen.
func TestABrokenRepositoryIsStillAnError(t *testing.T) {
	broken := scratchRepository(t)
	appendLine(t, filepath.Join(broken, ".git", "config"), "[[[")
	worktree := t.TempDir()
	appendLine(t, filepath.Join(worktree, ".git"), "gitdir: /nowhere")
	for _, directory := range []string{broken, worktree} {
		t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(directory))
		base, skipped, err := LoadBase(directory)
		if err == nil {
			t.Errorf("%s: got %+v, %q, no error", directory, base, skipped)
		}
	}
}

// Git's words for finding no repository, searching up to the root or
// stopping at a filesystem boundary (a tmpfs /tmp, a separate /home),
// against its words for a .git file that points nowhere.
func TestOnlyGitsNoRepositoryWordsMeanNoRepository(t *testing.T) {
	for message, want := range map[string]bool{
		"fatal: not a git repository (or any of the parent " +
			"directories): .git": true,
		"fatal: not a git repository (or any parent up to mount point " +
			"/tmp)\nStopping at filesystem boundary " +
			"(GIT_DISCOVERY_ACROSS_FILESYSTEM not set).": true,
		"fatal: not a git repository: /nowhere":        false,
		"fatal: bad config line 6 in file .git/config": false,
	} {
		if got := saysNoRepository(message); got != want {
			t.Errorf("saysNoRepository(%q) = %v, want %v", message, got,
				want)
		}
	}
}

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(line + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestARootThatDoesNotExistIsAnError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nowhere")
	base, skipped, err := LoadBase(missing)
	if err == nil {
		t.Fatalf("got %+v, %q, no error for a missing root", base, skipped)
	}
}

func TestGitMissingFromThePathIsAnError(t *testing.T) {
	repository := scratchRepository(t)
	t.Setenv("PATH", t.TempDir())
	base, skipped, err := LoadBase(repository)
	if err == nil {
		t.Fatalf("got %+v, %q, no error without git", base, skipped)
	}
}

// A shallow clone can have origin/main and no history in common with
// HEAD. Skipping would turn the ratchet off without anyone knowing.
func TestHistoriesWithNoCommonAncestorAreAnError(t *testing.T) {
	repository := scratchRepository(t)
	writeRecords(t, repository, "func a.go Serve 45\n")
	commitAll(t, repository)
	main := git(t, repository, "rev-parse", "HEAD")
	git(t, repository, "checkout", "-q", "--orphan", "unrelated")
	// A different tree, or the two root commits would be one object.
	writeRecords(t, repository, "func a.go Serve 46\n")
	commitAll(t, repository)
	git(t, repository, "update-ref", "refs/remotes/origin/main", main)
	base, skipped, err := LoadBase(repository)
	if err == nil {
		t.Fatalf("got %+v, %q, no error for unrelated histories",
			base, skipped)
	}
}

// A hook that runs the gate has git's own variables set, and they would
// point git at the wrong repository.
func TestGitsOwnVariablesDoNotRedirectIt(t *testing.T) {
	repository := scratchRepository(t)
	writeRecords(t, repository, "func a.go Serve 45\n")
	commitAll(t, repository)
	git(t, repository, "update-ref", "refs/remotes/origin/main", "HEAD")
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "elsewhere"))
	t.Setenv("GIT_WORK_TREE", t.TempDir())
	base, skipped, err := LoadBase(repository)
	if err != nil || skipped != "" || len(base.Records) != 1 {
		t.Fatalf("got %+v, %q, %v", base, skipped, err)
	}
}

// shallowBranch builds what the CI fleet checks out: a feature branch
// and origin/main each fetched at depth 1, so neither has the commit
// they share and merge-base finds nothing. The upstream's records say
// 45 on main; the branch's say branchRecords.
func shallowBranch(t *testing.T, branchRecords string) string {
	t.Helper()
	upstream := t.TempDir()
	git(t, upstream, "init", "-q", "-b", "main")
	writeRecords(t, upstream, "func a.go Serve 45\n")
	commitAll(t, upstream)
	git(t, upstream, "checkout", "-q", "-b", "feature")
	writeRecords(t, upstream, branchRecords)
	commitAll(t, upstream)
	git(t, upstream, "checkout", "-q", "main")
	writeRecords(t, upstream, "func a.go Serve 45\n# main moved on\n")
	commitAll(t, upstream)

	clone := filepath.Join(t.TempDir(), "clone")
	url := "file://" + upstream
	git(t, filepath.Dir(clone), "clone", "-q", "--depth", "1",
		"--branch", "feature", url, clone)
	git(t, clone, "fetch", "-q", "--depth", "1", "origin",
		"main:refs/remotes/origin/main")
	return clone
}

func TestAShallowCheckoutComparesWithTheTipAndSaysSo(t *testing.T) {
	clone := shallowBranch(t, "func a.go Serve 50\n")
	base, note, err := LoadBase(clone)
	if err != nil || note != ShallowTip {
		t.Fatalf("got %+v, %q, %v", base, note, err)
	}
	if len(base.Records) != 1 || base.Records[0].Count != 45 {
		t.Fatalf("base is %+v, want the 45 the tip has", base.Records)
	}
}

func TestARaiseAgainstTheTipSaysToMergeOriginMain(t *testing.T) {
	clone := shallowBranch(t, "func a.go Serve 50\n")
	base, _, err := LoadBase(clone)
	if err != nil {
		t.Fatal(err)
	}
	current, err := Load(clone)
	if err != nil {
		t.Fatal(err)
	}
	wantOne(t, CompareWithBase(current, base), "50", "reason",
		"merge origin/main")
}

func TestARaiseAgainstTheMergeBaseDoesNotMentionAMerge(t *testing.T) {
	base := &File{Records: []Record{recordOf(FuncLines, "a.go", "Serve", 45)}}
	current := &File{Records: []Record{
		recordOf(FuncLines, "a.go", "Serve", 50),
	}}
	for _, message := range CompareWithBase(current, base) {
		if strings.Contains(message, "merge origin/main") {
			t.Errorf("the base is the merge base, yet: %q", message)
		}
	}
}
