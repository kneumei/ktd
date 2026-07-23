// Package commands implements each `ktd` subcommand.
package commands

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"ktd/internal/categories"
	"ktd/internal/model"
	"ktd/internal/store"
)

// Staleness thresholds, matching list-todos.ps1: fresh (green) up to
// FreshDays old, aging (yellow) up to AgingDays old, red beyond that.
const (
	freshDays = 3
	agingDays = 14
)

// ANSI color codes. See useColor below for when these are actually applied.
const (
	colorReset    = "\033[0m"
	colorDarkGray = "\033[90m"
	colorGray     = "\033[37m"
	colorCyan     = "\033[36m"
	colorYellow   = "\033[33m"
	colorGreen    = "\033[32m"
	colorRed      = "\033[31m"
)

// ListOptions mirrors list-todos.ps1's parameters.
type ListOptions struct {
	Category string
	Status   string // "open" (default) | "closed" | "all"
	Sort     string // "category" (default) | "age" | "id"
	Since    int
	Search   string
	Detail   bool
	NoColor  bool
}

// List runs `ktd list`.
func List(s *store.Store, opts ListOptions) error {
	if opts.Status == "" {
		opts.Status = "open"
	}
	if opts.Sort == "" {
		opts.Sort = "category"
	}

	items, errs := s.List()
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "warning: %v\n", e)
	}

	var perItemCats [][]string
	for _, it := range items {
		perItemCats = append(perItemCats, it.Todo.Categories)
	}
	canon := categories.Build(perItemCats)

	useColor := !opts.NoColor && isTerminal(os.Stdout)
	today := time.Now()

	if opts.Since > 0 {
		return listSince(items, opts, canon, useColor, today)
	}
	return listNormal(items, opts, canon, useColor, today)
}

func listSince(items []store.Item, opts ListOptions, canon categories.CanonicalMap, useColor bool, today time.Time) error {
	cutoff := today.AddDate(0, 0, -opts.Since)

	type closedItem struct {
		it       store.Item
		closedAt time.Time
		daysAgo  int
	}
	var closed []closedItem
	for _, it := range items {
		if it.Todo.Status != "closed" || it.Todo.Closed == "" {
			continue
		}
		d, err := time.Parse("2006-01-02", it.Todo.Closed)
		if err != nil {
			continue
		}
		if d.Before(cutoff) || d.After(today) {
			continue
		}
		if opts.Category != "" && !hasCategorySubstring(it.Todo.Categories, opts.Category) {
			continue
		}
		closed = append(closed, closedItem{it: it, closedAt: d, daysAgo: int(today.Sub(d).Hours() / 24)})
	}

	if len(closed) == 0 {
		fmt.Printf("🤷 No items closed in the last %d days.\n", opts.Since)
		return nil
	}

	sort.Slice(closed, func(i, j int) bool {
		if !closed[i].closedAt.Equal(closed[j].closedAt) {
			return closed[i].closedAt.After(closed[j].closedAt)
		}
		return closed[i].it.Todo.ID < closed[j].it.Todo.ID
	})

	titleMatched := map[string]bool{}
	for _, c := range closed {
		if opts.Detail {
			writeItemVerbose(c.it.Todo, canon, useColor, titleMatched[c.it.Todo.ID], today)
			continue
		}
		id := colorize(useColor, colorDarkGray, c.it.Todo.ID)
		title := c.it.Todo.Title
		catStr := colorize(useColor, colorDarkGray, canon.FormatList(c.it.Todo.Categories))
		closedNote := colorize(useColor, colorCyan, fmt.Sprintf("✅ closed %s (%dd ago)", c.it.Todo.Closed, c.daysAgo))
		fmt.Printf("%s  %s  %s  %s\n", id, title, catStr, closedNote)
	}
	fmt.Println()
	fmt.Println(colorize(useColor, colorGray, fmt.Sprintf("📊 Total: %d item(s) closed in the last %d days.", len(closed), opts.Since)))
	return nil
}

func listNormal(items []store.Item, opts ListOptions, canon categories.CanonicalMap, useColor bool, today time.Time) error {
	var filtered []store.Item
	for _, it := range items {
		if opts.Status != "all" && it.Todo.Status != opts.Status {
			continue
		}
		if opts.Category != "" {
			if !hasCategorySubstring(it.Todo.Categories, opts.Category) {
				continue
			}
		}
		filtered = append(filtered, it)
	}
	distinctCount := len(filtered)

	titleMatched := map[string]bool{}
	if opts.Search != "" {
		needle := strings.ToLower(opts.Search)
		var searched []store.Item
		for _, it := range filtered {
			haystack := strings.ToLower(it.Todo.Title + " " + strings.Join(it.Todo.Categories, " ") + " " + it.Todo.Body)
			if strings.Contains(haystack, needle) {
				searched = append(searched, it)
				if strings.Contains(strings.ToLower(it.Todo.Title), needle) {
					titleMatched[it.Todo.ID] = true
				}
			}
		}
		filtered = searched
		distinctCount = len(filtered)
	}

	switch opts.Sort {
	case "age":
		sort.Slice(filtered, func(i, j int) bool {
			return ageDays(filtered[i].Todo, today) > ageDays(filtered[j].Todo, today)
		})
		for _, it := range filtered {
			writeItem(it.Todo, canon, useColor, titleMatched[it.Todo.ID], true, today)
		}
	case "id":
		sort.Slice(filtered, func(i, j int) bool { return filtered[i].Todo.ID < filtered[j].Todo.ID })
		for _, it := range filtered {
			writeItem(it.Todo, canon, useColor, titleMatched[it.Todo.ID], true, today)
		}
	default: // "category"
		listByCategory(filtered, canon, useColor, opts.Detail, titleMatched, today)
	}

	footer := "Total: " + fmt.Sprint(distinctCount) + " "
	if opts.Status != "all" {
		footer += opts.Status + " "
	}
	if distinctCount == 1 {
		footer += "item"
	} else {
		footer += "item(s)"
	}
	fmt.Println(colorize(useColor, colorGray, "📊 "+footer))
	return nil
}

func listByCategory(items []store.Item, canon categories.CanonicalMap, useColor, detail bool, titleMatched map[string]bool, today time.Time) {
	groups := map[string][]store.Item{}
	for _, it := range items {
		if len(it.Todo.Categories) == 0 {
			groups["No category"] = append(groups["No category"], it)
			continue
		}
		seen := map[string]bool{}
		for _, c := range it.Todo.Categories {
			cn := canon.Canonical(c)
			if seen[cn] {
				continue
			}
			seen[cn] = true
			groups[cn] = append(groups[cn], it)
		}
	}

	var names []string
	for name := range groups {
		if name != "No category" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if _, ok := groups["No category"]; ok {
		names = append(names, "No category")
	}

	for _, name := range names {
		group := groups[name]
		sort.Slice(group, func(i, j int) bool { return group[i].Todo.ID < group[j].Todo.ID })
		fmt.Println(colorize(useColor, colorCyan, "📁 "+name))
		for _, it := range group {
			if detail {
				writeItemVerbose(it.Todo, canon, useColor, titleMatched[it.Todo.ID], today)
			} else {
				writeItem(it.Todo, canon, useColor, titleMatched[it.Todo.ID], false, today)
			}
		}
		if !detail {
			fmt.Println()
		}
	}
}

func writeItem(t *model.Todo, canon categories.CanonicalMap, useColor, matched, inlineCategory bool, today time.Time) {
	prefix := ""
	titleColor := ""
	if t.Status == "closed" {
		prefix = "✅ "
		titleColor = colorDarkGray
	} else if matched {
		titleColor = colorYellow
	}

	id := colorize(useColor, colorDarkGray, t.ID)
	title := colorize(useColor, titleColor, t.Title)

	line := fmt.Sprintf("%s%s  %s  ", prefix, id, title)
	if inlineCategory {
		line += colorize(useColor, colorDarkGray, canon.FormatList(t.Categories)) + "  "
	}
	if t.Status == "closed" {
		line += colorize(useColor, colorDarkGray, "closed "+t.Closed)
	} else {
		age := ageDays(t, today)
		line += colorize(useColor, ageColor(age), fmt.Sprintf("%s (%dd)", ageEmoji(age), age))
	}
	fmt.Println(line)
}

func writeItemVerbose(t *model.Todo, canon categories.CanonicalMap, useColor, matched bool, today time.Time) {
	title := colorize(useColor, "", t.Title)
	if matched {
		title = colorize(useColor, colorYellow, t.Title)
	}
	fmt.Printf("%s  %s\n", colorize(useColor, colorDarkGray, t.ID), title)

	if t.Status == "closed" {
		note := "✅ closed " + t.Closed
		if d, err := time.Parse("2006-01-02", t.Closed); err == nil {
			note += fmt.Sprintf(" (%dd ago)", int(today.Sub(d).Hours()/24))
		}
		fmt.Println("  📌 Status:   " + colorize(useColor, colorDarkGray, note))
	} else {
		age := ageDays(t, today)
		fmt.Printf("  📌 Status:   %s\n", colorize(useColor, ageColor(age), fmt.Sprintf("%s open (%dd)", ageEmoji(age), age)))
	}
	fmt.Println("  🏷️  Categories: " + canon.FormatList(t.Categories))
	fmt.Println("  📆 Created:  " + t.Created)
	if len(t.Links) > 0 {
		fmt.Println("  🔗 Links:")
		for _, l := range t.Links {
			fmt.Println("    - " + colorize(useColor, colorCyan, l))
		}
	}
	if strings.TrimSpace(t.Body) != "" {
		fmt.Println("  📄 Description:")
		fmt.Println("    " + strings.ReplaceAll(t.Body, "\n", "\n    "))
	}
	if len(t.Log) > 0 {
		fmt.Println("  📜 Log:")
		for _, e := range t.Log {
			fmt.Printf("    - %s: %s\n", e.Date, e.Text)
		}
	}
	fmt.Println()
}

func ageDays(t *model.Todo, today time.Time) int {
	d, err := time.Parse("2006-01-02", t.Created)
	if err != nil {
		return 0
	}
	return int(today.Sub(d).Hours() / 24)
}

func ageColor(age int) string {
	switch {
	case age <= freshDays:
		return colorGreen
	case age <= agingDays:
		return colorYellow
	default:
		return colorRed
	}
}

func ageEmoji(age int) string {
	switch {
	case age <= freshDays:
		return "🟢"
	case age <= agingDays:
		return "🟡"
	default:
		return "🔴"
	}
}

func hasCategorySubstring(cats []string, filter string) bool {
	needle := strings.ToLower(filter)
	for _, c := range cats {
		if strings.Contains(strings.ToLower(c), needle) {
			return true
		}
	}
	return false
}

func colorize(useColor bool, code, text string) string {
	if !useColor || code == "" {
		return text
	}
	return code + text + colorReset
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
