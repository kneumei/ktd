package commands

import (
	"fmt"
	"os"
	"time"

	"ktd/internal/categories"
	"ktd/internal/store"
)

// Close runs `ktd close <id|text> [--as-of YYYY-MM-DD]`: resolve the item
// (mechanically — no AI) and set status/closed. No AI involved, matching
// the original SKILL.md close behavior.
func Close(s *store.Store, query, asOf string) error {
	items, errs := s.List()
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "warning: %v\n", e)
	}

	var perItemCats [][]string
	for _, it := range items {
		perItemCats = append(perItemCats, it.Todo.Categories)
	}
	canon := categories.Build(perItemCats)

	it, ok := resolveOne(items, query, canon)
	if !ok {
		return nil
	}

	closedDate, err := resolveAsOfDate(asOf)
	if err != nil {
		return err
	}

	if it.Todo.Status == "closed" {
		fmt.Printf("%s is already closed (closed %s).\n", it.Todo.ID, it.Todo.Closed)
		return nil
	}

	it.Todo.Status = "closed"
	it.Todo.Closed = closedDate
	if err := s.Save(it.Todo); err != nil {
		return err
	}

	fmt.Printf("Closed %s — %s (closed %s).\n", it.Todo.ID, it.Todo.Title, closedDate)
	return nil
}

// resolveAsOfDate returns asOf validated as YYYY-MM-DD, or today's date if
// asOf is empty.
func resolveAsOfDate(asOf string) (string, error) {
	if asOf == "" {
		return time.Now().Format("2006-01-02"), nil
	}
	if _, err := time.Parse("2006-01-02", asOf); err != nil {
		return "", fmt.Errorf("invalid --as-of date %q: expected YYYY-MM-DD", asOf)
	}
	return asOf, nil
}
