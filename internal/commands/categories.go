package commands

import (
	"strings"

	"ktd/internal/categories"
	"ktd/internal/store"
)

// chooseCategories decides what categories a newly-parsed item gets: the
// ones the user's own text stated, when there are any, otherwise the ones
// the AI inferred. A stated list is the complete answer, never a starting
// point to supplement — that rule lives here rather than in the prompt
// because asking the model to hold back its own suggestions didn't stick
// (`ktd add "<github link>, category=mdmd"` kept coming back tagged
// [mdmd, Consumers]). Reporting what the user said and deciding what to do
// about it are now separate jobs: the model only transcribes, this
// function enforces. Stated values are canonicalized against the casing
// already in use, so "mdmd" joins "MDMD" instead of forking it.
func chooseCategories(canon categories.CanonicalMap, stated, inferred []string) []string {
	if len(stated) == 0 {
		return inferred
	}
	out := make([]string, 0, len(stated))
	seen := map[string]bool{}
	for _, c := range stated {
		c = canon.Canonical(c)
		if key := strings.ToLower(c); !seen[key] {
			seen[key] = true
			out = append(out, c)
		}
	}
	return out
}

// canonMap builds the canonical-casing map from every stored item's
// categories, matching what list/context printing uses.
func canonMap(items []store.Item) categories.CanonicalMap {
	perItem := make([][]string, 0, len(items))
	for _, it := range items {
		perItem = append(perItem, it.Todo.Categories)
	}
	return categories.Build(perItem)
}
