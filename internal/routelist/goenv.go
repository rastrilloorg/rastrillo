package routelist

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
)

// goSettings runs before any other go command in dir. It refuses what
// listing cannot do safely there, and gives the -mod flag that every go
// command for dir is run with, so go list and go build read the same
// code. A workspace is refused because Go may write go.work.sum in
// workspace mode, even with -mod=readonly, and the app's files must be
// left as they were.
func goSettings(ctx context.Context, dir string) (string, error) {
	if !promisesCleanup {
		return "", say(noCleanupHere)
	}
	command := inOwnGroup(ctx, "go", "env", "GOWORK", "GOMOD")
	command.Dir = dir
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.run(); err != nil {
		if ctx.Err() != nil {
			return "", stopped{stoppedBeforeListing, ctx.Err()}
		}
		return "", say(goEnvFailed, dir, reported(&stderr, err))
	}
	// One line each, GOWORK's first and empty outside a workspace, so
	// the output is split before it is trimmed.
	work, goMod, _ := strings.Cut(stdout.String(), "\n")
	work = strings.TrimSpace(work)
	if work != "" && work != "off" {
		return "", say(inWorkspace, work)
	}
	return modFlag(strings.TrimSpace(goMod)), nil
}

// modFlag keeps the app's vendored code when it has some: the go
// command vendors on its own when vendor/modules.txt exists, and a
// forced -mod=readonly would compile the module cache's copy instead,
// or need the network. Otherwise -mod=readonly, so a GOFLAGS=-mod=mod
// cannot rewrite go.mod or go.sum. Outside a module GOMOD is empty or
// os.DevNull, and go list then fails with Go's own message.
func modFlag(goMod string) string {
	if goMod == "" || goMod == os.DevNull {
		return "-mod=readonly"
	}
	listed := filepath.Join(filepath.Dir(goMod), "vendor", "modules.txt")
	if _, err := os.Stat(listed); err == nil {
		return "-mod=vendor"
	}
	return "-mod=readonly"
}

// reported is what the go command said on stderr, or the error when it
// said nothing.
func reported(stderr *bytes.Buffer, err error) string {
	if said := strings.TrimSpace(stderr.String()); said != "" {
		return said
	}
	return err.Error()
}
