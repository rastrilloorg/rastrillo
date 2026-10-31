package source

import (
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	modzip "golang.org/x/mod/zip"
)

func TestNextVersion(t *testing.T) {
	tags := []string{"v0.29.0", "money/v0.2.0", "v0.28.0", "archive/x"}

	got := NextVersion(tags, "0a1b2c3d4e5f6a7b8c9d")

	if want := "v0.30.0-bench.g0a1b2c3d4e5f"; got != want {
		t.Errorf("NextVersion = %s, want %s", got, want)
	}
}

func TestLocalWritesAValidProxy(t *testing.T) {
	repo, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	head, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	rev := strings.TrimSpace(string(head))
	proxy := t.TempDir()

	source, stop, err := Local(repo, rev, proxy, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	versions := filepath.Join(proxy, "amadan.net/rastrillo/rastrillo/@v")
	list, err := os.ReadFile(filepath.Join(versions, "list"))
	if err != nil || strings.TrimSpace(string(list)) != source.Version {
		t.Errorf("@v/list = %q (%v), want %s", list, err, source.Version)
	}
	zip := filepath.Join(versions, source.Version+".zip")
	if _, err := modzip.CheckZip(modulePath(source.Version), zip); err != nil {
		t.Errorf("zip: %v", err)
	}
	for _, suffix := range []string{".info", ".mod"} {
		if _, err := os.Stat(filepath.Join(versions, source.Version+suffix)); err != nil {
			t.Error(err)
		}
	}
	if !strings.HasPrefix(source.GoEnv["GOPROXY"], "file://"+proxy+",") {
		t.Errorf("GOPROXY = %q", source.GoEnv["GOPROXY"])
	}

	response, err := http.Get(source.SkillURL)
	if err != nil {
		t.Fatal(err)
	}
	served, _ := io.ReadAll(response.Body)
	response.Body.Close()
	want, _ := exec.Command("git", "-C", repo, "show", rev+":SKILL.md").Output()
	if string(served) != string(want) {
		t.Errorf("served SKILL.md differs from %s's (%d vs %d bytes)",
			rev, len(served), len(want))
	}
}
