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

// noInstructionPlaceholder is passed to ParseCardEdit in place of an empty
// remainder when the user attached a GitHub link with no other words — it
// tells the model explicitly that summarizing the reference is the only
// job, rather than handing it a blank instruction.
const noInstructionPlaceholder = "(no instruction — add a concise log note dated today summarizing the referenced GitHub item(s); leave everything else unchanged)"

// Edit runs `ktd edit <id|text> <change>`: resolve the item mechanically,
// then hand the whole current card plus the freeform change to the AI,
// which returns the complete new card state (see aiEditCard). The confirm
// screen supports y/N/e — choosing e lets you describe a further change
// before writing, looping until you answer y or n.
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

	proposed := *it.Todo // shallow copy — safe since all fields we mutate are reassigned, not mutated in place
	if _, err := aiEditCard(ctx, s, &proposed, change, noFetch); err != nil {
		return err
	}

	render := func() {
		fmt.Printf("✏️  Proposed edit to %s — %s:\n", it.Todo.ID, it.Todo.Title)
		lines := diffCard(it.Todo, &proposed)
		if len(lines) == 0 {
			fmt.Println("  (no field changes)")
		}
		for _, l := range lines {
			fmt.Println("  " + l)
		}
		if len(proposed.Links) > len(it.Todo.Links) {
			fmt.Println("  🔗 add link(s):")
			for _, l := range proposed.Links[len(it.Todo.Links):] {
				fmt.Println("    - " + l)
			}
		}
	}
	applyEdit := func(_ int, instruction string) {
		if _, err := aiEditCard(ctx, s, &proposed, instruction, noFetch); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  edit failed: %v\n", err)
		}
	}

	if !confirmLoop([]*model.Todo{&proposed}, "💾 Apply?", render, applyEdit) {
		fmt.Println("❌ Aborted — nothing changed.")
		return nil
	}

	if err := s.Save(&proposed); err != nil {
		return err
	}
	fmt.Println("✅ Updated.")
	return nil
}

// aiEditCard asks the AI to apply a freeform instruction to proposed,
// mutating it in place, and returns human-readable lines describing what
// changed (see diffCard). Links are extracted and appended mechanically —
// never via the AI schema, which covers title/status/closed/categories/
// body/log. Returns an error (leaving proposed untouched) if instruction
// has no text and no links.
func aiEditCard(ctx context.Context, s *store.Store, proposed *model.Todo, instruction string, noFetch bool) ([]string, error) {
	links, remainder := ai.ExtractLinks(instruction)
	remainder = strings.TrimSpace(remainder)
	if remainder == "" && len(links) == 0 {
		return nil, fmt.Errorf("nothing to change: input contained no text and no links")
	}

	before := *proposed
	refs := github.DetectRefs(links)

	if len(links) > 0 {
		proposed.Links = append(append([]string{}, proposed.Links...), links...)
	}

	// A bare GitHub link (no other words) still calls the AI so its summary
	// lands as a dated log note, not just a URL in Links. A non-GitHub link
	// with no other text skips the AI entirely, as before.
	if remainder != "" || len(refs) > 0 {
		client, err := newAIClient(s)
		if err != nil {
			return nil, err
		}
		items, _ := s.List()
		existingCats := store.AllCategories(items)
		today := time.Now().Format("2006-01-02")

		reference := buildReference(ctx, links, noFetch)
		instr := remainder
		if instr == "" {
			instr = noInstructionPlaceholder
		}

		result, err := ai.ParseCardEdit(ctx, client, proposed, existingCats, today, instr, reference)
		if err != nil {
			return nil, fmt.Errorf("asking the AI to edit the card: %w", err)
		}
		if result.Status != "open" && result.Status != "closed" {
			return nil, fmt.Errorf("AI returned unrecognized status %q", result.Status)
		}

		proposed.Title = result.Title
		proposed.Status = result.Status
		if result.Status == "closed" {
			proposed.Closed = validAIDate(result.Closed)
			if proposed.Closed == "" {
				proposed.Closed = today
			}
		} else {
			proposed.Closed = "" // mechanical safety net regardless of what the AI sent
		}
		proposed.Categories = result.Categories
		proposed.Body = result.Body
		proposed.Log = nil
		for _, l := range result.Log {
			proposed.Log = append(proposed.Log, model.LogEntry{Date: l.Date, Text: l.Text})
		}
	}

	return diffCard(&before, proposed), nil
}

// diffCard reports human-readable lines describing what changed between
// old and new: title, status/closed, categories, body, and log
// additions/removals. Links are deliberately not covered here — callers
// that add links mechanically report that separately.
func diffCard(old, new *model.Todo) []string {
	var lines []string
	if old.Title != new.Title {
		lines = append(lines, fmt.Sprintf("title: %q -> %q", old.Title, new.Title))
	}
	switch {
	case old.Status != new.Status && new.Status == "closed":
		lines = append(lines, fmt.Sprintf("status -> closed (as of %s)", new.Closed))
	case old.Status != new.Status:
		lines = append(lines, "status -> open (reopened)")
	case old.Closed != new.Closed:
		lines = append(lines, fmt.Sprintf("closed date -> %s", new.Closed))
	}
	if added, removed := diffCategories(old.Categories, new.Categories); len(added)+len(removed) > 0 {
		var parts []string
		if len(added) > 0 {
			parts = append(parts, "+"+strings.Join(added, ", +"))
		}
		if len(removed) > 0 {
			parts = append(parts, "-"+strings.Join(removed, ", -"))
		}
		lines = append(lines, "categories: "+strings.Join(parts, ", "))
	}
	if old.Body != new.Body {
		lines = append(lines, "body -> "+truncateForDisplay(new.Body, 300))
	}
	for _, l := range logEntriesNotIn(new.Log, old.Log) {
		lines = append(lines, fmt.Sprintf("log + %s: %s", l.Date, l.Text))
	}
	for _, l := range logEntriesNotIn(old.Log, new.Log) {
		lines = append(lines, fmt.Sprintf("log - %s: %s", l.Date, l.Text))
	}
	return lines
}

// diffCategories reports which categories were added/removed between old
// and new, case-insensitively (matching the store's category-dedup
// convention).
func diffCategories(old, new []string) (added, removed []string) {
	oldSet := map[string]bool{}
	for _, c := range old {
		oldSet[strings.ToLower(c)] = true
	}
	newSet := map[string]bool{}
	for _, c := range new {
		newSet[strings.ToLower(c)] = true
	}
	for _, c := range new {
		if !oldSet[strings.ToLower(c)] {
			added = append(added, c)
		}
	}
	for _, c := range old {
		if !newSet[strings.ToLower(c)] {
			removed = append(removed, c)
		}
	}
	return added, removed
}

// logEntriesNotIn returns the entries of a that don't exactly appear in b.
func logEntriesNotIn(a, b []model.LogEntry) []model.LogEntry {
	inB := make(map[model.LogEntry]bool, len(b))
	for _, e := range b {
		inB[e] = true
	}
	var out []model.LogEntry
	for _, e := range a {
		if !inB[e] {
			out = append(out, e)
		}
	}
	return out
}
