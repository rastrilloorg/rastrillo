// Package source decides which Rastrillo an agent gets: the released
// one, as any user would, or a commit not yet released, delivered the
// same way (a Go module proxy and SKILL.md over HTTP) so a change can be
// measured before it ships.
package source

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	modzip "golang.org/x/mod/zip"
)

const rastrilloModule = "amadan.net/rastrillo/rastrillo"

type Source struct {
	Label    string            // "latest" or the commit
	Version  string            // what the agent installs; "" for latest
	SkillURL string            // where the prompt says SKILL.md is
	GoEnv    map[string]string // GOPROXY and GONOSUMDB for a commit
	Hosts    []string          // extra sandbox hosts (the skill server)
}

func Latest() Source {
	return Source{Label: "latest", SkillURL: "https://rastrillo.org/SKILL.md"}
}

// NextVersion names an unreleased commit as a pre-release of the next
// minor version, so `go install ...@latest` through the bench's proxy
// picks it and the app's go.mod reads as newer than the last release.
// The g keeps a hash of all digits from starting with a zero, which
// semver forbids in a numeric identifier.
func NextVersion(tags []string, sha string) string {
	latest := "v0.0.0"
	for _, tag := range tags {
		if semver.IsValid(tag) && semver.Prerelease(tag) == "" &&
			semver.Compare(tag, latest) > 0 {
			latest = tag
		}
	}
	var major, minor int
	fmt.Sscanf(semver.MajorMinor(latest), "v%d.%d", &major, &minor)
	return fmt.Sprintf("v%d.%d.0-bench.g%s", major, minor+1, sha[:12])
}

func modulePath(version string) module.Version {
	return module.Version{Path: rastrilloModule, Version: version}
}

// Local writes rev as a file-based module proxy under proxyDir and
// serves rev's SKILL.md on listenIP. The agent runs in a sandbox with a
// network of its own, so listenIP must be an address it can reach (the
// machine's Tailscale IP), not this process's 127.0.0.1.
func Local(repo, rev, proxyDir, listenIP string) (Source, func(), error) {
	noop := func() {}
	tags, err := git(repo, "tag", "--list")
	if err != nil {
		return Source{}, noop, err
	}
	sha, err := git(repo, "rev-parse", rev)
	if err != nil {
		return Source{}, noop, err
	}
	sha = strings.TrimSpace(sha)
	version := NextVersion(strings.Fields(tags), sha)
	if err := writeProxy(repo, sha, version, proxyDir); err != nil {
		return Source{}, noop, err
	}
	skill, err := git(repo, "show", sha+":SKILL.md")
	if err != nil {
		return Source{}, noop, err
	}
	address, stop, err := serveSkill(listenIP, skill)
	if err != nil {
		return Source{}, noop, err
	}
	return Source{
		Label:    sha,
		Version:  version,
		SkillURL: "http://" + address + "/SKILL.md",
		GoEnv: map[string]string{
			"GOPROXY":   "file://" + proxyDir + ",https://proxy.golang.org,direct",
			"GONOSUMDB": rastrilloModule,
		},
		Hosts: []string{listenIP},
	}, stop, nil
}

func writeProxy(repo, sha, version, proxyDir string) error {
	dir := filepath.Join(proxyDir, rastrilloModule, "@v")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// CreateFromVCS would read the commit directly, but it does not
	// recognise a linked worktree, whose .git is a file. An extracted
	// tree through CreateFromDir gets the same module rules (nested
	// modules and vendor left out) from any checkout.
	tree, err := os.MkdirTemp(proxyDir, "tree-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tree)
	archive := exec.Command("sh", "-c", `git -C "$1" archive "$2" | tar -x -C "$3"`,
		"archive", repo, sha, tree)
	if out, err := archive.CombinedOutput(); err != nil {
		return fmt.Errorf("git archive: %v: %s", err, out)
	}
	zipFile, err := os.Create(filepath.Join(dir, version+".zip"))
	if err != nil {
		return err
	}
	err = modzip.CreateFromDir(zipFile, modulePath(version), tree)
	if closeErr := zipFile.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	goMod, err := git(repo, "show", sha+":go.mod")
	if err != nil {
		return err
	}
	committed, err := git(repo, "show", "-s", "--format=%cI", sha)
	if err != nil {
		return err
	}
	when, err := time.Parse(time.RFC3339, strings.TrimSpace(committed))
	if err != nil {
		return err
	}
	info, _ := json.Marshal(map[string]any{"Version": version, "Time": when.UTC()})
	files := map[string]string{
		version + ".mod":  goMod,
		version + ".info": string(info),
		"list":            version + "\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func serveSkill(listenIP, skill string) (string, func(), error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(listenIP, "0"))
	if err != nil {
		return "", nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /SKILL.md", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Write([]byte(skill))
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go server.Serve(listener)
	return listener.Addr().String(), func() { server.Close() }, nil
}

func git(repo string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
