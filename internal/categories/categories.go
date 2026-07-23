// Package categories implements case-insensitive category canonicalization:
// categories are freeform and case-insensitive everywhere, and the casing
// shown to the user is whichever variant is used most often across all
// items (ties broken alphabetically), matching list-todos.ps1's
// Get-CanonicalCategory/Register-CategoryCasing behavior.
package categories

import (
	"sort"
	"strings"
)

// CanonicalMap maps a lowercased category key to its canonical display
// casing.
type CanonicalMap map[string]string

// Build tallies casing usage across every item's category list and
// resolves, for each lowercased category, the most-frequently-used exact
// casing (ties broken alphabetically ascending by casing string).
func Build(perItemCategories [][]string) CanonicalMap {
	counts := map[string]map[string]int{} // lower -> casing -> count
	for _, cats := range perItemCategories {
		for _, c := range cats {
			lower := lower(c)
			if counts[lower] == nil {
				counts[lower] = map[string]int{}
			}
			counts[lower][c]++
		}
	}

	canon := CanonicalMap{}
	for lowerKey, casings := range counts {
		var best string
		bestCount := -1
		var variants []string
		for casing := range casings {
			variants = append(variants, casing)
		}
		sort.Strings(variants) // alphabetical tie-break
		for _, casing := range variants {
			if casings[casing] > bestCount {
				bestCount = casings[casing]
				best = casing
			}
		}
		canon[lowerKey] = best
	}
	return canon
}

// Canonical returns the canonical display casing for a category, falling
// back to the input unchanged if it's not in the map.
func (m CanonicalMap) Canonical(category string) string {
	if c, ok := m[lower(category)]; ok {
		return c
	}
	return category
}

// FormatList canonicalizes, dedupes, and formats a category list as
// "[a, b]", or "[No category]" when empty.
func (m CanonicalMap) FormatList(cats []string) string {
	if len(cats) == 0 {
		return "[No category]"
	}
	seen := map[string]bool{}
	var out []string
	for _, c := range cats {
		canon := m.Canonical(c)
		key := lower(canon)
		if !seen[key] {
			seen[key] = true
			out = append(out, canon)
		}
	}
	s := "["
	for i, c := range out {
		if i > 0 {
			s += ", "
		}
		s += c
	}
	s += "]"
	return s
}

func lower(s string) string {
	return strings.ToLower(s)
}
