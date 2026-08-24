// Package app provides a file-system abstraction for project scanning.
// Inspired by Railpack's app.App — instead of calling os.Stat() everywhere,
// providers use a clean API: HasFile(), ReadJSON(), FindFiles(), etc.
package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// App represents a scanned project directory, pre-indexed for fast lookups.
type App struct {
	Source string            // Absolute path to project root
	files  map[string]bool   // Pre-scanned file index (relative paths → true)
	dirs   map[string]bool   // Pre-scanned directory index
}

// NewApp scans the project directory and builds an index of all files.
// It skips common ignore directories (node_modules, .git, .venv, etc.)
func NewApp(source string) (*App, error) {
	absSource, err := filepath.Abs(source)
	if err != nil {
		return nil, err
	}

	app := &App{
		Source: absSource,
		files:  make(map[string]bool),
		dirs:   make(map[string]bool),
	}

	skipDirs := map[string]bool{
		"node_modules": true, ".git": true, ".venv": true, "venv": true,
		".idlistack": true, "__pycache__": true, ".next": true,
		"target": true, ".cargo": true, ".gradle": true,
		"dist": true, "build": true, "vendor": true,
		".pnpm-store": true,
	}

	err = filepath.Walk(absSource, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		rel, _ := filepath.Rel(absSource, path)
		if rel == "." {
			return nil
		}

		if info.IsDir() {
			base := filepath.Base(rel)
			if skipDirs[base] {
				return filepath.SkipDir
			}
			app.dirs[rel] = true
			return nil
		}

		app.files[rel] = true
		return nil
	})

	return app, err
}

// HasFile checks if a file exists in the project (relative path).
func (a *App) HasFile(name string) bool {
	if a.files[name] {
		return true
	}
	// Fallback for symlinks or unscanned files
	info, err := os.Stat(filepath.Join(a.Source, name))
	if err == nil && !info.IsDir() {
		return true
	}
	return false
}

// HasDir checks if a directory exists in the project (relative path).
func (a *App) HasDir(name string) bool {
	return a.dirs[name]
}

// HasMatch checks if any file matches a simple glob pattern (e.g., "*.csproj").
func (a *App) HasMatch(pattern string) bool {
	for f := range a.files {
		if matched, _ := filepath.Match(pattern, filepath.Base(f)); matched {
			return true
		}
	}
	return false
}

// FindFiles returns all files matching a glob pattern against their base name.
func (a *App) FindFiles(pattern string) ([]string, error) {
	var matches []string
	for f := range a.files {
		if matched, _ := filepath.Match(pattern, filepath.Base(f)); matched {
			matches = append(matches, f)
		}
	}
	return matches, nil
}

// ReadFile reads a file's content from the project directory.
func (a *App) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join(a.Source, name))
}

// ReadJSON reads a JSON file and unmarshals it into the given struct.
func (a *App) ReadJSON(name string, v any) error {
	data, err := a.ReadFile(name)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// ReadFileString reads a file and returns its content as a string.
func (a *App) ReadFileString(name string) (string, error) {
	data, err := a.ReadFile(name)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// HasFileWithContent checks if a file exists and contains a specific substring.
func (a *App) HasFileWithContent(name, substring string) bool {
	if !a.HasFile(name) {
		return false
	}
	content, err := a.ReadFileString(name)
	if err != nil {
		return false
	}
	return strings.Contains(content, substring)
}

// ListFiles returns all indexed file paths.
func (a *App) ListFiles() []string {
	result := make([]string, 0, len(a.files))
	for f := range a.files {
		result = append(result, f)
	}
	return result
}

// FilePath returns the absolute path for a relative project file.
func (a *App) FilePath(name string) string {
	return filepath.Join(a.Source, name)
}
