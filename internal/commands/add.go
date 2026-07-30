package commands

import (
	"context"
	"fmt"
	"time"

	"ktd/internal/ai"
	"ktd/internal/model"
	"ktd/internal/store"
)

// Add runs `ktd add <text>`: extract links deterministically, fetch any
// referenced GitHub issue/PR (unless noFetch), ask the AI to distill a
// title/body/categories from the remaining text plus that reference,
// confirm with the user, then write a new open item.
func Add(ctx context.Context, s *store.Store, text string, noFetch bool) error {
	apiKey, err := s.APIKey()
	if err != nil {
		return err
	}
	client := ai.NewClient(apiKey)

	items, _ := s.List()
	existingCats := store.AllCategories(items)

	today := time.Now().Format("2006-01-02")
	links, remainder := ai.ExtractLinks(text)
	reference := buildReference(ctx, links, noFetch)
	result, err := ai.ParseAdd(ctx, client, existingCats, today, remainder, reference)
	if err != nil {
		return fmt.Errorf("asking the AI to parse the item: %w", err)
	}

	id, err := s.NextID()
	if err != nil {
		return err
	}

	created := today
	if v := validAIDate(result.Date); v != "" {
		created = v
	}

	body := remainder
	if result.Body != "" {
		body = result.Body
	}

	t := &model.Todo{
		ID:         id,
		Title:      result.Title,
		Status:     "open",
		Categories: result.Categories,
		Created:    created,
		Links:      links,
		Body:       body,
	}

	if !confirmItems("add", []*model.Todo{t}) {
		fmt.Println("❌ Aborted — nothing written.")
		return nil
	}

	if err := s.Save(t); err != nil {
		return err
	}
	fmt.Printf("✨ Added %s — %s %s\n", t.ID, t.Title, formatCatsInline(t.Categories))
	return nil
}
