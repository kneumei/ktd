package commands

import (
	"fmt"
	"os"
	"time"

	"ktd/internal/categories"
	"ktd/internal/store"
)

// Context runs `ktd context <id|text>`: resolve the item and print its
// full, untruncated detail.
func Context(s *store.Store, query string) error {
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

	useColor := isTerminal(os.Stdout)
	fmt.Println()
	writeItemVerbose(it.Todo, canon, useColor, false, time.Now())
	return nil
}
