package commands

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"ktd/internal/categories"
	"ktd/internal/model"
	"ktd/internal/store"
)

func TestListSinceCombinesSinceAndSearch(t *testing.T) {
	items := []store.Item{
		{Todo: &model.Todo{ID: "0001", Title: "Advent migration", Status: "closed", Created: "2026-09-20", Closed: "2026-10-02"}},
		{Todo: &model.Todo{ID: "0002", Title: "Unrelated recent work", Status: "closed", Created: "2026-09-20", Closed: "2026-10-02"}},
		{Todo: &model.Todo{ID: "0003", Title: "Old Advent work", Status: "closed", Created: "2026-09-01", Closed: "2026-09-15"}},
	}
	opts := ListOptions{Status: "all", Since: 5, Search: "advent", NoColor: true}

	output := captureStdout(t, func() {
		if err := listSince(items, opts, categories.Build(nil), false, time.Date(2026, 10, 2, 12, 0, 0, 0, time.Local)); err != nil {
			t.Fatalf("listSince() error = %v", err)
		}
	})

	if !strings.Contains(output, "Advent migration") {
		t.Errorf("output did not contain the matching in-window item:\n%s", output)
	}
	if strings.Contains(output, "Unrelated recent work") {
		t.Errorf("output contained an in-window item that did not match --search:\n%s", output)
	}
	if strings.Contains(output, "Old Advent work") {
		t.Errorf("output contained a matching item outside --since:\n%s", output)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	original := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = original })

	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = original

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return string(out)
}
