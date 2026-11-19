package tour

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Toolchain runs the two programs a reader runs, rastrillo and go.
type Toolchain struct {
	// Rastrillo is the command that runs rastrillo: a built binary, or
	// go run with the module at a version.
	Rastrillo []string
	// Env is added to every command's environment, and NewEnv to
	// rastrillo new's after it.
	Env, NewEnv []string
	// Made, when set, runs once on the new app before its first step; a
	// test points the app at this checkout here.
	Made func(directory string) error
}

// Change is one file a step wrote. Old is "" for a file that is new.
type Change struct {
	Path     string
	Old, New string
	Existed  bool
}

// Played hears each step once it is done. Directory is the app, or ""
// for a step before the scaffold, when there is no app yet.
type Played func(step Step, directory string, changes []Change) error

// Replay makes the tour's app in parent and plays each step on it, as a
// reader does, then compiles the templates as rastrillo dev would.
func (tour Tour) Replay(
	parent string, toolchain Toolchain, played Played,
) error {
	directory := ""
	for _, step := range tour.Steps {
		if step.Scaffold {
			made, err := tour.scaffold(step, parent, toolchain)
			if err != nil {
				return err
			}
			directory = made
		}
		changes, err := tour.play(step, directory, toolchain)
		if err != nil {
			return fmt.Errorf("step %d, %s: %w", step.Number, step.Title,
				err)
		}
		if err := played(step, directory, changes); err != nil {
			return fmt.Errorf("step %d, %s: %w", step.Number, step.Title,
				err)
		}
	}
	return nil
}

func (tour Tour) scaffold(
	step Step, parent string, toolchain Toolchain,
) (string, error) {
	command, err := tour.newCommand(step)
	if err != nil {
		return "", err
	}
	making := toolchain
	making.Env = append(append([]string{}, toolchain.Env...),
		toolchain.NewEnv...)
	if err := making.command(parent, command); err != nil {
		return "", err
	}
	directory := filepath.Join(parent, tour.App)
	if toolchain.Made != nil {
		if err := toolchain.Made(directory); err != nil {
			return "", err
		}
	}
	return directory, nil
}

func (tour Tour) play(
	step Step, directory string, toolchain Toolchain,
) ([]Change, error) {
	if directory == "" {
		if len(step.Files) > 0 || len(step.Run) > 0 {
			return nil, errors.New("it changes the app before the app " +
				"is made")
		}
		return nil, nil
	}
	var changes []Change
	for _, file := range step.Files {
		change, err := tour.write(step, directory, file)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	for _, command := range step.Run {
		if err := toolchain.command(directory, command); err != nil {
			return nil, err
		}
	}
	if len(changes) == 0 {
		return nil, nil
	}
	return changes, toolchain.rastrillo(directory, "generate")
}

func (tour Tour) write(step Step, directory, file string) (Change, error) {
	text, err := tour.File(step, file)
	if err != nil {
		return Change{}, err
	}
	target := filepath.Join(directory, filepath.FromSlash(file))
	old, err := os.ReadFile(target)
	existed := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Change{}, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return Change{}, err
	}
	change := Change{Path: file, Old: string(old), New: text,
		Existed: existed}
	return change, os.WriteFile(target, []byte(text), 0o644)
}

// command runs one of a step's commands: rastrillo or go, words split
// on spaces, as the tour writes them.
func (toolchain Toolchain) command(directory, command string) error {
	words := strings.Fields(command)
	switch {
	case len(words) > 1 && words[0] == "rastrillo":
		return toolchain.rastrillo(directory, words[1:]...)
	case len(words) > 1 && words[0] == "go":
		return toolchain.run(directory, "go", words[1:]...)
	}
	return fmt.Errorf("cannot replay %q: a replayed command starts "+
		"with rastrillo or go", command)
}

func (toolchain Toolchain) rastrillo(
	directory string, arguments ...string,
) error {
	argv := append(append([]string{}, toolchain.Rastrillo...), arguments...)
	return toolchain.run(directory, argv[0], argv[1:]...)
}

func (toolchain Toolchain) run(
	directory, program string, arguments ...string,
) error {
	command := exec.Command(program, arguments...)
	command.Dir = directory
	command.Env = append(os.Environ(), toolchain.Env...)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %w\n%s", program,
			strings.Join(arguments, " "), err, output)
	}
	return nil
}
