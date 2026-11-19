// Package brief reads the app requests a round runs, and the framing
// each arm puts around them.
package brief

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

type Arm string

const (
	Carlos  Arm = "carlos"
	Control Arm = "control"
	// Spec builds unguided but must meet spec.carlosframework.com: what
	// meeting the CARLOS bar costs without the framework.
	Spec Arm = "spec"
)

type Page struct {
	Role, Description string
	SignedIn          bool
}

type Brief struct {
	Name, Set      string // Set: "dev", "heldout", "smoke"
	Text, Followup string
	Framing        map[Arm]string
	Pages          []Page
	Words          []string
}

type Vars struct{ App, Platform, SkillURL, DeployAccount string }

// Load reads one brief directory. The framing comes from the bench's
// arms/ directory, not the brief: an arm says the same thing around
// every brief, so a difference between two briefs is always the app.
func Load(dir string) (Brief, error) {
	read := func(name string) (string, error) {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		return strings.TrimSpace(string(raw)), err
	}
	b := Brief{Name: filepath.Base(dir), Framing: map[Arm]string{}}
	var err error
	if b.Set, err = read("set.txt"); err != nil {
		return b, err
	}
	if b.Text, err = read("brief.txt"); err != nil {
		return b, err
	}
	if b.Followup, err = read("followup.txt"); err != nil {
		return b, err
	}
	pages, err := read("pages.txt")
	if err != nil {
		return b, err
	}
	if b.Pages, err = parsePages(pages); err != nil {
		return b, fmt.Errorf("%s/pages.txt: %w", dir, err)
	}
	words, err := read("words.txt")
	if err != nil {
		return b, err
	}
	b.Words = strings.Fields(words)
	for _, arm := range []Arm{Carlos, Control, Spec} {
		path := filepath.Join(dir, "..", "..", "arms", string(arm)+".txt")
		raw, err := os.ReadFile(path)
		if err != nil {
			return b, err
		}
		b.Framing[arm] = strings.TrimSpace(string(raw))
	}
	return b, nil
}

// parsePages reads lines of "role | yes or no | description".
func parsePages(text string) ([]Page, error) {
	var pages []Page
	for _, line := range strings.Split(text, "\n") {
		fields := strings.SplitN(line, "|", 3)
		if len(fields) != 3 {
			return nil, fmt.Errorf("want role | yes/no | description: %q",
				line)
		}
		signedIn := strings.TrimSpace(fields[1])
		if signedIn != "yes" && signedIn != "no" {
			return nil, fmt.Errorf("signed-in must be yes or no: %q", line)
		}
		pages = append(pages, Page{
			Role:        strings.TrimSpace(fields[0]),
			SignedIn:    signedIn == "yes",
			Description: strings.TrimSpace(fields[2]),
		})
	}
	return pages, nil
}

func LoadAll(root string) ([]Brief, error) {
	dirs, err := filepath.Glob(filepath.Join(root, "briefs", "*"))
	if err != nil {
		return nil, err
	}
	sort.Strings(dirs)
	var briefs []Brief
	for _, dir := range dirs {
		b, err := Load(dir)
		if err != nil {
			return nil, err
		}
		briefs = append(briefs, b)
	}
	return briefs, nil
}

func (b Brief) Prompt(arm Arm, vars Vars) (string, error) {
	framing, known := b.Framing[arm]
	if !known {
		return "", fmt.Errorf("no framing for arm %q", arm)
	}
	filled, err := fill(framing, vars)
	return b.Text + "\n\n" + filled, err
}

func Env(root string, vars Vars) (string, error) {
	raw, err := os.ReadFile(filepath.Join(root, "env.txt"))
	if err != nil {
		return "", err
	}
	return fill(strings.TrimSpace(string(raw)), vars)
}

// Mapping is the question asked of a finished session: where each page
// role ended up, and how to sign in.
func Mapping(root string, b Brief) (string, error) {
	raw, err := os.ReadFile(filepath.Join(root, "mapping.txt"))
	if err != nil {
		return "", err
	}
	needsSignIn := false
	for _, page := range b.Pages {
		needsSignIn = needsSignIn || page.SignedIn
	}
	return fill(strings.TrimSpace(string(raw)), struct {
		Pages       []Page
		NeedsSignIn bool
	}{b.Pages, needsSignIn})
}

func fill(text string, data any) (string, error) {
	parsed, err := template.New("").Option("missingkey=error").Parse(text)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	err = parsed.Execute(&out, data)
	return out.String(), err
}
