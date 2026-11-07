package commands

import (
	"fmt"
	"strings"

	"ktd/internal/github"
	"ktd/internal/model"
	"ktd/internal/store"
)

// linkKey normalizes a URL for duplicate detection: GitHub issue/PR links
// reduce to owner/repo/kind/number so ".../pull/76/files" matches
// ".../pull/76"; anything else is compared without a trailing slash.
func linkKey(url string) string {
	if refs := github.DetectRefs([]string{url}); len(refs) > 0 {
		r := refs[0]
		return strings.ToLower(fmt.Sprintf("github:%s/%s/%s/%s", r.Owner, r.Repo, r.Kind, r.Number))
	}
	return strings.TrimRight(url, "/")
}

func hasLink(links []string, url string) bool {
	key := linkKey(url)
	for _, l := range links {
		if linkKey(l) == key {
			return true
		}
	}
	return false
}

// findByLinks returns the first stored item that already carries any of
// links, or nil.
func findByLinks(items []store.Item, links []string) *model.Todo {
	for _, l := range links {
		for _, it := range items {
			if hasLink(it.Todo.Links, l) {
				return it.Todo
			}
		}
	}
	return nil
}

type existingChoice int

const (
	choiceSkip existingChoice = iota
	choiceUpdate
	choiceAddNew
)

// askExisting asks what to do when existing (an item already carrying one
// of the input's links) matches: update it, add a new item anyway, or skip.
// Blank means skip, so a stray Enter never writes anything.
func askExisting(existing *model.Todo) existingChoice {
	fmt.Printf("🔁 %s — %s (%s) already has this link.\n", existing.ID, existing.Title, existing.Status)
	fmt.Println("  1. Update existing item")
	fmt.Println("  2. Add a new item")
	fmt.Println("  3. Do nothing")
	for {
		fmt.Print("Choose [1-3, default 3] ")
		switch readLine() {
		case "1":
			return choiceUpdate
		case "2":
			return choiceAddNew
		case "", "3":
			return choiceSkip
		}
		fmt.Println("  invalid choice")
	}
}
