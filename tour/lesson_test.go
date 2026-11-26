package tour

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// lessonMatches holds a step's words to the app as it stands after the
// step: every file it names is there, every rastrillo command it shows
// is one the CLI has, with flags it takes, and every Go name it mentions
// is in the app's code. The replay proves the code; this proves the
// prose around it, which is what drifts when the framework moves.
func lessonMatches(step Step, app string, toolchain Toolchain) error {
	text := step.Lesson + "\n" + step.Check
	spans := codeSpan.FindAllStringSubmatch(text, -1)
	var problems []string
	for _, command := range step.Commands {
		spans = append(spans, []string{"", command})
	}
	source, err := appSource(app)
	if err != nil {
		return err
	}
	for _, span := range spans {
		problem := checkSpan(span[1], app, source, toolchain)
		if problem != "" {
			problems = append(problems, problem)
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("the lesson no longer matches the app:\n  %s",
			strings.Join(problems, "\n  "))
	}
	return nil
}

var (
	codeSpan = regexp.MustCompile("`([^`\n]+)`")
	goName   = regexp.MustCompile(
		`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Z][A-Za-z0-9_]*)?$`)
	looksLikeA = regexp.MustCompile(`^[a-z0-9_.*/-]+\.(go|html|sql|md|toml)$` +
		`|/$|^(cmd|internal|Makefile|AGENTS\.md)$`)
)

// notNames are spans in backticks that are words, not names in the
// code: values, wire names and the like.
var notNames = []string{"ok", "chat", "ls"}

func checkSpan(
	span, app, source string, toolchain Toolchain,
) string {
	switch {
	case strings.HasPrefix(span, "rastrillo "):
		return checkCommand(span, toolchain)
	case looksLikeA.MatchString(span):
		if !pathExists(app, span) {
			return fmt.Sprintf("%s is not in the app", span)
		}
	case goName.MatchString(span) && !slices.Contains(notNames, span):
		name := span[strings.LastIndex(span, ".")+1:]
		if !regexp.MustCompile(`\b` + name + `\b`).MatchString(source) {
			return fmt.Sprintf("%s is named but not in the app's code", span)
		}
	}
	return ""
}

// checkCommand holds a shown rastrillo command to the CLI: its
// subcommand is listed in the usage, so is a second word the usage
// lists under it (migration generate), and each flag is in the -help
// of the two together.
func checkCommand(command string, toolchain Toolchain) string {
	words := strings.Fields(command)
	usage := toolchain.output("")
	if !strings.Contains(usage, "rastrillo "+words[1]) {
		return fmt.Sprintf("%q: rastrillo has no %s command", command,
			words[1])
	}
	path := words[1:2]
	if len(words) > 2 && isSubcommand(usage, words[1], words[2]) {
		path = words[1:3]
	}
	// Some subcommands print flag help, some only their usage line.
	help := toolchain.output(append(slices.Clone(path), "-help")...) +
		usageOf(usage, "rastrillo "+strings.Join(path, " "))
	for _, word := range words[1+len(path):] {
		flag, found := strings.CutPrefix(word, "-")
		if !found {
			continue
		}
		flag = strings.TrimPrefix(flag, "-")
		flag, _, _ = strings.Cut(flag, "=")
		takes := regexp.MustCompile(`(^|[\s\[|])--?` +
			regexp.QuoteMeta(flag) + `([\s\]|=]|$)`)
		if !takes.MatchString(help) {
			return fmt.Sprintf("%q: rastrillo %s takes no -%s", command,
				strings.Join(path, " "), flag)
		}
	}
	return ""
}

// usageOf is the usage's lines that start with command.
func usageOf(usage, command string) string {
	var lines []string
	for _, line := range strings.Split(usage, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), command+" ") {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// isSubcommand is whether the usage lists word as one of command's own:
// "rastrillo budget code", or indented under "rastrillo migration
// <cmd>". A word it does not list, like new's app name, is an argument.
func isSubcommand(usage, command, word string) bool {
	if strings.Contains(usage, "rastrillo "+command+" "+word+" ") {
		return true
	}
	_, after, found := strings.Cut(usage, "rastrillo "+command+" <cmd>")
	if !found {
		return false
	}
	for _, line := range strings.Split(after, "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "rastrillo") {
			return false
		}
		if fields[0] == word {
			return true
		}
	}
	return false
}

// pathExists finds a path the lesson names: from the app's root, as a
// glob, or ending any path in the app, since a lesson in
// internal/chat/ names `templates/room.html` from there.
func pathExists(app, path string) bool {
	if matches, _ := filepath.Glob(filepath.Join(app, path)); len(matches) > 0 {
		return true
	}
	suffix := "/" + strings.TrimSuffix(path, "/")
	found := false
	filepath.WalkDir(app, func(name string, entry fs.DirEntry,
		err error) error {
		if err == nil && strings.HasSuffix(filepath.ToSlash(name), suffix) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// appSource is the app's own code and templates, joined, for finding
// the names a lesson mentions.
func appSource(app string) (string, error) {
	var source strings.Builder
	err := filepath.WalkDir(app, func(name string, entry fs.DirEntry,
		err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		switch filepath.Ext(name) {
		case ".go", ".html", ".sql":
			data, err := os.ReadFile(name)
			source.Write(data)
			return err
		}
		return nil
	})
	return source.String(), err
}
