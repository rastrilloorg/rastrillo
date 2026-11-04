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
	var moved []vendoredFile
	for index, file := range group {
		if err := findings.swapIn(staged[index], file.path); err != nil {
			findings.putBack(moved)
			return pairError(group, err)
		}
		moved = append(moved, file)
	}
	for _, file := range moved {
		os.Remove(backupOf(file.path))
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

// swapIn keeps the file it replaces as a backup until the whole group
// is in, which is what putBack restores from.
func (findings *report) swapIn(staged, target string) error {
	_, err := os.Stat(target)
	existed := err == nil
	if existed {
		if err := findings.move(target, backupOf(target)); err != nil {
			return err
		}
	}
	if err := findings.move(staged, target); err != nil {
		if existed {
			findings.move(backupOf(target), target)
		}
		return err
	}
	return nil
}

func (findings *report) putBack(moved []vendoredFile) {
	for _, file := range moved {
		if _, err := os.Stat(backupOf(file.path)); err == nil {
			findings.move(backupOf(file.path), file.path)
		} else {
			os.Remove(file.path)
		}
	}
}

func (findings *report) move(from, to string) error {
	if findings.rename != nil {
		return findings.rename(from, to)
	}
	return os.Rename(from, to)
}

func backupOf(path string) string {
	return filepath.Join(filepath.Dir(path),
		"."+filepath.Base(path)+".old")
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
