package prompt

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/chzyer/readline"
)

type pathCompleter struct{}

func (p *pathCompleter) Do(line []rune, pos int) ([][]rune, int) {
	input := string(line[:pos])

	// Expand ~ if present
	expanded := input
	if strings.HasPrefix(input, "~/") {
		u, err := user.Current()
		if err == nil {
			expanded = filepath.Join(u.HomeDir, input[2:])
		}
	} else if input == "~" {
		u, err := user.Current()
		if err == nil {
			expanded = u.HomeDir
		}
	}

	var dir, prefix string
	if strings.HasSuffix(expanded, "/") {
		dir = expanded
		prefix = ""
	} else {
		dir = filepath.Dir(expanded)
		prefix = filepath.Base(expanded)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0
	}

	var candidates [][]rune
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, prefix) {
			suffix := name[len(prefix):] + "/"
			candidates = append(candidates, []rune(suffix))
		}
	}

	return candidates, len([]rune(prefix))
}

// AskPath prompts the user for a directory path with tab completion.
func AskPath(promptStr string) (string, error) {
	rl, err := readline.NewEx(&readline.Config{
		Prompt:       promptStr,
		AutoComplete: &pathCompleter{},
	})
	if err != nil {
		return "", err
	}
	defer rl.Close()

	line, err := rl.Readline()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// ResolvePath validates and resolves a path to an absolute directory.
func ResolvePath(path string) (string, error) {
	if path == "" {
		return "", errors.New("path must not be empty")
	}

	// Expand ~/...
	if strings.HasPrefix(path, "~/") {
		u, err := user.Current()
		if err != nil {
			return "", err
		}
		path = filepath.Join(u.HomeDir, path[2:])
	} else if path == "~" {
		u, err := user.Current()
		if err != nil {
			return "", err
		}
		path = u.HomeDir
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("path is not a directory: " + abs)
	}

	return abs, nil
}
