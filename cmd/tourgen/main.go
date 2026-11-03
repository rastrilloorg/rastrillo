// Command tourgen writes what rastrillo.org's tour pages are built from:
// tour.json, and a zip of the app as it stands after each step. It
// replays the tour with the rastrillo release it was itself run at, so
// the site shows exactly what a reader installing that release gets:
//
//	go run amadan.net/rastrillo/rastrillo/cmd/tourgen@v0.31.0 -out DIR
package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"

	"amadan.net/rastrillo/rastrillo/tour"
)

const module = "amadan.net/rastrillo/rastrillo"

func main() {
	out := flag.String("out", "",
		"the directory to write tour.json and zips/ into")
	name := flag.String("tour", "chat", "the tour to write")
	checkout := flag.String("checkout", "", "a rastrillo checkout to "+
		"replay with instead of the release, to preview a tour")
	flag.Parse()
	if err := write(*out, *name, *checkout); err != nil {
		fmt.Fprintln(os.Stderr, "tourgen:", err)
		os.Exit(1)
	}
}

// release is a tagged version, the only kind a reader can install.
var release = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

func write(out, name, checkout string) error {
	if out == "" {
		return errors.New("name the output directory with -out")
	}
	scratch, err := os.MkdirTemp("", "tourgen-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	version, toolchain, err := toolchainFor(checkout, scratch)
	if err != nil {
		return err
	}
	chosen, err := tour.Load(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(out, "zips"), 0o755); err != nil {
		return err
	}
	page := pageOf(chosen, version)
	played := func(step tour.Step, directory string,
		changes []tour.Change) error {
		return page.add(step, directory, changes, out)
	}
	if err := chosen.Replay(scratch, toolchain, played); err != nil {
		return err
	}
	return page.save(filepath.Join(out, "tour.json"))
}

// toolchainFor runs rastrillo at this module's own version, or built
// from checkout; the version is what the install step tells the reader.
func toolchainFor(
	checkout, scratch string,
) (string, tour.Toolchain, error) {
	environment := []string{"GOFLAGS=-buildvcs=false"}
	if checkout != "" {
		toolchain, err := fromCheckout(checkout, scratch, environment)
		return "devel", toolchain, err
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || !release.MatchString(info.Main.Version) {
		return "", tour.Toolchain{}, fmt.Errorf("tourgen is at %q, not a "+
			"release: run it as go run %s/cmd/tourgen@vX.Y.Z, at a tagged "+
			"version, or pass -rastrillo", info.Main.Version, module)
	}
	run := []string{"go", "run", module + "/cmd/rastrillo@" +
		info.Main.Version}
	return info.Main.Version, tour.Toolchain{Rastrillo: run,
		Env: environment}, nil
}

// fromCheckout builds rastrillo from a checkout and points each new app
// at it, since the release rastrillo new pins has not been cut yet.
func fromCheckout(
	checkout, scratch string, environment []string,
) (tour.Toolchain, error) {
	root, err := filepath.Abs(checkout)
	if err != nil {
		return tour.Toolchain{}, err
	}
	binary := filepath.Join(scratch, "rastrillo")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", binary,
		"./cmd/rastrillo")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		return tour.Toolchain{}, fmt.Errorf("build rastrillo: %w\n%s",
			err, output)
	}
	toolchain := tour.Toolchain{Rastrillo: []string{binary},
		Env:    append(environment, "GOFLAGS=-mod=mod -buildvcs=false"),
		NewEnv: []string{"GOPROXY=off"}}
	toolchain.Made = func(directory string) error {
		for _, argv := range [][]string{
			{"go", "mod", "edit", "-replace", module + "=" + root},
			{"go", "mod", "tidy"},
			{binary, "generate"}} {
			command := exec.Command(argv[0], argv[1:]...)
			command.Dir = directory
			if output, err := command.CombinedOutput(); err != nil {
				return fmt.Errorf("%s: %w\n%s", strings.Join(argv, " "),
					err, output)
			}
		}
		return nil
	}
	return toolchain, nil
}

// The tour.json the site reads; docs/superpowers/specs/
// 2026-11-03-chat-tour-design.md has its shape.
type page struct {
	Slug    string     `json:"slug"`
	Title   string     `json:"title"`
	Version string     `json:"version"`
	Steps   []pageStep `json:"steps"`
	app     string
}

type pageStep struct {
	Number   int        `json:"number"`
	Slug     string     `json:"slug"`
	Title    string     `json:"title"`
	Lesson   string     `json:"lesson"`
	Check    string     `json:"check"`
	Commands []string   `json:"commands"`
	Files    []pageFile `json:"files"`
	Zip      string     `json:"zip,omitempty"`
}

type pageFile struct {
	Path     string   `json:"path"`
	Status   string   `json:"status"`
	Language string   `json:"language"`
	Content  string   `json:"content"`
	Changed  [][2]int `json:"changed"`
}

func pageOf(chosen tour.Tour, version string) *page {
	return &page{Slug: chosen.Slug, Title: chosen.Title, Version: version,
		Steps: []pageStep{}, app: chosen.App}
}

func (tourPage *page) add(
	step tour.Step, directory string, changes []tour.Change, out string,
) error {
	versioned := strings.NewReplacer("{{version}}", tourPage.Version)
	entry := pageStep{Number: step.Number, Slug: step.Slug,
		Title: step.Title, Lesson: versioned.Replace(step.Lesson),
		Check: versioned.Replace(step.Check), Commands: []string{},
		Files: []pageFile{}}
	for _, command := range step.Commands {
		entry.Commands = append(entry.Commands, versioned.Replace(command))
	}
	for _, change := range changes {
		entry.Files = append(entry.Files, fileOf(change))
	}
	if directory != "" {
		entry.Zip = fmt.Sprintf("%s-%02d-%s.zip", tourPage.Slug, step.Number,
			step.Slug)
		err := zipApp(directory, tourPage.app,
			filepath.Join(out, "zips", entry.Zip))
		if err != nil {
			return err
		}
	}
	tourPage.Steps = append(tourPage.Steps, entry)
	return nil
}

func fileOf(change tour.Change) pageFile {
	status := "new"
	if change.Existed {
		status = "edit"
	}
	return pageFile{Path: change.Path, Status: status,
		Language: languageOf(change.Path), Content: change.New,
		Changed: tour.ChangedLines(change.Old, change.New)}
}

func languageOf(file string) string {
	switch path.Ext(file) {
	case ".go":
		return "go"
	case ".html":
		return "html"
	case ".sql":
		return "sql"
	case ".toml":
		return "toml"
	}
	return "text"
}

func (tourPage *page) save(file string) error {
	data, err := json.MarshalIndent(tourPage, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, append(data, '\n'), 0o644)
}

// zipApp writes the app as a reader's copy would be, under one folder
// named for it, leaving out its database and anything git or a build
// made.
func zipApp(directory, app, file string) error {
	archive, err := os.Create(file)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(archive)
	err = filepath.WalkDir(directory, func(name string, entry fs.DirEntry,
		err error) error {
		if err != nil || name == directory {
			return err
		}
		relative, _ := filepath.Rel(directory, name)
		if leftOut(relative, entry) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		return addFile(writer, name, path.Join(app,
			filepath.ToSlash(relative)))
	})
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	if closeErr := archive.Close(); err == nil {
		err = closeErr
	}
	return err
}

func leftOut(relative string, entry fs.DirEntry) bool {
	base := filepath.Base(relative)
	if entry.IsDir() {
		return base == ".git" || base == ".build" || base == "releases"
	}
	return strings.Contains(base, ".db")
}

func addFile(writer *zip.Writer, name, inZip string) error {
	info, err := os.Stat(name)
	if err != nil {
		return err
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name, header.Method = inZip, zip.Deflate
	target, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	_, err = target.Write(data)
	return err
}
