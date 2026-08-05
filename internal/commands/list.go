// Package commands implements each `ktd` subcommand.
package commands

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

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
	var rows []listRow
	for _, a := range activity {
		for _, e := range a.events {
			counts[e.kind]++
		}
		if opts.Detail {
			writeItemVerbose(a.it.Todo, canon, useColor, false, today)
			continue
		}
		rows = append(rows, buildRow(a.it.Todo, canon, false))
	}
	printRows(rows, useColor)
	fmt.Println()
	fmt.Println(colorize(useColor, colorGray, fmt.Sprintf("📊 Total: %d item(s) active in the last %s — %s.",
		len(activity), dayCount(opts.Since), sinceBreakdown(counts))))
	return nil
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

	// Every sort produces the same flat, column-aligned listing — only the
	// order differs. Items with several categories appear once, not once
	// per category.
	switch opts.Sort {
	case "age":
		// Stalest first, measured by the same last-modified date the
		// listing prints — not by when the item was created.
		sort.SliceStable(filtered, func(i, j int) bool {
			a, b := lastModified(filtered[i].Todo), lastModified(filtered[j].Todo)
			if a != b {
				return a < b
			}
			return filtered[i].Todo.ID < filtered[j].Todo.ID
		})
	case "id":
		sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].Todo.ID < filtered[j].Todo.ID })
	default: // "category"
		sort.SliceStable(filtered, func(i, j int) bool {
			a, b := categorySortKey(filtered[i].Todo, canon), categorySortKey(filtered[j].Todo, canon)
			if a != b {
				return a < b
			}
			return filtered[i].Todo.ID < filtered[j].Todo.ID
		})
	}

	var rows []listRow
	for _, it := range filtered {
		if opts.Detail {
			writeItemVerbose(it.Todo, canon, useColor, titleMatched[it.Todo.ID], today)
			continue
		}
		rows = append(rows, buildRow(it.Todo, canon, titleMatched[it.Todo.ID]))
	}
	printRows(rows, useColor)
	if len(rows) > 0 {
		fmt.Println()
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

// categorySortKey orders an item within the default category sort: by its
// canonical categories, uncategorized items last.
func categorySortKey(t *model.Todo, canon categories.CanonicalMap) string {
	if len(t.Categories) == 0 {
		return "￿" // sorts after any real category name
	}
	cats := make([]string, 0, len(t.Categories))
	for _, c := range t.Categories {
		cats = append(cats, strings.ToLower(canon.Canonical(c)))
	}
	sort.Strings(cats)
	return strings.Join(cats, ", ")
}

// listRow is one item's listing line, kept as separate fields so a whole
// listing can be column-aligned before any of it is printed. Every
// non-detail listing (plain, sorted, or --since) renders through this, so
// the shape is always and only: id, status emoji, last-modified date,
// categories, title.
type listRow struct {
	id         string
	emoji      string
	date       string
	category   string
	title      string
	dateColor  string
	titleColor string
}

// Status markers for the listing's emoji column: an unchecked box for
// open, a checked one for closed. Deliberately not the traffic lights
// used elsewhere for age — this column reports state, not staleness.
const (
	statusEmojiOpen   = "⬜"
	statusEmojiClosed = "✅"
)

func buildRow(t *model.Todo, canon categories.CanonicalMap, matched bool) listRow {
	row := listRow{
		id:       t.ID,
		date:     lastModified(t),
		category: canon.FormatList(t.Categories),
		title:    t.Title,
	}
	if t.Status == "closed" {
		row.emoji = statusEmojiClosed
		row.dateColor = colorDarkGray
		row.titleColor = colorDarkGray
		return row
	}
	row.emoji = statusEmojiOpen
	if matched {
		row.titleColor = colorYellow
	}
	return row
}

// printRows writes rows as a table, padding the category column to the
// widest one present so the titles line up.
func printRows(rows []listRow, useColor bool) {
	catWidth := 0
	for _, r := range rows {
		if w := utf8.RuneCountInString(r.category); w > catWidth {
			catWidth = w
		}
	}
	for _, r := range rows {
		cat := r.category + strings.Repeat(" ", catWidth-utf8.RuneCountInString(r.category))
		fmt.Printf("%s %s %s %s  %s\n",
			colorize(useColor, colorDarkGray, r.id),
			r.emoji,
			colorize(useColor, r.dateColor, r.date),
			colorize(useColor, colorDarkGray, cat),
			colorize(useColor, r.titleColor, r.title))
	}
}

// lastModified is the most recent date an item was touched: created, then
// any log entry, then closed.
func lastModified(t *model.Todo) string {
	latest := t.Created
	for _, e := range t.Log {
		if e.Date > latest {
			latest = e.Date
		}
	}
	if t.Closed > latest {
		latest = t.Closed
	}
	return latest
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
