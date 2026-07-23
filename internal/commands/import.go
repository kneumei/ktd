package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ktd/internal/store"
)

// Import runs `ktd import <old-todos-dir>`: one-time migration from the
// original todo-list repo's todos/*.md into this store, preserving ids
// and content (re-serialized canonically — see store.Serialize).
func Import(s *store.Store, oldDir string) error {
	entries, err := os.ReadDir(oldDir)
	if err != nil {
		return fmt.Errorf("reading %s: %w", oldDir, err)
	}

	var errs []error
	count := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(oldDir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		t, err := store.ParseFile(string(raw))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		if t.ID == "" {
			name := strings.TrimSuffix(e.Name(), ".md")
			if len(name) >= 4 {
				t.ID = name[:4]
			} else {
				t.ID = name
			}
		}
		if t.Status == "" {
			t.Status = "open"
		}
		if err := s.Save(t); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		count++
	}

	fmt.Printf("Imported %d item(s) into %s\n", count, s.TodosDir)
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "warning: %v\n", e)
	}
	if len(errs) > 0 {
		return fmt.Errorf("%d file(s) failed to import", len(errs))
	}
	return nil
}
