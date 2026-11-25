package rastrillo

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// releaseFiles is the file list hack/release.sh gives tag's release,
// with SBOM_AFTER and SIGN_AFTER from settings or else the script's own.
func releaseFiles(t *testing.T, tag string, settings ...string) []string {
	t.Helper()
	cmd := exec.Command("sh", "hack/release.sh", "-files", tag)
	cmd.Env = append(os.Environ(), "SBOM_AFTER=", "SIGN_AFTER=")
	cmd.Env = append(cmd.Env, settings...)
	out, err := cmd.Output()
	if exit, ok := err.(*exec.ExitError); ok {
		t.Fatalf("hack/release.sh -files %s: %v: %s", tag, err, exit.Stderr)
	}
	if err != nil {
		t.Fatalf("hack/release.sh -files %s: %v", tag, err)
	}
	return strings.Fields(string(out))
}

func tarballs(tag string) []string {
	var names []string
	for _, target := range []string{
		"darwin_amd64", "darwin_arm64", "linux_amd64", "linux_arm64",
	} {
		names = append(names, "rastrillo_"+tag+"_"+target+".tar.gz")
	}
	return names
}

func sboms(tag string) []string {
	var names []string
	for _, tarball := range tarballs(tag) {
		base := strings.TrimSuffix(tarball, ".tar.gz")
		names = append(names, base+".cdx.json")
	}
	return append(names, "rastrillo_"+tag+"_module.cdx.json")
}

// TestReleaseFilesStartAfterTheCut checks where SBOMs and signatures
// begin. Every published release is permanent on amadan, and its
// checksums.txt lists only the files it has: if an older tag were
// expected to carry an SBOM, release-check would call every release
// incomplete and release would try to add files that checksums.txt
// does not cover. A later tag that lacks one must be reported.
func TestReleaseFilesStartAfterTheCut(t *testing.T) {
	published := append(tarballs("v0.47.0"), "checksums.txt")
	if got := releaseFiles(t, "v0.47.0"); !slices.Equal(got, published) {
		t.Errorf("v0.47.0, already published, would carry %v", got)
	}
	for _, tag := range []string{"v0.47.1", "v0.48.0", "v0.100.0"} {
		want := append(tarballs(tag), sboms(tag)...)
		want = append(want, "checksums.txt", "checksums.txt.sig")
		if got := releaseFiles(t, tag); !slices.Equal(got, want) {
			t.Errorf("%s carries %v, want %v", tag, got, want)
		}
	}
	got := releaseFiles(t, "v0.48.0", "SIGN_AFTER=v0.48.0")
	if slices.Contains(got, "checksums.txt.sig") {
		t.Errorf("v0.48.0 needs a signature though it is SIGN_AFTER")
	}
	got = releaseFiles(t, "v0.48.1", "SIGN_AFTER=v0.48.0")
	if got[len(got)-1] != "checksums.txt.sig" {
		t.Errorf("v0.48.1, after SIGN_AFTER, carries %v, no signature", got)
	}
}

// releaseSBOM is the head of an SBOM as cyclonedx-gomod v1.12.0 lays it
// out, with toolHash as the hash it records of its own executable.
func releaseSBOM(toolHash string) string {
	return `{
  "metadata": {
    "tools": [
      {
        "vendor": "CycloneDX",
        "name": "cyclonedx-gomod",
        "version": "v1.12.0",
        "hashes": [
          {
            "alg": "SHA-256",
            "content": "` + toolHash + `"
          }
        ],
        "externalReferences": []
      }
    ]
  },
  "components": [
    {
      "name": "std",
      "hashes": [
        {
          "alg": "SHA-256",
          "content": "c0ffee"
        }
      ]
    }
  ]
}
`
}

// TestReleaseSBOMsDoNotDependOnTheReleaseHost checks that the tool's
// hashes of its own executable, which differ between an amd64 and an
// arm64 release host, are dropped, and nothing else is. Otherwise a
// release interrupted on one host fails agree when retried on another.
func TestReleaseSBOMsDoNotDependOnTheReleaseHost(t *testing.T) {
	dir := t.TempDir()
	amd := dir + "/amd64.cdx.json"
	arm := dir + "/arm64.cdx.json"
	for path, hash := range map[string]string{amd: "aaaa", arm: "bbbb"} {
		err := os.WriteFile(path, []byte(releaseSBOM(hash)), 0o644)
		if err != nil {
			t.Fatal(err)
		}
	}
	tidy := exec.Command("sh", "hack/release.sh", "-tidy-sbom", amd, arm)
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("-tidy-sbom: %v: %s", err, out)
	}
	amdSBOM, _ := os.ReadFile(amd)
	armSBOM, _ := os.ReadFile(arm)
	if !bytes.Equal(amdSBOM, armSBOM) {
		t.Errorf("SBOMs from two hosts differ after tidying:\n%s\n%s",
			amdSBOM, armSBOM)
	}
	if !json.Valid(amdSBOM) {
		t.Errorf("tidied SBOM is not JSON:\n%s", amdSBOM)
	}
	if !bytes.Contains(amdSBOM, []byte(`"c0ffee"`)) {
		t.Errorf("tidying dropped a component's hash:\n%s", amdSBOM)
	}
	again := exec.Command("sh", "hack/release.sh", "-tidy-sbom", amd)
	if again.Run() == nil {
		t.Errorf("-tidy-sbom accepted an SBOM with no tool hashes to drop")
	}
}

// fakeSigner writes a signer that logs each step it is asked for to
// log, and whose verify exits with verifyExit.
func fakeSigner(t *testing.T, verifyExit string) (signer, log string) {
	t.Helper()
	dir := t.TempDir()
	signer, log = dir+"/signer", dir+"/log"
	script := "#!/bin/sh\necho \"$1\" >>" + log + "\n" +
		"case $1 in sign) echo sig >\"$3\" ;; verify) exit " +
		verifyExit + " ;; esac\n"
	if err := os.WriteFile(signer, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return signer, log
}

// TestReleaseSignRehearsalRunsTheSigner checks that a rehearsal calls
// both sign and verify, and fails when either does, so a broken signer
// is found before any release needs it.
func TestReleaseSignRehearsalRunsTheSigner(t *testing.T) {
	signer, log := fakeSigner(t, "0")
	try := exec.Command("sh", "hack/release.sh", "-sign", signer, "-try-sign")
	if out, err := try.CombinedOutput(); err != nil {
		t.Fatalf("-try-sign: %v: %s", err, out)
	}
	steps, _ := os.ReadFile(log)
	if string(steps) != "sign\nverify\n" {
		t.Errorf("the signer was asked for %q, want sign then verify", steps)
	}
	bad, _ := fakeSigner(t, "1")
	try = exec.Command("sh", "hack/release.sh", "-sign", bad, "-try-sign")
	if out, err := try.CombinedOutput(); err == nil {
		t.Errorf("-try-sign passed a signature that fails verify: %s", out)
	}
	try = exec.Command("sh", "hack/release.sh",
		"-sign", t.TempDir()+"/missing", "-try-sign")
	if out, err := try.CombinedOutput(); err == nil {
		t.Errorf("-try-sign passed with no signer at all: %s", out)
	}
}

// The committed fixture was signed with the release key itself, so a
// changed allowed_signers or a broken verify step fails here, not in
// front of someone checking a download.
func TestReleaseKeyVerifiesItsFixture(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("no ssh-keygen")
	}
	verify := func(file string) error {
		return exec.Command("sh", "hack/sign-ssh.sh", "verify", file,
			"testdata/release/signed.txt.sig").Run()
	}
	if err := verify("testdata/release/signed.txt"); err != nil {
		t.Fatalf("the release key's fixture does not verify: %v", err)
	}
	changed := filepath.Join(t.TempDir(), "signed.txt")
	os.WriteFile(changed,
		[]byte("Rastrillo release key test fixture!\n"), 0o644)
	if verify(changed) == nil {
		t.Error("a changed fixture verified against the original signature")
	}
}

// release.sh publishes a signed release only from a tag the release
// key signed. An unsigned tag and one signed by any other SSH key must
// both be refused; git verify-tag alone would also take a trusted GPG
// signature.
func TestReleaseTagNeedsTheReleaseKey(t *testing.T) {
	for _, tool := range []string{"ssh-keygen", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("no " + tool)
		}
	}
	script, _ := filepath.Abs("hack/sign-ssh.sh")
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		run := exec.Command("git", append([]string{"-c", "user.name=t",
			"-c", "user.email=t@example.org"}, args...)...)
		run.Dir = repo
		if out, err := run.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	git("commit", "-q", "--allow-empty", "-m", "x")
	git("tag", "-a", "-m", "x", "unsigned")
	key := filepath.Join(t.TempDir(), "other")
	keygen := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "",
		"-f", key)
	if out, err := keygen.CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v: %s", err, out)
	}
	git("-c", "gpg.format=ssh", "-c", "user.signingkey="+key+".pub",
		"tag", "-s", "-m", "x", "other")
	for _, tag := range []string{"unsigned", "other"} {
		verify := exec.Command("sh", script, "verify-tag", tag)
		verify.Dir = repo
		if out, err := verify.CombinedOutput(); err == nil {
			t.Errorf("verify-tag accepted %s: %s", tag, out)
		}
	}
}
