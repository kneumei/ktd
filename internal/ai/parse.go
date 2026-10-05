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
		// Keep the trimmed punctuation: "review <url>, category: x" must not
		// collapse to "review category: x".
		return url[len(trimmed):]
	})
	remainder = strings.Join(strings.Fields(remainder), " ")
	return links, remainder
}

// AddResult is the AI-distilled shape of a freeform `ktd add`/`ktd done`
// input: a short headline title, an optional body summary, and zero or more
// categories reusing the existing category set's casing.
type AddResult struct {
	Title      string   `json:"title"`
	Body       string   `json:"body"`
	Categories []string `json:"categories"`
	// StatedCategories is the categories the user's own text named
	// outright, reported separately from the inferred ones so the caller
	// can enforce "stated wins" itself rather than trusting the model to
	// hold back. See the addSystemPrompt note on why this is split out.
	StatedCategories []string `json:"stated_categories"`
	Date             string   `json:"date"`
}

const addSystemPrompt = `You help maintain a personal work-todo tracker. Given freeform text describing a task, distill:

- "title": a short, punchy headline (not a full sentence) that names the task.
- "body": only fill this in when a "Referenced GitHub items" block is given below the text — write a concise 1-2 sentence summary of what the referenced issue(s)/PR(s) are about, to serve as the item's description. Omit this field when no reference block is present (the caller falls back to the user's own text).
- "categories": zero or more freeform theme tags that apply, inferred only from what the text implies — never invent a category with no basis in the text. When an existing category clearly applies, reuse its exact casing rather than creating a near-duplicate.
- "stated_categories": the categories the user's own text names outright, however phrased — "category=mdmd", "category: mdmd", "categor is mdmd", "tag it mdmd", "file this under mdmd", "put it with the compass stuff". Copy the words they used, one entry per category, and leave this empty when they didn't name any. This is a transcription task, not a judgment call: report only what the user's own words state, never a category drawn from a "Referenced GitHub items" block (labels, repo name, issue content) or otherwise inferred — those belong in "categories". When the user states categories they are the complete answer, so the caller uses this list alone and discards "categories" entirely; don't try to merge the two yourself.
- "date": only if the text explicitly states a date the item applies to (absolute like "2026-07-25", or relative like "yesterday", "last Monday"), resolve it to YYYY-MM-DD using today's date, which is %s. Omit this field entirely if no date is stated. Never leave the resolved date sitting inside "title" — strip it out.

When a "Referenced GitHub items" block is present: if the user's own text is descriptive (more than just a bare link), prefer their own words for "title" and use the reference only to enrich "body". If the user supplied little or no text of their own, derive both "title" and "body" from the referenced item(s). Either way, work the referenced repo's name into "title" (humanize it, e.g. "file-transfer-service" -> "file transfer service") so the item is identifiable at a glance without opening the link — e.g. "File transfer service PR #76: Support Destination Overrides".

The user's relationship to a referenced item comes only from their own words. If their text names an action — even a single word like "review", "implement", "test", "deploy" — lead the title with it, e.g. "%s file transfer service PR #76: Support Destination Overrides". If it names none, never guess one: a bare link gets a neutral title naming the item itself.%s

Existing categories in use: %s

Respond only via the add_item tool.`

var addTool = tool{
	Name:        "add_item",
	Description: "Record the distilled title, body, and categories for a new todo item.",
	InputSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{
				"type":        "string",
				"description": "Short, headline-style title for the item.",
			},
			"body": map[string]any{
				"type":        "string",
				"description": "1-2 sentence summary derived from referenced GitHub items, if any were given. Omit otherwise.",
			},
			"categories": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Zero or more category tags implied by the text.",
			},
			"stated_categories": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "The categories the user's own words name outright, copied as written. Empty if they named none. Never inferred, and never taken from referenced GitHub items.",
			},
			"date": map[string]any{
				"type":        "string",
				"description": "Only if the text explicitly states a date (absolute or relative), resolved to YYYY-MM-DD. Omit otherwise.",
			},
		},
		"required":             []string{"title", "categories", "stated_categories"},
		"additionalProperties": false,
	},
}

// ParseAdd distills a title, optional body, categories, and an optional
// as-of date from freeform input text for `ktd add` / `ktd done`. today is
// passed as YYYY-MM-DD so the AI can resolve relative dates like
// "yesterday". Callers should run ExtractLinks first and pass the
// link-stripped remainder as text. reference, if non-empty, is a formatted
// block of fetched GitHub issue/PR content (see internal/github) appended
// to the user message as extra context — pass "" when there's none. done
// marks `ktd done` input, whose titles record finished work in past tense.
func ParseAdd(ctx context.Context, c *Client, existingCategories []string, today, text, reference string, done bool) (AddResult, error) {
	verb, tense := "Review", ""
	if done {
		verb, tense = "Reviewed", ` This item records work already finished, so put the action in past tense ("review" -> "Reviewed").`
	}
	system := fmt.Sprintf(addSystemPrompt, today, verb, tense, formatCategoryList(existingCategories))
	raw, err := c.CallTool(ctx, system, withReference(text, reference), addTool)
	if err != nil {
		return AddResult{}, err
	}
	var result AddResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return AddResult{}, fmt.Errorf("parsing add_item tool input: %w", err)
	}
	return result, nil
}

// WeeklyItemSummary is the AI's distilled one-line summary of a single
// candidate item for the Last or This section of the weekly report.
// Category grouping and links are deliberately not the AI's job — they're
// done mechanically by the caller from each item's actual categories/links,
// the same way `ktd list` groups by category, since that's data the AI would
// otherwise have to (unreliably) echo back correctly.
type WeeklyItemSummary struct {
	ID   string `json:"id"`
	Text string `json:"text"`
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
- "text": a short phrase naming the action taken and, briefly, the problem/goal it addresses — e.g. "Reviewed compass-prompt-compute-metrics changes" or "Created infra for Acme integration". Not a full sentence, no em dash, no trailing period.

Do not group or categorize, and do not include links — both are added mechanically by the caller. Just list distilled items per section, in the order given. Omit trivial or redundant items; keep each section scannable, not exhaustive.

Respond only via the draft_weekly tool.`

var weeklyItemSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"id":   map[string]any{"type": "string", "description": "The item's id, copied exactly as given in the input."},
		"text": map[string]any{"type": "string", "description": "Short action-focused summary phrase for the item, no narration or links."},
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

// UnmarshalJSON accepts either section as a JSON-encoded string, which the
// model occasionally emits instead of a real array.
func (r *WeeklyResult) UnmarshalJSON(data []byte) error {
	var raw struct {
		Last json.RawMessage `json:"last"`
		This json.RawMessage `json:"this"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var err error
	if r.Last, err = decodeSummaries(raw.Last); err != nil {
		return fmt.Errorf("last: %w", err)
	}
	if r.This, err = decodeSummaries(raw.This); err != nil {
		return fmt.Errorf("this: %w", err)
	}
	return nil
}

func decodeSummaries(raw json.RawMessage) ([]WeeklyItemSummary, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var encoded string
	if json.Unmarshal(raw, &encoded) == nil {
		raw = json.RawMessage(encoded)
	}
	var items []WeeklyItemSummary
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	return items, nil
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

// withReference appends a formatted block of fetched GitHub issue/PR
// content to the user's own text, delimited clearly so the model can tell
// the user's words apart from reference material. Returns text unchanged
// when reference is empty.
func withReference(text, reference string) string {
	if reference == "" {
		return text
	}
	return text + "\n\n--- Referenced GitHub items ---\n" + reference
}

func formatCategoryList(categories []string) string {
	if len(categories) == 0 {
		return "(none yet)"
	}
	return strings.Join(categories, ", ")
}
