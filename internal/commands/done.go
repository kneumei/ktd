package commands

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ktd/internal/ai"
	"ktd/internal/model"
	"ktd/internal/store"
)

// Done runs `ktd done <text> [--as-of YYYY-MM-DD]`. Each non-blank line of
// text is treated as one closed item (so pasting several lines files
// several items in one call, per the original SKILL.md behavior); a
// single line is the common case. Every item gets both created and closed
// set to the as-of date; --as-of, when given, applies to every line and
// wins over anything in the text. Otherwise each line's own AI-extracted
// date is used (e.g. "closed date is yesterday"), falling back to today.
// Each item also gets one seeded "## Log" bullet dated the same date.
func Done(ctx context.Context, s *store.Store, text, asOf string, noFetch bool) error {
	apiKey, err := s.APIKey()
	if err != nil {
		return err
	}
	client := ai.NewClient(apiKey)

	closedDate, err := resolveAsOfDate(asOf)
	if err != nil {
		return err
	}

	items, _ := s.List()
	existingCats := store.AllCategories(items)

	startID, err := s.NextID()
	if err != nil {
		return err
	}
	nextIDNum, err := strconv.Atoi(startID)
	if err != nil {
		return fmt.Errorf("parsing next id %q: %w", startID, err)
	}

	today := time.Now().Format("2006-01-02")

	var todos []*model.Todo
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		links, remainder := ai.ExtractLinks(line)
		reference := buildReference(ctx, links, noFetch)
		result, err := ai.ParseAdd(ctx, client, existingCats, today, remainder, reference)
		if err != nil {
			return fmt.Errorf("asking the AI to parse %q: %w", line, err)
		}

		itemDate := closedDate
		if asOf == "" {
			if v := validAIDate(result.Date); v != "" {
				itemDate = v
			}
		}

		body := remainder
		if result.Body != "" {
			body = result.Body
		}

		t := &model.Todo{
			ID:         fmt.Sprintf("%04d", nextIDNum),
			Title:      result.Title,
			Status:     "closed",
			Categories: result.Categories,
			Created:    itemDate,
			Closed:     itemDate,
			Links:      links,
			Body:       body,
			Log:        []model.LogEntry{{Date: itemDate, Text: result.Title}},
		}
		todos = append(todos, t)
		nextIDNum++
	}

	if len(todos) == 0 {
		fmt.Println("🤷 Nothing to do — no non-blank lines in input.")
		return nil
	}

	if !confirmItems("done", todos) {
		fmt.Println("❌ Aborted — nothing written.")
		return nil
	}

	var ids []string
	for _, t := range todos {
		if err := s.Save(t); err != nil {
			return err
		}
		ids = append(ids, fmt.Sprintf("%s — %s", t.ID, t.Title))
	}

	fmt.Printf("✅ Closed %d item(s) as of %s:\n", len(todos), closedDate)
	for _, line := range ids {
		fmt.Println("  " + line)
	}
	return nil
}
