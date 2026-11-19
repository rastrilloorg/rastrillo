package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// refresh writes each pair as one change: a write that fails part way
// must leave a pair as it was, never the new calendar.js beside the old
// datetime.js, or the slim rastrillo.js without its fiesta.js.
func (findings *report) refresh(output io.Writer, files []vendoredFile) error {
	for _, group := range pairedUp(files) {
		if err := findings.replaceTogether(group); err != nil {
			return err
		}
		for _, file := range group {
			findings.printWritten(output, file)
		}
	}
	return nil
}

func pairedUp(files []vendoredFile) [][]vendoredFile {
	var groups [][]vendoredFile
	placed := map[string]bool{}
	for _, file := range files {
		if placed[file.name] {
			continue
		}
		group := []vendoredFile{file}
		for _, other := range files {
			if other.name == vendoredPairs[file.name] {
				group = append(group, other)
			}
		}
		for _, member := range group {
			placed[member.name] = true
		}
		groups = append(groups, group)
	}
	return groups
}

// replaceTogether stages every new file beside its target before it
// moves any of them in, since a full disk or a read-only file is far
// likelier while writing bytes than while renaming. If a move still
// fails, the moves already made are put back.
func (findings *report) replaceTogether(group []vendoredFile) error {
	var staged []string
	defer func() {
		for _, path := range staged {
			os.Remove(path)
		}
	}()
	for _, file := range group {
		path, err := stage(file)
		if err != nil {
			return pairError(group, err)
		}
		staged = append(staged, path)
	}
	var swaps []swap
	for index, file := range group {
		done, err := findings.swapIn(staged[index], file.path)
		swaps = append(swaps, done)
		if err != nil {
			return findings.rollBack(group, swaps, err)
		}
	}
	for _, done := range swaps {
		os.Remove(done.backup)
	}
	return nil
}

// stage writes the library's bytes to a temporary file in the target's
// own directory, so moving it in is a rename on one filesystem. An
// existing target is opened for writing first: doctor never wrote over
// a read-only file, and renaming over one would.
func stage(file vendoredFile) (string, error) {
	directory := filepath.Dir(file.path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", err
	}
	if target, err := os.OpenFile(file.path, os.O_WRONLY, 0); err == nil {
		target.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	temporary, err := os.CreateTemp(directory, "."+file.name+".new-*")
	if err != nil {
		return "", err
	}
	_, err = temporary.Write(file.lib)
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(temporary.Name(), 0o644)
	}
	if err != nil {
		os.Remove(temporary.Name())
		return "", err
	}
	return temporary.Name(), nil
}

// swap is what one member's move changed. backup holds the original
// until the whole group is in; it is empty when there was no original,
// and then placed says whether the new file now stands where nothing
// stood, which a rollback removes rather than restoring anything.
type swap struct {
	target string
	backup string
	placed bool
}

// swapIn moves the original aside to a backup name no other file has,
// so neither a file the app keeps nor an earlier run's leftover can be
// overwritten, deleted or restored in its place.
func (findings *report) swapIn(staged, target string) (swap, error) {
	done := swap{target: target}
	if _, err := os.Lstat(target); err == nil {
		backup, err := reserveBackup(target)
		if err != nil {
			return done, err
		}
		if err := findings.move(target, backup); err != nil {
			os.Remove(backup)
			return done, err
		}
		done.backup = backup
	}
	if err := findings.move(staged, target); err != nil {
		return done, err
	}
	done.placed = true
	return done, nil
}

func reserveBackup(target string) (string, error) {
	reserved, err := os.CreateTemp(filepath.Dir(target),
		"."+filepath.Base(target)+".old-*")
	if err != nil {
		return "", err
	}
	reserved.Close()
	return reserved.Name(), nil
}

// rollBack puts every original back. A restore that fails keeps its
// backup, and the error names it, because a message saying the files
// are as they were would then send nobody to look for them.
func (findings *report) rollBack(
	group []vendoredFile, swaps []swap, cause error,
) error {
	var failures []error
	var kept []string
	for _, done := range swaps {
		switch {
		case done.backup != "":
			if err := findings.move(done.backup, done.target); err != nil {
				failures = append(failures, err)
				kept = append(kept, rel(findings.dir, done.backup))
			}
		case done.placed:
			if err := os.Remove(done.target); err != nil {
				failures = append(failures, err)
			}
		}
	}
	if len(failures) == 0 {
		return pairError(group, cause)
	}
	return restoreError(group, cause, errors.Join(failures...), kept)
}

func (findings *report) move(from, to string) error {
	if findings.rename != nil {
		return findings.rename(from, to)
	}
	return os.Rename(from, to)
}

// pairError names every file in the group, so the person knows which
// were kept as they were and not only the one that failed.
func pairError(group []vendoredFile, err error) error {
	var names []string
	for _, file := range group {
		names = append(names, file.name)
	}
	if len(names) == 1 {
		return fmt.Errorf("write %s: %w", names[0], err)
	}
	return fmt.Errorf("write %s: %w; both are as they were",
		strings.Join(names, " and "), err)
}

func restoreError(
	group []vendoredFile, cause, restore error, kept []string,
) error {
	var names []string
	for _, file := range group {
		names = append(names, file.name)
	}
	written := strings.Join(names, " and ")
	if len(kept) == 0 {
		return fmt.Errorf("write %s: %w; restoring failed (%v)",
			written, cause, restore)
	}
	return fmt.Errorf("write %s: %w; restoring failed (%v): "+
		"the originals are at %s", written, cause, restore,
		strings.Join(kept, " and "))
}
