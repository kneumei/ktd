// Package ai wraps the official Anthropic Go SDK for the handful of
// forced-tool-use calls this CLI makes. See parse.go for the specific
// operations (ParseAdd, ParseEdit, DraftWeekly).
package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Model is Claude Haiku 4.5 — cheap and fast, appropriate for the small
// structured-extraction tasks this CLI asks of it.
const Model = "claude-haiku-4-5"

// Client wraps the Anthropic SDK client.
type Client struct {
	sdk anthropic.Client
}

// NewClient returns a Client authenticated with apiKey. The SDK applies
// its own default timeout and automatic retries on 429/5xx.
func NewClient(apiKey string) *Client {
	return &Client{sdk: anthropic.NewClient(option.WithAPIKey(apiKey))}
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
func (c *Client) CallTool(ctx context.Context, system, userText string, t tool) (json.RawMessage, error) {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(Model),
		MaxTokens: 1024,
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(userText))},
		Tools:     []anthropic.ToolUnionParam{t.toToolParam()},
		ToolChoice: anthropic.ToolChoiceUnionParam{
			OfTool: &anthropic.ToolChoiceToolParam{Name: t.Name},
		},
	}
	if system != "" {
		params.System = []anthropic.TextBlockParam{{Text: system}}
	}

	msg, err := c.sdk.Messages.New(ctx, params)
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
