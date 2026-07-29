package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// urlRe matches bare URLs in freeform text. Trailing punctuation that's
// almost certainly sentence structure, not part of the URL, is trimmed off
// by ExtractLinks.
var urlRe = regexp.MustCompile(`https?://[^\s<>"]+`)

// ExtractLinks pulls URLs out of freeform text deterministically (no AI
// call needed for this — SKILL.md treats link extraction as reliable
// regex work). It returns the links found and the text with those links
// removed, so the remainder can be handed to the AI for title/category/
// classification work without the URLs cluttering it.
func ExtractLinks(text string) (links []string, remainder string) {
	remainder = urlRe.ReplaceAllStringFunc(text, func(url string) string {
		trimmed := strings.TrimRight(url, ".,;:!?)]}")
		links = append(links, trimmed)
		return ""
	})
	remainder = strings.Join(strings.Fields(remainder), " ")
	return links, remainder
}

// AddResult is the AI-distilled shape of a freeform `ktd add`/`ktd done`
// input: a short headline title and zero or more categories reusing the
// existing category set's casing.
type AddResult struct {
	Title      string   `json:"title"`
	Categories []string `json:"categories"`
	Date       string   `json:"date"`
}

const addSystemPrompt = `You help maintain a personal work-todo tracker. Given freeform text describing a task, distill:

- "title": a short, punchy headline (not a full sentence) that names the task.
- "categories": zero or more freeform theme tags that apply, inferred only from what the text implies — never invent a category with no basis in the text. When an existing category clearly applies, reuse its exact casing rather than creating a near-duplicate.
- "date": only if the text explicitly states a date the item applies to (absolute like "2026-07-25", or relative like "yesterday", "last Monday"), resolve it to YYYY-MM-DD using today's date, which is %s. Omit this field entirely if no date is stated. Never leave the resolved date sitting inside "title" — strip it out.

Existing categories in use: %s

Respond only via the add_item tool.`

var addTool = tool{
	Name:        "add_item",
	Description: "Record the distilled title and categories for a new todo item.",
	InputSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{
				"type":        "string",
				"description": "Short, headline-style title for the item.",
			},
			"categories": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Zero or more category tags implied by the text.",
			},
			"date": map[string]any{
				"type":        "string",
				"description": "Only if the text explicitly states a date (absolute or relative), resolved to YYYY-MM-DD. Omit otherwise.",
			},
		},
		"required":             []string{"title", "categories"},
		"additionalProperties": false,
	},
}

// ParseAdd distills a title, categories, and an optional as-of date from
// freeform input text for `ktd add` / `ktd done`. today is passed as
// YYYY-MM-DD so the AI can resolve relative dates like "yesterday".
// Callers should run ExtractLinks first and pass the link-stripped
// remainder as text.
func ParseAdd(ctx context.Context, c *Client, existingCategories []string, today, text string) (AddResult, error) {
	system := fmt.Sprintf(addSystemPrompt, today, formatCategoryList(existingCategories))
	raw, err := c.CallTool(ctx, system, text, addTool)
	if err != nil {
		return AddResult{}, err
	}
	var result AddResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return AddResult{}, fmt.Errorf("parsing add_item tool input: %w", err)
	}
	return result, nil
}

// EditClassification is the kind of change an `ktd edit` input represents.
type EditClassification string

const (
	ClassificationLogNote        EditClassification = "log_note"
	ClassificationBodyAddition   EditClassification = "body_addition"
	ClassificationCategoryChange EditClassification = "category_change"
	ClassificationCloseItem      EditClassification = "close_item"
)

// EditResult is the AI classification of a freeform `ktd edit` change.
// Exactly the fields relevant to Classification are meaningful; the rest
// are zero values. Date is populated for log_note and close_item when the
// text explicitly states an as-of date.
type EditResult struct {
	Classification   EditClassification `json:"classification"`
	LogText          string             `json:"log_text"`
	BodyAddition     string             `json:"body_addition"`
	CategoriesAdd    []string           `json:"categories_add"`
	CategoriesRemove []string           `json:"categories_remove"`
	Date             string             `json:"date"`
}

const editSystemPrompt = `You help maintain a personal work-todo tracker. Given a change a user wants to apply to an existing item, classify it as exactly one of:

- "log_note": a progress update ("worked on X", "did Y today") — write a concise version to log_text.
- "body_addition": more description or context to append to the item — write it to body_addition.
- "category_change": the user wants to add and/or remove category tags — list them in categories_add / categories_remove, reusing existing casing where an existing category applies.
- "close_item": the user wants to mark the item closed/done, e.g. "close it", "mark this done", "close date is yesterday" — no log_text/body_addition needed.

For log_note and close_item, if the text explicitly states a date the work/close applies to (absolute like "2026-07-25", or relative like "yesterday", "last Monday"), resolve it to YYYY-MM-DD in "date" using today's date, which is %s. Omit "date" if none is stated. Never leave the resolved date sitting inside log_text.

Existing categories in use: %s

Respond only via the classify_edit tool, and only fill in the field(s) relevant to your chosen classification.`

var editTool = tool{
	Name:        "classify_edit",
	Description: "Classify a freeform edit request and extract the relevant content.",
	InputSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"classification": map[string]any{
				"type": "string",
				"enum": []string{"log_note", "body_addition", "category_change", "close_item"},
			},
			"log_text": map[string]any{
				"type":        "string",
				"description": "Only when classification is log_note.",
			},
			"body_addition": map[string]any{
				"type":        "string",
				"description": "Only when classification is body_addition.",
			},
			"categories_add": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Only when classification is category_change.",
			},
			"categories_remove": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Only when classification is category_change.",
			},
			"date": map[string]any{
				"type":        "string",
				"description": "Only when classification is log_note or close_item, and the text states an explicit date. Resolved to YYYY-MM-DD.",
			},
		},
		"required":             []string{"classification"},
		"additionalProperties": false,
	},
}

// ParseEdit classifies a freeform `ktd edit` change. today is passed as
// YYYY-MM-DD so the AI can resolve relative dates like "yesterday".
// Callers should run ExtractLinks first and append any found links to the
// item directly (mechanically) rather than relying on this classification
// for links.
func ParseEdit(ctx context.Context, c *Client, existingCategories []string, today, text string) (EditResult, error) {
	system := fmt.Sprintf(editSystemPrompt, today, formatCategoryList(existingCategories))
	raw, err := c.CallTool(ctx, system, text, editTool)
	if err != nil {
		return EditResult{}, err
	}
	var result EditResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return EditResult{}, fmt.Errorf("parsing classify_edit tool input: %w", err)
	}
	return result, nil
}

// WeeklyItemSummary is the AI's distilled one-line summary of a single
// candidate item for the Last or This section of the weekly report.
// Category grouping and layout are deliberately not the AI's job — they're
// done mechanically by the caller from each item's actual categories, the
// same way `ktd list` groups by category, since that's data the AI would
// otherwise have to (unreliably) echo back correctly.
type WeeklyItemSummary struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Link string `json:"link"`
}

// WeeklyResult holds the AI-summarized candidate items for both sections
// of `ktd weekly`.
type WeeklyResult struct {
	Last []WeeklyItemSummary `json:"last"`
	This []WeeklyItemSummary `json:"this"`
}

const weeklySystemPrompt = `You help draft a weekly status report from a personal work-todo tracker. You're given candidate items for "Last" (recently completed or active work) and "This" (upcoming work), each prefixed with "id=", and possibly a "body:"/"log" detail lines with the actual substance of what happened.

For each item worth reporting, distill it to:
- "id": copied exactly from the input.
- "text": a short "<name for the task/project> — <what happened or what's planned>" line, drawing on the body/log detail if present. Not a full sentence, no trailing period.
- "link": the item's most useful URL, only if one was given and it adds real value; omit otherwise.

Do not group or categorize — just list distilled items per section, in the order given. Omit trivial or redundant items; keep each section scannable, not exhaustive.

Respond only via the draft_weekly tool.`

var weeklyItemSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"id":   map[string]any{"type": "string", "description": "The item's id, copied exactly as given in the input."},
		"text": map[string]any{"type": "string", "description": "Short distilled summary line for the item."},
		"link": map[string]any{"type": "string", "description": "The item's most useful URL, if any adds value."},
	},
	"required":             []string{"id", "text"},
	"additionalProperties": false,
}

var weeklyTool = tool{
	Name:        "draft_weekly",
	Description: "Provide distilled Last/This item summaries for the weekly report.",
	InputSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"last": map[string]any{
				"type":        "array",
				"items":       weeklyItemSchema,
				"description": "Distilled summaries for the Last section.",
			},
			"this": map[string]any{
				"type":        "array",
				"items":       weeklyItemSchema,
				"description": "Distilled summaries for the This section.",
			},
		},
		"required":             []string{"last", "this"},
		"additionalProperties": false,
	},
}

// DraftWeekly distills the Last/This candidate items into short summary
// lines given a plain-text summary of the relevant items (closed-this-week
// items and open items with in-week log activity, plus open items
// favoring plan:this-week for the This section). Callers assemble that
// summary text from the store and do the category grouping themselves.
func DraftWeekly(ctx context.Context, c *Client, itemsSummary string) (WeeklyResult, error) {
	raw, err := c.CallTool(ctx, weeklySystemPrompt, itemsSummary, weeklyTool)
	if err != nil {
		return WeeklyResult{}, err
	}
	var result WeeklyResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return WeeklyResult{}, fmt.Errorf("parsing draft_weekly tool input: %w", err)
	}
	return result, nil
}

func formatCategoryList(categories []string) string {
	if len(categories) == 0 {
		return "(none yet)"
	}
	return strings.Join(categories, ", ")
}
