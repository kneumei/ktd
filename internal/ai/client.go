// Package ai wraps the official Anthropic Go SDK for the handful of
// forced-tool-use calls this CLI makes. See parse.go and card_edit.go for
// the specific operations (ParseAdd, ParseCardEdit, DraftWeekly).
package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Defaults for the three model dials. Claude Sonnet 5 at low effort with
// thinking off: these are small structured-extraction tasks behind an
// interactive prompt, so the win is judgment per token, not reasoning
// depth — and every one of them blocks a terminal until it returns.
// Sonnet over Haiku 4.5 costs a couple of dollars a month at personal-CLI
// volume, which is cheaper than re-editing an item ktd got wrong.
const (
	DefaultModel    = "claude-sonnet-5"
	DefaultEffort   = "low"
	DefaultThinking = ThinkingOff
)

// Thinking modes accepted by Config.Thinking. Off is explicit rather than
// left to the model's own default, because that default differs by model:
// omitting the field means no thinking on Haiku 4.5 but adaptive thinking
// on Sonnet 5, so an unset field would silently change behavior the moment
// the model dial moved.
const (
	ThinkingOff      = "off"
	ThinkingAdaptive = "adaptive"
)

// EffortOff disables the effort dial, omitting output_config entirely.
// Needed for models that reject the parameter — Haiku 4.5 among them — so
// pointing the model dial at one doesn't just start returning 400s.
const EffortOff = "off"

// Config holds everything the caller chooses per call site: the API key
// plus the three dials, each of which the user can override (see
// internal/commands' newAIClient). Empty fields take the defaults above.
type Config struct {
	APIKey   string
	Model    string
	Effort   string
	Thinking string
}

// Client wraps the Anthropic SDK client together with the resolved dials,
// so CallTool's shape stays the same regardless of how they were set.
type Client struct {
	sdk      anthropic.Client
	model    string
	effort   string
	thinking string
}

// NewClient returns a Client authenticated with cfg.APIKey, with any empty
// dial filled in from the defaults. The SDK applies its own default
// timeout and automatic retries on 429/5xx.
func NewClient(cfg Config) *Client {
	c := &Client{
		sdk:      anthropic.NewClient(option.WithAPIKey(cfg.APIKey)),
		model:    orDefault(cfg.Model, DefaultModel),
		effort:   orDefault(cfg.Effort, DefaultEffort),
		thinking: orDefault(cfg.Thinking, DefaultThinking),
	}
	return c
}

// Model reports the model this client is configured to call, for display.
func (c *Client) Model() string { return c.model }

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// tool describes a single forced-tool-use tool: a name, description, and
// JSON schema with keys "type", "properties", "required", and
// "additionalProperties", matching the shape every parse.go operation
// builds by hand.
type tool struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// toToolParam converts the local tool description into the SDK's typed
// tool param.
func (t tool) toToolParam() anthropic.ToolUnionParam {
	schema := anthropic.ToolInputSchemaParam{}
	if props, ok := t.InputSchema["properties"]; ok {
		schema.Properties = props
	}
	if required, ok := t.InputSchema["required"].([]string); ok {
		schema.Required = required
	}
	if addl, ok := t.InputSchema["additionalProperties"]; ok {
		schema.ExtraFields = map[string]any{"additionalProperties": addl}
	}

	return anthropic.ToolUnionParam{
		OfTool: &anthropic.ToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: schema,
		},
	}
}

// CallTool sends a single user-turn message with a forced tool_choice and
// returns the raw JSON input of the resulting tool_use block. system may
// be empty. This is the shape every parse.go operation is built on: one
// tool schema per operation, forced, so the response is deterministic
// JSON rather than free text that needs to be scraped out of a text block.
// It caps the response at 1024 output tokens; use CallToolMax for
// operations that may need to echo back more content than that.
func (c *Client) CallTool(ctx context.Context, system, userText string, t tool) (json.RawMessage, error) {
	return c.CallToolMax(ctx, system, userText, t, 1024)
}

// CallToolMax is CallTool with an explicit output token cap, for
// operations (like ParseCardEdit) that may need to echo back more content
// than CallTool's default budget allows.
func (c *Client) CallToolMax(ctx context.Context, system, userText string, t tool, maxTokens int64) (json.RawMessage, error) {
	msg, err := c.sdk.Messages.New(ctx, c.buildParams(system, userText, t, maxTokens))
	if err != nil {
		return nil, fmt.Errorf("calling Anthropic API: %w", err)
	}

	for _, block := range msg.Content {
		if variant, ok := block.AsAny().(anthropic.ToolUseBlock); ok && variant.Name == t.Name {
			return variant.Input, nil
		}
	}
	return nil, fmt.Errorf("no tool_use block for %q in response (stop_reason=%s)", t.Name, msg.StopReason)
}

// buildParams assembles the request for one forced-tool-use call, applying
// the client's dials. Split out from CallToolMax so the wire shape the
// dials produce can be asserted without making an API call.
func (c *Client) buildParams(system, userText string, t tool, maxTokens int64) anthropic.MessageNewParams {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: maxTokens,
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(userText))},
		Tools:     []anthropic.ToolUnionParam{t.toToolParam()},
		ToolChoice: anthropic.ToolChoiceUnionParam{
			OfTool: &anthropic.ToolChoiceToolParam{Name: t.Name},
		},
		Thinking: thinkingParam(c.thinking),
	}
	if c.effort != EffortOff {
		params.OutputConfig = anthropic.OutputConfigParam{
			Effort: anthropic.OutputConfigEffort(c.effort),
		}
	}
	if system != "" {
		params.System = []anthropic.TextBlockParam{{Text: system}}
	}
	return params
}

// thinkingParam maps the thinking dial onto the SDK's config union.
// Anything other than "adaptive" disables thinking — including the
// default — so the fast path is the one you get without asking.
func thinkingParam(mode string) anthropic.ThinkingConfigParamUnion {
	if mode == ThinkingAdaptive {
		return anthropic.ThinkingConfigParamUnion{
			OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{},
		}
	}
	return anthropic.ThinkingConfigParamUnion{
		OfDisabled: &anthropic.ThinkingConfigDisabledParam{},
	}
}
