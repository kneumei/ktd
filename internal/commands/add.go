package commands

import (
	"context"
	"fmt"
	"time"

	"ktd/internal/ai"
	"ktd/internal/model"
	"ktd/internal/store"
)

// Add runs `ktd add <text>`: extract links deterministically, ask the AI
// to distill a title and categories from the remaining text, confirm with
// the user, then write a new open item.
func Add(ctx context.Context, s *store.Store, text string) error {
	apiKey, err := s.APIKey()
	if err != nil {
		return err
	}
	client := ai.NewClient(apiKey)

	items, _ := s.List()
	existingCats := store.AllCategories(items)

	today := time.Now().Format("2006-01-02")
	links, remainder := ai.ExtractLinks(text)
	result, err := ai.ParseAdd(ctx, client, existingCats, today, remainder)
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

	t := &model.Todo{
		ID:         id,
		Title:      result.Title,
		Status:     "open",
		Categories: result.Categories,
		Created:    created,
		Links:      links,
		Body:       remainder,
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
