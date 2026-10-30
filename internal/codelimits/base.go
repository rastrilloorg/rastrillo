package codelimits

import (
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
)

// CompareWithBase finds the records this branch added or raised since
// the base, and says which have no reason. Records written by Init carry
// none, and are not a problem: they are in the base.
func CompareWithBase(current, base *File) []string {
	if base == nil {
		return nil
	}
	before := map[identity]Record{}
	for _, record := range base.Records {
		before[record.identity()] = record
	}
	var messages []string
	for _, record := range current.Records {
		if record.Reason != "" {
			continue
		}
		earlier, existed := before[record.identity()]
		switch {
		case !existed:
			messages = append(messages, addedMessage(record))
		case record.Count > earlier.Count:
			messages = append(messages,
				raisedMessage(record, earlier, base.fromTip))
		}
	}
	return messages
}

// LoadBase reads the module's file as the branch's base had it: the
// merge base of HEAD and origin/main. The note it returns is a sentence
// for the person running the gate, and is one of two kinds. With no file,
// it says why there is nothing to compare with: there is no origin/main,
// or the base has no records file. With a file, it says the comparison is
// with origin/main's tip, because the checkout is shallow and has no merge
// base to find. Anything else that goes wrong (git missing, a root that is
// not there, histories with nothing in common in a full clone) is an
// error, because skipping would turn the ratchet off unseen.
//
// The file is named relative to root, not to the top of the repository,
// since root may be a nested module: git resolves "./" from the directory
// the command runs in.
func LoadBase(root string) (file *File, note string, err error) {
	_, err = gitOutput(root, "rev-parse", "--verify", "--quiet", "origin/main")
	if exitCode(err) == 1 {
		return nil, NoOrigin, nil
	}
	if err != nil {
		return nil, "", err
	}
	revision, note, err := baseRevision(root)
	if err != nil {
		return nil, "", err
	}
	path := "./" + FileName
	listing, err := gitOutput(root, "ls-tree", "--name-only", revision, path)
	if err != nil {
		return nil, "", err
	}
	if listing == "" {
		return nil, NoRecordsAtBase, nil
	}
	data, err := gitOutput(root, "show", revision+":"+path)
	if err != nil {
		return nil, "", err
	}
	file, err = Parse([]byte(data))
	if file != nil {
		file.fromTip = note != ""
	}
	return file, note, err
}

// baseRevision is the merge base of HEAD and origin/main. A CI checkout
// is shallow: HEAD and origin/main are each fetched without the commit
// they share, so there is no merge base to find and failing there would
// fail every branch. Then origin/main's tip stands in, with a note, and
// a raise it reports may only be main having moved on. In a full clone
// the same failure is real and stays an error.
func baseRevision(root string) (revision, note string, err error) {
	revision, err = gitOutput(root, "merge-base", "HEAD", "origin/main")
	if err == nil {
		return revision, "", nil
	}
	shallow, _ := gitOutput(root, "rev-parse", "--is-shallow-repository")
	if shallow != "true" {
		return "", "", err
	}
	return "origin/main", ShallowTip, nil
}

// exitCode is git's exit status, or -1 when git did not run to one.
func exitCode(err error) int {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

// gitOutput runs git in directory. GIT_DIR and GIT_WORK_TREE are dropped
// because a git hook sets them, and they would send git to the repository
// that is running the hook instead of the one in directory.
func gitOutput(directory string, arguments ...string) (string, error) {
	command := exec.Command("git", arguments...)
	command.Dir = directory
	command.Env = slices.DeleteFunc(os.Environ(), func(entry string) bool {
		return strings.HasPrefix(entry, "GIT_DIR=") ||
			strings.HasPrefix(entry, "GIT_WORK_TREE=")
	})
	output, err := command.Output()
	if err != nil {
		return "", gitFailed(arguments, err, stderrOf(err))
	}
	return strings.TrimSpace(string(output)), nil
}

func stderrOf(err error) string {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return strings.TrimSpace(string(exit.Stderr))
	}
	return ""
}
