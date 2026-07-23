package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ktd/internal/ai"
	"ktd/internal/model"
	"ktd/internal/store"
)

// Weekly runs `ktd weekly [--week-of YYYY-MM-DD]`: assemble the target
// week's Last/This candidate items and hand them to the AI to draft into
// the paste-ready Last/This markdown report.
func Weekly(ctx context.Context, s *store.Store, weekOf string) error {
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

	fmt.Printf("Week of %s – %s\n\n", monday.Format("2006-01-02"), sunday.Format("2006-01-02"))
	fmt.Println(result.Markdown)
	return nil
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

func formatItemForSummary(t *model.Todo) string {
	line := fmt.Sprintf("- %s [%s]", t.Title, strings.Join(t.Categories, ", "))
	if len(t.Links) > 0 {
		line += " " + t.Links[0]
	}
	return line
}

func joinOrNone(lines []string) string {
	if len(lines) == 0 {
		return "(none)"
	}
	return strings.Join(lines, "\n")
}
