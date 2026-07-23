// Package resolve implements mechanical (non-AI) item resolution for
// `ktd edit`/`ktd close`/`ktd context`: exact id match, then a
// case-insensitive substring match against title/categories/body.
// Ambiguous or absent matches are left for the caller to report as
// candidates rather than guessed at.
package resolve

import (
	"strconv"
	"strings"

	"ktd/internal/store"
)

// Result is the outcome of resolving a query against the item list.
// Exactly one of Match or Candidates is meaningful:
//   - Match set: exactly one confident match — proceed.
//   - Match nil, len(Candidates) > 1: ambiguous — ask the user to narrow.
//   - Match nil, len(Candidates) == 0: no match at all.
type Result struct {
	Match      *store.Item
	Candidates []store.Item
}

// Resolve finds the item(s) matching query among items, per the
// resolution order documented in the original SKILL.md: exact id match
// (with or without zero-padding) first, then case-insensitive substring
// match against title, categories, and body.
func Resolve(items []store.Item, query string) Result {
	query = strings.TrimSpace(query)
	if id, ok := normalizeID(query); ok {
		for i := range items {
			if items[i].Todo.ID == id {
				return Result{Match: &items[i]}
			}
		}
	}

	needle := strings.ToLower(query)
	var matches []store.Item
	for _, it := range items {
		if matchesText(it, needle) {
			matches = append(matches, it)
		}
	}
	if len(matches) == 1 {
		return Result{Match: &matches[0]}
	}
	return Result{Candidates: matches}
}

// normalizeID reports whether query looks like an id (all digits) and
// returns it zero-padded to 4 digits.
func normalizeID(query string) (string, bool) {
	if query == "" {
		return "", false
	}
	if _, err := strconv.Atoi(query); err != nil {
		return "", false
	}
	if len(query) > 4 {
		return "", false
	}
	padded := strings.Repeat("0", 4-len(query)) + query
	return padded, true
}

func matchesText(it store.Item, lowerNeedle string) bool {
	if strings.Contains(strings.ToLower(it.Todo.Title), lowerNeedle) {
		return true
	}
	for _, c := range it.Todo.Categories {
		if strings.Contains(strings.ToLower(c), lowerNeedle) {
			return true
		}
	}
	if strings.Contains(strings.ToLower(it.Todo.Body), lowerNeedle) {
		return true
	}
	return false
}
