package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"ktd/internal/ai"
	"ktd/internal/categories"
	"ktd/internal/github"
	"ktd/internal/model"
	"ktd/internal/store"
)

// noInstructionPlaceholder is passed to ParseEdit in place of an empty
// remainder when the user attached a GitHub link with no other words —
// it tells the model explicitly that summarizing the reference is the
// only job, rather than handing it a blank user message.
const noInstructionPlaceholder = "(no instruction — summarize the referenced GitHub item(s) as a concise log note)"

// Edit runs `ktd edit <id|text> <change>`: resolve the item mechanically,
// pull out any URLs deterministically, then ask the AI to classify the
// remaining text as a log note, a body addition, a category change, or a
// close. A bare GitHub issue/PR link (no other words) still triggers the
// AI so its summary lands as a dated log_note — see noInstructionPlaceholder.
func Edit(ctx context.Context, s *store.Store, query, change string, noFetch bool) error {
	items, errs := s.List()
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "warning: %v\n", e)
	}

	var perItemCats [][]string
	for _, it := range items {
		perItemCats = append(perItemCats, it.Todo.Categories)
	}
	canon := categories.Build(perItemCats)

	it, ok := resolveOne(items, query, canon)
	if !ok {
		return nil
	}

	links, remainder := ai.ExtractLinks(change)
	remainder = strings.TrimSpace(remainder)
	if remainder == "" && len(links) == 0 {
		return fmt.Errorf("nothing to change: input contained no text and no links")
	}
	refs := github.DetectRefs(links)

	proposed := *it.Todo // shallow copy — safe since all fields we mutate are reassigned, not mutated in place
	if len(links) > 0 {
		proposed.Links = append(append([]string{}, it.Todo.Links...), links...)
	}

	// A bare GitHub link (no other words) still calls the AI so its summary
	// lands as a dated log note, not just a URL in Links. A non-GitHub link
	// with no other text skips the AI entirely, as before.
	var summary string
	if remainder != "" || len(refs) > 0 {
		apiKey, err := s.APIKey()
		if err != nil {
			return err
		}
		client := ai.NewClient(apiKey)
		existingCats := store.AllCategories(items)
		today := time.Now().Format("2006-01-02")

		reference := buildReference(ctx, links, noFetch)
		instruction := remainder
		if instruction == "" {
			instruction = noInstructionPlaceholder
		}

		result, err := ai.ParseEdit(ctx, client, existingCats, today, instruction, reference)
		if err != nil {
			return fmt.Errorf("asking the AI to classify the change: %w", err)
		}

		switch result.Classification {
		case ai.ClassificationLogNote:
			text := result.LogText
			if text == "" {
				text = remainder
			}
			if text == "" {
				text = "referenced " + strings.Join(links, ", ")
			}
			date := today
			if v := validAIDate(result.Date); v != "" {
				date = v
			}
			proposed.Log = append(append([]model.LogEntry{}, it.Todo.Log...), model.LogEntry{Date: date, Text: text})
			summary = fmt.Sprintf("append log note (%s): %s", date, text)
		case ai.ClassificationBodyAddition:
			addition := result.BodyAddition
			if addition == "" {
				addition = remainder
			}
			proposed.Body = strings.TrimRight(it.Todo.Body, "\n") + "\n\n" + addition
			summary = "append to body: " + addition
		case ai.ClassificationCategoryChange:
			proposed.Categories = applyCategoryChange(it.Todo.Categories, result.CategoriesAdd, result.CategoriesRemove)
			summary = "categories -> " + formatCatsInline(proposed.Categories)
		case ai.ClassificationCloseItem:
			date := today
			if v := validAIDate(result.Date); v != "" {
				date = v
			}
			proposed.Status = "closed"
			proposed.Closed = date
			summary = fmt.Sprintf("close as of %s", date)
		default:
			return fmt.Errorf("unrecognized AI classification %q", result.Classification)
		}
	}

	fmt.Printf("✏️  Proposed edit to %s — %s:\n", it.Todo.ID, it.Todo.Title)
	if summary != "" {
		fmt.Println("  " + summary)
	}
	if len(links) > 0 {
		fmt.Println("  🔗 add link(s):")
		for _, l := range links {
			fmt.Println("    - " + l)
		}
	}
	fmt.Print("💾 Apply? [y/N] ")
	if !readYesNo() {
		fmt.Println("❌ Aborted — nothing changed.")
		return nil
	}

	if err := s.Save(&proposed); err != nil {
		return err
	}
	fmt.Println("✅ Updated.")
	return nil
}

// applyCategoryChange returns existing with remove entries dropped and add
// entries appended, case-insensitively deduped.
func applyCategoryChange(existing, add, remove []string) []string {
	removeSet := map[string]bool{}
	for _, r := range remove {
		removeSet[strings.ToLower(r)] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, c := range existing {
		if removeSet[strings.ToLower(c)] {
			continue
		}
		key := strings.ToLower(c)
		if !seen[key] {
			seen[key] = true
			out = append(out, c)
		}
	}
	for _, a := range add {
		key := strings.ToLower(a)
		if !seen[key] {
			seen[key] = true
			out = append(out, a)
		}
	}
	return out
}
