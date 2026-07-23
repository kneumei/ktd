// Package ai is a minimal hand-written Anthropic Messages API client
// (net/http, no SDK) — this project is a small dependency-light CLI, so a
// full SDK would be overkill for the handful of forced-tool-use calls it
// makes. See parse.go for the specific operations (ParseAdd, ParseEdit,
// DraftWeekly).
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	apiURL       = "https://api.anthropic.com/v1/messages"
	anthropicVer = "2023-06-01"
	// Model is Claude Haiku 4.5 — cheap and fast, appropriate for the small
	// structured-extraction tasks this CLI asks of it.
	Model = "claude-haiku-4-5"
)

// Client is a minimal Anthropic Messages API client.
type Client struct {
	APIKey     string
	HTTPClient *http.Client
}

// NewClient returns a Client with a sane request timeout.
func NewClient(apiKey string) *Client {
	return &Client{
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type toolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type messagesRequest struct {
	Model      string      `json:"model"`
	MaxTokens  int         `json:"max_tokens"`
	System     string      `json:"system,omitempty"`
	Messages   []message   `json:"messages"`
	Tools      []tool      `json:"tools,omitempty"`
	ToolChoice *toolChoice `json:"tool_choice,omitempty"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type messagesResponse struct {
	Content    []contentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
}

type apiErrorResponse struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// CallTool sends a single user-turn message with a forced tool_choice and
// returns the raw JSON input of the resulting tool_use block. system may
// be empty. This is the shape every parse.go operation is built on: one
// tool schema per operation, forced, so the response is deterministic
// JSON rather than free text that needs to be scraped out of a text block.
func (c *Client) CallTool(ctx context.Context, system, userText string, t tool) (json.RawMessage, error) {
	reqBody := messagesRequest{
		Model:     Model,
		MaxTokens: 1024,
		System:    system,
		Messages:  []message{{Role: "user", Content: userText}},
		Tools:     []tool{t},
		ToolChoice: &toolChoice{
			Type: "tool",
			Name: t.Name,
		},
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", anthropicVer)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling Anthropic API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr apiErrorResponse
		if jsonErr := json.Unmarshal(body, &apiErr); jsonErr == nil && apiErr.Error.Message != "" {
			return nil, fmt.Errorf("anthropic API error (%s, HTTP %d): %s", apiErr.Error.Type, resp.StatusCode, apiErr.Error.Message)
		}
		return nil, fmt.Errorf("anthropic API error: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var msg messagesResponse
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	for _, block := range msg.Content {
		if block.Type == "tool_use" && block.Name == t.Name {
			return block.Input, nil
		}
	}
	return nil, fmt.Errorf("no tool_use block for %q in response (stop_reason=%s)", t.Name, msg.StopReason)
}
