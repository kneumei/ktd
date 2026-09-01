package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"ktd/internal/model"
)

// CardLogEntry mirrors model.LogEntry for JSON round-tripping through the
// edit_card tool.
type CardLogEntry struct {
	Date string `json:"date"`
	Text string `json:"text"`
}

// CardEditResult is the AI's complete new state for a todo card after
// applying a freeform edit instruction. Every field is meaningful — unlike
// the old classification-based edit result, this is not a delta: fields
// the instruction didn't touch are simply echoed back unchanged, and the
// caller applies every field wholesale onto the proposed Todo. ID,
// Created, Links, and Plan are deliberately not part of this shape — they
// stay under mechanical (non-AI) control.
type CardEditResult struct {
	Title      string         `json:"title"`
	Status     string         `json:"status"` // "open" or "closed"
	Closed     string         `json:"closed"` // YYYY-MM-DD, "" iff open
	Categories []string       `json:"categories"`
	Body       string         `json:"body"`
	Log        []CardLogEntry `json:"log"`
}

const cardEditSystemPrompt = `You help maintain a personal work-todo tracker. You are given the full current state of one card and a freeform instruction describing a change to make. Return the COMPLETE new state of the card via the edit_card tool — every field, not just the ones that changed.

Default rule: copy every field forward EXACTLY as given unless the instruction clearly implies a change to it. Never drop, reword, or reorder existing log entries you weren't asked to touch — that data is not recoverable if you lose it.

- "title": change only if the instruction asks to rename/retitle the item.
- "categories": add and/or remove tags as implied by the instruction. When an existing category (see below) applies, reuse its exact casing rather than creating a near-duplicate. Never invent a category with no basis in the instruction.
- "body": the item's description. Rewrite, trim, or append to it only as the instruction directs (e.g. "reword to be shorter" replaces it; "add that it also affects X" appends). Otherwise copy it forward unchanged.
- "log": a dated history of bullets. To add a note, append a new entry (with today's date unless the instruction states another date). To remove a note, the instruction will reference it by date and/or content (e.g. "remove the log on 1/2/2026") — match it against the actual dated entries listed below and omit only the matching entry from your returned log array; keep every other entry byte-for-byte as given, in the same order. If the instruction's date is ambiguous (e.g. "1/2/2026"), match it against the entries actually present below rather than guessing a format.
- "status"/"closed": closing the item ("close it", "mark done") sets status to "closed" and closed to today's date unless the instruction states another date, resolved the same way as log dates. Reopening ("reopen", "not done yet") sets status to "open" and closed to "" (empty string). Otherwise copy both forward unchanged.

When a "Referenced GitHub items" block is given below the instruction, weave a summary of it into whichever field the instruction targets. If the instruction carries no content of its own beyond the reference (a bare link), append one new log entry dated today summarizing the referenced item(s) and leave everything else unchanged.

Today's date is %s. Existing categories in use: %s.

Respond only via the edit_card tool.`

var cardLogEntrySchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"date": map[string]any{"type": "string", "description": "YYYY-MM-DD."},
		"text": map[string]any{"type": "string"},
	},
	"required":             []string{"date", "text"},
	"additionalProperties": false,
}

var cardEditTool = tool{
	Name:        "edit_card",
	Description: "Return the complete new state of the todo card after applying the requested change.",
	InputSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":  map[string]any{"type": "string"},
			"status": map[string]any{"type": "string", "enum": []string{"open", "closed"}},
			"closed": map[string]any{"type": "string", "description": "YYYY-MM-DD if status is closed, else empty string."},
			"categories": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"body": map[string]any{"type": "string"},
			"log": map[string]any{
				"type":  "array",
				"items": cardLogEntrySchema,
			},
		},
		"required":             []string{"title", "status", "closed", "categories", "body", "log"},
		"additionalProperties": false,
	},
}

// cardEditMaxTokens caps the edit_card response. It's larger than the
// default CallTool budget since the model must echo the full body and log
// history back, not just a diff.
const cardEditMaxTokens = 4096

// ParseCardEdit asks the AI to apply a freeform instruction to current and
// return the complete new card state. today is passed as YYYY-MM-DD so the
// AI can resolve relative dates like "yesterday". Callers should run
// ExtractLinks on instruction first and apply any found links to the item
// directly (mechanically) — links are not part of this schema. reference,
// if non-empty, is a formatted block of fetched GitHub issue/PR content
// (see internal/github) appended to the instruction as extra context —
// pass "" when there's none.
func ParseCardEdit(ctx context.Context, c *Client, current *model.Todo, existingCategories []string, today, instruction, reference string) (CardEditResult, error) {
	system := fmt.Sprintf(cardEditSystemPrompt, today, formatCategoryList(existingCategories))
	userText := fmt.Sprintf("Current card:\n%s\n\n--- Requested change ---\n%s", formatCard(current), instruction)
	userText = withReference(userText, reference)

	raw, err := c.CallToolMax(ctx, system, userText, cardEditTool, cardEditMaxTokens)
	if err != nil {
		return CardEditResult{}, err
	}
	var result CardEditResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return CardEditResult{}, fmt.Errorf("parsing edit_card tool input: %w", err)
	}
	return result, nil
}

// formatCard renders the AI-editable subset of a Todo as plain text for
// the prompt. This is deliberately separate from store/frontmatter.go's
// Serialize, which produces the on-disk file format and covers a
// different field set (it also includes ID/Created/Links/Plan, none of
// which are under AI control here).
func formatCard(t *model.Todo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\n", t.Title)
	fmt.Fprintf(&b, "Status: %s\n", t.Status)
	if t.Status == "closed" {
		fmt.Fprintf(&b, "Closed: %s\n", t.Closed)
	}
	fmt.Fprintf(&b, "Categories: %s\n", formatCategoryList(t.Categories))
	fmt.Fprintf(&b, "Body:\n%s\n", t.Body)
	if len(t.Log) == 0 {
		b.WriteString("Log: (none)\n")
	} else {
		b.WriteString("Log:\n")
		for _, l := range t.Log {
			fmt.Fprintf(&b, "- %s: %s\n", l.Date, l.Text)
		}
	}
	return b.String()
}
