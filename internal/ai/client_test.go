package ai

import (
	"encoding/json"
	"testing"
)

// TestBuildParamsDials asserts the wire shape each dial combination
// produces — the defaults in particular, since "thinking omitted" means
// something different on Sonnet 5 than it did on Haiku 4.5 and the whole
// point of setting it explicitly is that the request never depends on
// which model is configured.
func TestBuildParamsDials(t *testing.T) {
	probe := tool{Name: "probe", InputSchema: map[string]any{"type": "object"}}

	tests := []struct {
		name  string
		cfg   Config
		check map[string]any
	}{
		{
			name: "defaults",
			cfg:  Config{},
			check: map[string]any{
				"model":                DefaultModel,
				"thinking.type":        "disabled",
				"output_config.effort": DefaultEffort,
			},
		},
		{
			name: "adaptive thinking opt-in",
			cfg:  Config{Thinking: ThinkingAdaptive},
			check: map[string]any{
				"thinking.type": "adaptive",
			},
		},
		{
			name: "effort off omits output_config entirely",
			cfg:  Config{Model: "claude-haiku-4-5", Effort: EffortOff},
			check: map[string]any{
				"model":         "claude-haiku-4-5",
				"output_config": nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(NewClient(tt.cfg).buildParams("sys", "hello", probe, 1024))
			if err != nil {
				t.Fatalf("marshaling params: %v", err)
			}
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("unmarshaling params: %v", err)
			}
			for path, want := range tt.check {
				if got := dig(body, path); got != want {
					t.Errorf("%s = %#v, want %#v (body: %s)", path, got, want, raw)
				}
			}
			// Forced tool use is the contract every parse.go operation
			// relies on; none of the dials may disturb it.
			if got := dig(body, "tool_choice.type"); got != "tool" {
				t.Errorf("tool_choice.type = %#v, want \"tool\"", got)
			}
		})
	}
}

// dig walks a dotted path into decoded JSON, returning nil for any key
// that isn't present.
func dig(body map[string]any, path string) any {
	var cur any = body
	for _, key := range splitDots(path) {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = m[key]
		if !ok {
			return nil
		}
	}
	return cur
}

func splitDots(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
