package ai

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestStringListUnmarshal(t *testing.T) {
	tests := []struct {
		in   string
		want StringList
	}{
		{`["Cadence","MDHD"]`, StringList{"Cadence", "MDHD"}},
		{`"[\"Cadence\"]"`, StringList{"Cadence"}},
		{`"Cadence"`, StringList{"Cadence"}},
		{`""`, nil},
		{`null`, nil},
	}
	for _, tt := range tests {
		var got StringList
		if err := json.Unmarshal([]byte(tt.in), &got); err != nil {
			t.Errorf("Unmarshal(%s): %v", tt.in, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Unmarshal(%s) = %q; want %q", tt.in, got, tt.want)
		}
	}
}

func TestExtractLinks(t *testing.T) {
	tests := []struct {
		in        string
		links     []string
		remainder string
	}{
		{
			in:        "review https://github.com/o/r/pull/1, category: mdhd",
			links:     []string{"https://github.com/o/r/pull/1"},
			remainder: "review , category: mdhd",
		},
		{
			in:        "see (https://example.com/a).",
			links:     []string{"https://example.com/a"},
			remainder: "see ().",
		},
		{
			in:        "no links here",
			remainder: "no links here",
		},
	}
	for _, tt := range tests {
		links, remainder := ExtractLinks(tt.in)
		if !reflect.DeepEqual(links, tt.links) || remainder != tt.remainder {
			t.Errorf("ExtractLinks(%q) = %q, %q; want %q, %q", tt.in, links, remainder, tt.links, tt.remainder)
		}
	}
}
