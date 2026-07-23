package commands

import (
	"fmt"
	"sort"

	"ktd/internal/categories"
	"ktd/internal/model"
	"ktd/internal/resolve"
	"ktd/internal/store"
)

// resolveOne resolves query against items and reports (to stdout)
// candidates/no-match diagnostics itself when it can't land on exactly one
// confident match, matching SKILL.md's "never guess silently" resolution
// contract. Returns ok=false when the caller should stop.
func resolveOne(items []store.Item, query string, canon categories.CanonicalMap) (*store.Item, bool) {
	res := resolve.Resolve(items, query)
	if res.Match != nil {
		fmt.Printf("%s — %s\n", res.Match.Todo.ID, res.Match.Todo.Title)
		return res.Match, true
	}

	if len(res.Candidates) == 0 {
		fmt.Printf("No item matches %q.\n", query)
		printClosestOpen(items, canon)
		return nil, false
	}

	fmt.Printf("Multiple items match %q — re-run with a tighter match:\n", query)
	for _, c := range res.Candidates {
		fmt.Println("  " + formatCandidate(c.Todo, canon))
	}
	return nil, false
}

func formatCandidate(t *model.Todo, canon categories.CanonicalMap) string {
	return fmt.Sprintf("%s — %s %s (%s)", t.ID, t.Title, canon.FormatList(t.Categories), t.Status)
}

func printClosestOpen(items []store.Item, canon categories.CanonicalMap) {
	var open []store.Item
	for _, it := range items {
		if it.Todo.IsOpen() {
			open = append(open, it)
		}
	}
	sort.Slice(open, func(i, j int) bool { return open[i].Todo.Created > open[j].Todo.Created })

	n := 5
	if len(open) < n {
		n = len(open)
	}
	if n == 0 {
		return
	}
	fmt.Println("Closest open items:")
	for _, it := range open[:n] {
		fmt.Println("  " + formatCandidate(it.Todo, canon))
	}
}
