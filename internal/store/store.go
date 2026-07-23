// Package store handles data-directory resolution, git plumbing, and
// reading/writing todo files. File format lives in frontmatter.go.
package store

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"ktd/internal/model"
)

// dataDirEnvOverride lets tests and local dev point at a scratch store
// instead of the real %AppData%\kyle-to-do.
const dataDirEnvOverride = "KTD_DATA_DIR"

// Store wraps a resolved data directory (todos/*.md + a git repo rooted
// there).
type Store struct {
	Dir      string // e.g. %AppData%\kyle-to-do
	TodosDir string // Dir/todos
}

// Open resolves the data directory, creates it (and the todos/
// subdirectory) if missing, and auto-`git init`s it on first run. It also
// ensures a .gitignore containing ".env" exists so a locally-stored API
// key never gets committed by `sync`.
func Open() (*Store, error) {
	dir, err := resolveDataDir()
	if err != nil {
		return nil, err
	}
	todosDir := filepath.Join(dir, "todos")
	if err := os.MkdirAll(todosDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating data dir: %w", err)
	}

	s := &Store{Dir: dir, TodosDir: todosDir}
	if err := s.ensureGitInit(); err != nil {
		return nil, err
	}
	if err := s.ensureGitignore(); err != nil {
		return nil, err
	}
	return s, nil
}

func resolveDataDir() (string, error) {
	if override := os.Getenv(dataDirEnvOverride); override != "" {
		return override, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolving user config dir: %w", err)
	}
	return filepath.Join(base, "kyle-to-do"), nil
}

func (s *Store) ensureGitInit() error {
	if _, err := os.Stat(filepath.Join(s.Dir, ".git")); err == nil {
		return nil
	}
	cmd := exec.Command("git", "init")
	cmd.Dir = s.Dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git init in %s: %w: %s", s.Dir, err, out)
	}
	return nil
}

func (s *Store) ensureGitignore() error {
	path := filepath.Join(s.Dir, ".gitignore")
	existing, err := os.ReadFile(path)
	if err == nil && strings.Contains(string(existing), ".env") {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("writing .gitignore: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(".env\n"); err != nil {
		return fmt.Errorf("writing .gitignore: %w", err)
	}
	return nil
}

// Item pairs a parsed Todo with the file path it was loaded from, so
// callers can save back to the same path.
type Item struct {
	Path string
	Todo *model.Todo
}

// List reads and parses every todos/*.md file. Files that fail to parse
// are skipped with their error reported via the errs return value rather
// than aborting the whole load.
func (s *Store) List() ([]Item, []error) {
	entries, err := os.ReadDir(s.TodosDir)
	if err != nil {
		return nil, []error{fmt.Errorf("reading todos dir: %w", err)}
	}

	var items []Item
	var errs []error
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(s.TodosDir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		t, err := ParseFile(string(raw))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		if t.ID == "" {
			// Fall back to the leading digits of the filename, matching the
			// original list-todos.ps1 behavior for malformed frontmatter.
			t.ID = strings.TrimSuffix(e.Name(), ".md")
		}
		if t.Status == "" {
			t.Status = "open"
		}
		items = append(items, Item{Path: path, Todo: t})
	}
	return items, errs
}

var idPrefixRe = regexp.MustCompile(`^(\d+)`)

// NextID returns the next 4-digit zero-padded id: the highest numeric
// filename prefix currently in todos/, plus one ("0001" if empty).
func (s *Store) NextID() (string, error) {
	entries, err := os.ReadDir(s.TodosDir)
	if err != nil {
		return "", fmt.Errorf("reading todos dir: %w", err)
	}
	max := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := idPrefixRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	return fmt.Sprintf("%04d", max+1), nil
}

var slugNonAlnumRe = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify kebab-cases a title for use in a filename. Purely cosmetic — the
// id (not the slug) is a todo's identity.
func Slugify(title string) string {
	s := strings.ToLower(title)
	s = slugNonAlnumRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "untitled"
	}
	return s
}

// pathFor builds the todos/{id}-{slug}.md path for a todo.
func (s *Store) pathFor(t *model.Todo) string {
	name := fmt.Sprintf("%s-%s.md", t.ID, Slugify(t.Title))
	return filepath.Join(s.TodosDir, name)
}

// Save canonically re-serializes t and writes it to todos/{id}-{slug}.md.
// If an existing file for this id has a different slug (the title
// changed), the old file is removed so there's never more than one file
// per id.
func (s *Store) Save(t *model.Todo) error {
	newPath := s.pathFor(t)

	if old, ok := s.findExistingPath(t.ID); ok && old != newPath {
		if err := os.Remove(old); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing stale file for %s: %w", t.ID, err)
		}
	}

	content, err := Serialize(t)
	if err != nil {
		return fmt.Errorf("serializing %s: %w", t.ID, err)
	}
	if err := os.WriteFile(newPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", newPath, err)
	}
	return nil
}

func (s *Store) findExistingPath(id string) (string, bool) {
	entries, err := os.ReadDir(s.TodosDir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), id+"-") || e.Name() == id+".md" {
			return filepath.Join(s.TodosDir, e.Name()), true
		}
	}
	return "", false
}

// AllCategories returns every category string currently in use, for
// passing to the AI as context (see internal/categories for canonical
// casing).
func AllCategories(items []Item) []string {
	seen := map[string]bool{}
	var out []string
	for _, it := range items {
		for _, c := range it.Todo.Categories {
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	sort.Strings(out)
	return out
}
