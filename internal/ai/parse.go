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
}

const addSystemPrompt = `You help maintain a personal work-todo tracker. Given freeform text describing a task, distill:

- "title": a short, punchy headline (not a full sentence) that names the task.
- "categories": zero or more freeform theme tags that apply, inferred only from what the text implies — never invent a category with no basis in the text. When an existing category clearly applies, reuse its exact casing rather than creating a near-duplicate.

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
		},
		"required":             []string{"title", "categories"},
		"additionalProperties": false,
	},
}

// ParseAdd distills a title and categories from freeform input text for
// `ktd add` / `ktd done`. Callers should run ExtractLinks first and pass
// the link-stripped remainder as text.
func ParseAdd(ctx context.Context, c *Client, existingCategories []string, text string) (AddResult, error) {
	system := fmt.Sprintf(addSystemPrompt, formatCategoryList(existingCategories))
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
)

// EditResult is the AI classification of a freeform `ktd edit` change.
// Exactly the fields relevant to Classification are meaningful; the rest
// are zero values.
type EditResult struct {
	Classification   EditClassification `json:"classification"`
	LogText          string             `json:"log_text"`
	BodyAddition     string             `json:"body_addition"`
	CategoriesAdd    []string           `json:"categories_add"`
	CategoriesRemove []string           `json:"categories_remove"`
}

const editSystemPrompt = `You help maintain a personal work-todo tracker. Given a change a user wants to apply to an existing item, classify it as exactly one of:

- "log_note": a progress update ("worked on X", "did Y today") — write a concise version to log_text.
- "body_addition": more description or context to append to the item — write it to body_addition.
- "category_change": the user wants to add and/or remove category tags — list them in categories_add / categories_remove, reusing existing casing where an existing category applies.

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
				"enum": []string{"log_note", "body_addition", "category_change"},
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
		},
		"required":             []string{"classification"},
		"additionalProperties": false,
	},
}

// ParseEdit classifies a freeform `ktd edit` change. Callers should run
// ExtractLinks first and append any found links to the item directly
// (mechanically) rather than relying on this classification for links.
func ParseEdit(ctx context.Context, c *Client, existingCategories []string, text string) (EditResult, error) {
	system := fmt.Sprintf(editSystemPrompt, formatCategoryList(existingCategories))
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

// WeeklyResult holds the drafted "Last"/"This" markdown report body for
// `ktd weekly`.
type WeeklyResult struct {
	Markdown string `json:"markdown"`
}

const weeklySystemPrompt = `You draft a weekly status report from a personal work-todo tracker, formatted for pasting as rich text into email/docs.

Output two sections, "Last" and "This", each with items grouped under bold theme headers (categories), and uncategorized items as standalone top-level bullets:

**Last**
**<Theme>**
- <item line>

**This**
**<Theme>**
- <item line>

Only "Last"/"This" are bold at the top level; theme names are bold sub-headers. Use "-" bullet lists. One tight line per item; include a link inline only if it adds value. Keep it scannable, not exhaustive.

Respond only via the draft_weekly tool.`

var weeklyTool = tool{
	Name:        "draft_weekly",
	Description: "Provide the drafted Last/This weekly report as markdown.",
	InputSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"markdown": map[string]any{
				"type":        "string",
				"description": "The full Last/This report body, in the format described in the system prompt.",
			},
		},
		"required":             []string{"markdown"},
		"additionalProperties": false,
	},
}

// DraftWeekly drafts the Last/This markdown report body given a plain-text
// summary of the relevant items (closed-this-week items and open items
// with in-week log activity, plus open items favoring plan:this-week for
// the This section). Callers assemble that summary text from the store.
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
