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
	colorBold     = "\033[1m"
	colorDarkGray = "\033[90m"
	colorGray     = "\033[37m"
	colorCyan     = "\033[36m"
	colorYellow   = "\033[33m"
	colorGreen    = "\033[32m"
	colorRed      = "\033[31m"
)

// ListOptions mirrors list-todos.ps1's parameters.
type ListOptions struct {
	Category       string
	Status         string // "open" (default) | "closed" | "all"
	StatusExplicit bool   // did the caller actually pass --status? (see List)
	Sort           string // "category" (default) | "age" | "id"
	Since          int
	Search         string
	Detail         bool
	NoColor        bool
}

// List runs `ktd list`.
func List(s *store.Store, opts ListOptions) error {
	if opts.Status == "" {
		opts.Status = "open"
	}
	if opts.Sort == "" {
		opts.Sort = "category"
	}
	if opts.Since > 0 && !opts.StatusExplicit {
		// A since-window is a recap of everything that happened, so the
		// usual "open" default would hide the items you just closed —
		// exactly what you're looking for. An explicit --status still
		// narrows the window's results.
		opts.Status = "all"
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

// Activity kinds reported by --since, in the order they can happen to an
// item (which is also the order they're printed in on a single line).
const (
	sinceAdded  = "added"
	sinceLogged = "logged"
	sinceClosed = "closed"
)

// sinceEvent is one dated thing that happened to an item inside the
// --since window.
type sinceEvent struct {
	kind string
	date time.Time
}

// sinceActivity is one item plus its in-window events, oldest first.
type sinceActivity struct {
	it     store.Item
	events []sinceEvent
}

// latest is the date of the most recent in-window event — what the
// listing sorts on.
func (a sinceActivity) latest() time.Time { return a.events[len(a.events)-1].date }

// listSince reports every item touched in the last N days and how it was
// touched: created (added), given a log entry (logged), or closed. An
// item can show more than one of those; it's still one line.
func listSince(items []store.Item, opts ListOptions, canon categories.CanonicalMap, useColor bool, today time.Time) error {
	// The window is whole days, anchored at midnight: --since 1 means
	// "yesterday and today", not "the last 24 hours" — dates parse to
	// midnight, so a clock-time cutoff would drop the far edge entirely.
	// Boundaries are built in UTC to match what time.Parse hands back for
	// a bare YYYY-MM-DD; today's *calendar* day is still the local one.
	end := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	start := end.AddDate(0, 0, -opts.Since)
	inWindow := func(dateStr string) (time.Time, bool) {
		d, err := time.Parse("2006-01-02", dateStr)
		if err != nil || d.Before(start) || d.After(end) {
			return time.Time{}, false
		}
		return d, true
	}

	var activity []sinceActivity
	for _, it := range items {
		t := it.Todo
		if opts.Status != "all" && t.Status != opts.Status {
			continue
		}
		if opts.Category != "" && !hasCategorySubstring(t.Categories, opts.Category) {
			continue
		}

		var events []sinceEvent
		if d, ok := inWindow(t.Created); ok {
			events = append(events, sinceEvent{sinceAdded, d})
		}
		// Several log bullets in one window still just mean "worked on
		// it", so only the most recent one is reported.
		var logged time.Time
		for _, e := range t.Log {
			if d, ok := inWindow(e.Date); ok && d.After(logged) {
				logged = d
			}
		}
		if !logged.IsZero() {
			events = append(events, sinceEvent{sinceLogged, logged})
		}
		if t.Status == "closed" {
			if d, ok := inWindow(t.Closed); ok {
				events = append(events, sinceEvent{sinceClosed, d})
			}
		}
		if len(events) == 0 {
			continue
		}
		sort.SliceStable(events, func(i, j int) bool { return events[i].date.Before(events[j].date) })
		activity = append(activity, sinceActivity{it: it, events: events})
	}

	if len(activity) == 0 {
		fmt.Printf("🤷 No activity in the last %s.\n", dayCount(opts.Since))
		return nil
	}

	sort.Slice(activity, func(i, j int) bool {
		if !activity[i].latest().Equal(activity[j].latest()) {
			return activity[i].latest().After(activity[j].latest())
		}
		return activity[i].it.Todo.ID < activity[j].it.Todo.ID
	})

	counts := map[string]int{}
	for _, a := range activity {
		for _, e := range a.events {
			counts[e.kind]++
		}
		if opts.Detail {
			writeItemVerbose(a.it.Todo, canon, useColor, false, today)
			continue
		}
		id := colorize(useColor, colorDarkGray, a.it.Todo.ID)
		catStr := colorize(useColor, colorDarkGray, canon.FormatList(a.it.Todo.Categories))
		fmt.Printf("%s  %s  %s  %s\n", id, a.it.Todo.Title, catStr, formatSinceEvents(a.events, useColor, end))
	}
	fmt.Println()
	fmt.Println(colorize(useColor, colorGray, fmt.Sprintf("📊 Total: %d item(s) active in the last %s — %s.",
		len(activity), dayCount(opts.Since), sinceBreakdown(counts))))
	return nil
}

// formatSinceEvents renders an item's in-window events as one trailing
// note, e.g. "🆕 added 4d ago · ✅ closed today".
func formatSinceEvents(events []sinceEvent, useColor bool, end time.Time) string {
	parts := make([]string, 0, len(events))
	for _, e := range events {
		days := int(end.Sub(e.date).Hours() / 24)
		note := fmt.Sprintf("%s %s %s", sinceEmoji(e.kind), e.kind, daysAgo(days))
		parts = append(parts, colorize(useColor, sinceColor(e.kind), note))
	}
	return strings.Join(parts, " · ")
}

// sinceBreakdown summarizes the event counts behind a since listing, e.g.
// "3 added, 1 logged, 4 closed", skipping kinds that didn't occur.
func sinceBreakdown(counts map[string]int) string {
	var parts []string
	for _, kind := range []string{sinceAdded, sinceLogged, sinceClosed} {
		if n := counts[kind]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, kind))
		}
	}
	return strings.Join(parts, ", ")
}

func dayCount(days int) string {
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}

func daysAgo(days int) string {
	if days == 0 {
		return "today"
	}
	return fmt.Sprintf("%dd ago", days)
}

func sinceColor(kind string) string {
	switch kind {
	case sinceAdded:
		return colorGreen
	case sinceLogged:
		return colorYellow
	default:
		return colorCyan
	}
}

func sinceEmoji(kind string) string {
	switch kind {
	case sinceAdded:
		return "🆕"
	case sinceLogged:
		return "📝"
	default:
		return "✅"
	}
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
