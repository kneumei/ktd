package commands

import (
	"strings"

	"ktd/internal/categories"
	"ktd/internal/store"
)

const categoriesEnvName = "KTD_CATEGORIES"

var defaultCategories = []string{"Cadence", "PacificLife", "MDHD", "ExportDatabase"}

// allowedCategories is the comma-separated KTD_CATEGORIES setting, or
// defaultCategories when unset: the set an item's category is inferred
// from when the user doesn't name one.
func allowedCategories(s *store.Store) []string {
	v := s.Setting(categoriesEnvName)
	if v == "" {
		return defaultCategories
	}
	var out []string
	for _, c := range strings.Split(v, ",") {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return defaultCategories
	}
	return out
}

// chooseCategories decides what categories a newly-parsed item gets: the
// ones the user's own text stated, when there are any, otherwise the first
// inferred one that's in the allowed set. A stated list is the complete
// answer, never a starting point to supplement — that rule lives here
// rather than in the prompt because asking the model to hold back its own
// suggestions didn't stick (`ktd add "<github link>, category=mdmd"` kept
// coming back tagged [mdmd, Consumers]). Stated values take the allowed
// set's casing, else the casing already in use, so "mdhd" joins "MDHD"
// instead of forking it.
func chooseCategories(canon categories.CanonicalMap, allowed, stated, inferred []string) []string {
	allowedByKey := make(map[string]string, len(allowed))
	for _, c := range allowed {
		allowedByKey[strings.ToLower(c)] = c
	}

	if len(stated) == 0 {
		for _, c := range inferred {
			if a, ok := allowedByKey[strings.ToLower(strings.TrimSpace(c))]; ok {
				return []string{a}
			}
		}
		return nil
	}

	out := make([]string, 0, len(stated))
	seen := map[string]bool{}
	for _, c := range stated {
		if a, ok := allowedByKey[strings.ToLower(c)]; ok {
			c = a
		} else {
			c = canon.Canonical(c)
		}
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
