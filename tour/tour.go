// Package tour holds rastrillo.org's step-by-step tours and replays
// them: each step's files written into a new app and its commands run,
// as a reader following the tour does. A test replays every step, so
// the code a tour shows always builds and passes on this checkout.
package tour

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// The _files directories hold Go that belongs to the tour's app, not to
// this module; Go skips a directory whose name starts with _.
//
//go:embed all:chat
var content embed.FS

// Tour is one tour: the app it builds and the steps that build it.
type Tour struct {
	Slug  string
	Title string
	// App is the name the scaffold step's rastrillo new gives the app.
	App   string
	Steps []Step
}

// Step is one page of a tour. Files are the paths, from the app's root,
// that the reader types or changes; Commands are what they run, and Run
// what a replay runs of those (installing and serving are not replayed).
type Step struct {
	Number   int
	Slug     string
	Title    string
	Lesson   string
	Check    string
	Commands []string
	Run      []string
	Files    []string
	// Scaffold marks the step whose commands make the app.
	Scaffold bool
}

// Load reads the tour named slug.
func Load(slug string) (Tour, error) {
	tour := Tour{Slug: slug}
	meta, err := fs.ReadFile(content, path.Join(slug, "tour.toml"))
	if err != nil {
		return tour, fmt.Errorf("tour %q: %w", slug, err)
	}
	if err := toml.Unmarshal(meta, &tour); err != nil {
		return tour, fmt.Errorf("tour %q: tour.toml: %w", slug, err)
	}
	names, err := fs.Glob(content, path.Join(slug, "[0-9][0-9]-*.md"))
	if err != nil {
		return tour, err
	}
	slices.Sort(names)
	for index, name := range names {
		step, err := readStep(name)
		if err != nil {
			return tour, err
		}
		if step.Number != index+1 {
			return tour, fmt.Errorf("%s: step %d follows step %d; number "+
				"the steps 01, 02 and on with no gap", name, step.Number,
				index)
		}
		tour.Steps = append(tour.Steps, step)
	}
	return tour, tour.check()
}

// stepName is a step file's name: its number and its slug.
var stepName = regexp.MustCompile(`^(\d\d)-([a-z0-9-]+)\.md$`)

func readStep(name string) (Step, error) {
	parts := stepName.FindStringSubmatch(path.Base(name))
	if parts == nil {
		return Step{}, fmt.Errorf("%s: name a step NN-slug.md", name)
	}
	source, err := fs.ReadFile(content, name)
	if err != nil {
		return Step{}, err
	}
	front, lesson, found := cutFrontMatter(source)
	if !found {
		return Step{}, fmt.Errorf("%s: start it with +++ front matter", name)
	}
	var step Step
	if err := toml.Unmarshal(front, &step); err != nil {
		return Step{}, fmt.Errorf("%s: %w", name, err)
	}
	step.Number, _ = strconv.Atoi(parts[1])
	step.Slug, step.Lesson = parts[2], strings.TrimSpace(string(lesson))
	return step, nil
}

func cutFrontMatter(source []byte) (front, rest []byte, found bool) {
	const fence = "+++\n"
	if !bytes.HasPrefix(source, []byte(fence)) {
		return nil, nil, false
	}
	return bytes.Cut(source[len(fence):], []byte("\n"+fence))
}

// check refuses a tour a reader could not follow: no scaffold step, or a
// step naming a file the tour does not hold.
func (tour Tour) check() error {
	scaffolds := 0
	for _, step := range tour.Steps {
		if step.Scaffold {
			scaffolds++
		}
		if step.Title == "" {
			return fmt.Errorf("tour %s step %d has no title", tour.Slug,
				step.Number)
		}
		for _, file := range step.Files {
			if _, err := tour.File(step, file); err != nil {
				return err
			}
		}
	}
	if scaffolds != 1 {
		return fmt.Errorf("tour %s has %d scaffold steps, want 1",
			tour.Slug, scaffolds)
	}
	return nil
}

// newCommand is the scaffold step's rastrillo new, exactly as the reader
// is shown it: the replay runs that line, so a command a reader cannot
// run fails the tour's test.
func (tour Tour) newCommand(step Step) (string, error) {
	for _, command := range step.Commands {
		words := strings.Fields(command)
		if len(words) > 2 && words[0] == "rastrillo" && words[1] == "new" &&
			words[len(words)-1] == tour.App {
			return command, nil
		}
	}
	return "", fmt.Errorf("tour %s step %d makes the app but shows no "+
		"rastrillo new ending in %s", tour.Slug, step.Number, tour.App)
}

// File is the whole text of file as step leaves it.
func (tour Tour) File(step Step, file string) (string, error) {
	name := path.Join(tour.Slug, "_files", fmt.Sprintf("%02d", step.Number),
		file)
	text, err := fs.ReadFile(content, name)
	if err != nil {
		return "", fmt.Errorf("tour %s step %d names %s, but %s is "+
			"missing: %w", tour.Slug, step.Number, file, name, err)
	}
	return string(text), nil
}
