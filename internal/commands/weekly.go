package commands

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"ktd/internal/ai"
	"ktd/internal/categories"
	"ktd/internal/model"
	"ktd/internal/store"
)

// Weekly runs `ktd weekly [--week-of YYYY-MM-DD]`: assemble the target
// week's Last/This candidate items and hand them to the AI to draft into
// the paste-ready Last/This report.
func Weekly(ctx context.Context, s *store.Store, weekOf string, noColor bool) error {
	var monday, sunday time.Time
	if weekOf != "" {
		ref, err := time.Parse("2006-01-02", weekOf)
		if err != nil {
			return fmt.Errorf("invalid --week-of date %q: expected YYYY-MM-DD", weekOf)
		}
		monday, sunday = weekBounds(ref)
	} else {
		// Most recent completed Monday–Sunday strictly before today.
		todayMonday, _ := weekBounds(time.Now())
		monday = todayMonday.AddDate(0, 0, -7)
		sunday = monday.AddDate(0, 0, 6)
	}

	items, _ := s.List()

	byID := map[string]*model.Todo{}
	var perItemCats [][]string
	for _, it := range items {
		byID[it.Todo.ID] = it.Todo
		perItemCats = append(perItemCats, it.Todo.Categories)
	}
	canon := categories.Build(perItemCats)

	var lastLines []string
	for _, it := range items {
		t := it.Todo
		if t.Status == "closed" && inRange(t.Closed, monday, sunday) {
			lastLines = append(lastLines, formatItemForSummary(t))
			continue
		}
		if t.IsOpen() {
			for _, e := range t.Log {
				if inRange(e.Date, monday, sunday) {
					lastLines = append(lastLines, formatItemForSummary(t))
					break
				}
			}
		}
	}

	var thisLines []string
	for _, it := range items {
		t := it.Todo
		if t.IsOpen() && t.Plan == "this-week" {
			thisLines = append(thisLines, formatItemForSummary(t))
		}
	}
	if len(thisLines) == 0 {
		// No items explicitly marked plan:this-week — fall back to
		// recently active open items as draft candidates.
		cutoff := time.Now().AddDate(0, 0, -14)
		for _, it := range items {
			t := it.Todo
			if !t.IsOpen() {
				continue
			}
			for _, e := range t.Log {
				if d, err := time.Parse("2006-01-02", e.Date); err == nil && !d.Before(cutoff) {
					thisLines = append(thisLines, formatItemForSummary(t))
					break
				}
			}
		}
	}

	summary := fmt.Sprintf(
		"Target week: %s to %s\n\nLast (candidate items — closed this week, or open with log activity this week):\n%s\n\nThis (candidate items — plan:this-week, or recently active open items):\n%s\n",
		monday.Format("2006-01-02"), sunday.Format("2006-01-02"),
		joinOrNone(lastLines), joinOrNone(thisLines),
	)

	apiKey, err := s.APIKey()
	if err != nil {
		return err
	}
	client := ai.NewClient(apiKey)
	result, err := ai.DraftWeekly(ctx, client, summary)
	if err != nil {
		return fmt.Errorf("asking the AI to draft the report: %w", err)
	}

	useColor := !noColor && isTerminal(os.Stdout)
	fmt.Printf("📅 Week of %s – %s\n\n", monday.Format("2006-01-02"), sunday.Format("2006-01-02"))
	fmt.Println(renderWeeklyReport(result, byID, canon, useColor))
	return nil
}

// weeklyGroup is a category's worth of distilled items within a Last/This
// section, in display order.
type weeklyGroup struct {
	Category string
	Items    []ai.WeeklyItemSummary
}

// groupByCategory buckets the AI's distilled item summaries under each
// item's actual (canonicalized) categories — the same grouping `ktd list`
// does — rather than trusting the AI to reproduce categories itself. An
// item with multiple categories appears once under each; an item with none
// (or an id the AI didn't echo back correctly) lands under "Other".
// Groups are sorted alphabetically, with "Other" last.
func groupByCategory(summaries []ai.WeeklyItemSummary, byID map[string]*model.Todo, canon categories.CanonicalMap) []weeklyGroup {
	const other = "Other"
	buckets := map[string][]ai.WeeklyItemSummary{}
	for _, s := range summaries {
		t, ok := byID[s.ID]
		if !ok || len(t.Categories) == 0 {
			buckets[other] = append(buckets[other], s)
			continue
		}
		seen := map[string]bool{}
		for _, c := range t.Categories {
			cn := canon.Canonical(c)
			if seen[cn] {
				continue
			}
			seen[cn] = true
			buckets[cn] = append(buckets[cn], s)
		}
	}

	var names []string
	for name := range buckets {
		if name != other {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if _, ok := buckets[other]; ok {
		names = append(names, other)
	}

	groups := make([]weeklyGroup, 0, len(names))
	for _, name := range names {
		groups = append(groups, weeklyGroup{Category: name, Items: buckets[name]})
	}
	return groups
}

// renderWeeklyReport lays out the Last/This sections with real ANSI bold
// on headers (so terminals that copy formatting, e.g. Windows Terminal,
// carry the bold into a rich text editor like Gmail) and indented bullets
// so category groups read as nested lists.
func renderWeeklyReport(result ai.WeeklyResult, byID map[string]*model.Todo, canon categories.CanonicalMap, useColor bool) string {
	var b strings.Builder
	writeSection := func(title string, summaries []ai.WeeklyItemSummary) {
		b.WriteString(bold(title, useColor))
		b.WriteString("\n\n")
		for _, g := range groupByCategory(summaries, byID, canon) {
			b.WriteString(bold(g.Category, useColor))
			b.WriteString("\n\n")
			for _, item := range g.Items {
				line := item.Text
				if item.Link != "" {
					line += " " + item.Link
				}
				b.WriteString("    • " + line + "\n")
			}
			b.WriteString("\n")
		}
	}
	writeSection("Last", result.Last)
	writeSection("This", result.This)
	return strings.TrimRight(b.String(), "\n")
}

func bold(s string, useColor bool) string {
	if !useColor {
		return s
	}
	return colorBold + s + colorReset
}

// weekBounds returns the Monday and Sunday (inclusive) of the week
// containing ref.
func weekBounds(ref time.Time) (monday, sunday time.Time) {
	offset := (int(ref.Weekday()) + 6) % 7 // Monday=0 ... Sunday=6
	m := ref.AddDate(0, 0, -offset)
	monday = time.Date(m.Year(), m.Month(), m.Day(), 0, 0, 0, 0, m.Location())
	sunday = monday.AddDate(0, 0, 6)
	return monday, sunday
}

func inRange(dateStr string, start, end time.Time) bool {
	if dateStr == "" {
		return false
	}
	d, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return false
	}
	return !d.Before(start) && !d.After(end)
}

// formatItemForSummary renders one candidate item as plain text for the AI
// prompt: an id (echoed back in WeeklyItemSummary so the caller can map it
// back to the item's real categories), title, and any body/log detail —
// the actual substance the AI needs to write more than just the title back.
func formatItemForSummary(t *model.Todo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- id=%s title=%q", t.ID, t.Title)
	if len(t.Links) > 0 {
		fmt.Fprintf(&b, " link=%s", t.Links[0])
	}
	if body := oneLine(t.Body); body != "" {
		fmt.Fprintf(&b, "\n  body: %s", body)
	}
	for _, e := range t.Log {
		fmt.Fprintf(&b, "\n  log %s: %s", e.Date, oneLine(e.Text))
	}
	return b.String()
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func joinOrNone(lines []string) string {
	if len(lines) == 0 {
		return "(none)"
	}
	return strings.Join(lines, "\n")
}
