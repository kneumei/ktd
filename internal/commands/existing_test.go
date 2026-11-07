package commands

import "testing"

func TestHasLink(t *testing.T) {
	tests := []struct {
		name  string
		links []string
		url   string
		want  bool
	}{
		{"same PR", []string{"https://github.com/o/r/pull/76"}, "https://github.com/o/r/pull/76", true},
		{"PR subpage matches PR", []string{"https://github.com/o/r/pull/76"}, "https://github.com/o/r/pull/76/files", true},
		{"owner/repo case-insensitive", []string{"https://github.com/CareEvolution/R/pull/76"}, "https://github.com/careevolution/r/pull/76", true},
		{"different PR number", []string{"https://github.com/o/r/pull/76"}, "https://github.com/o/r/pull/77", false},
		{"issue vs PR with same number", []string{"https://github.com/o/r/issues/76"}, "https://github.com/o/r/pull/76", false},
		{"non-GitHub trailing slash", []string{"https://example.com/doc/"}, "https://example.com/doc", true},
		{"non-GitHub different", []string{"https://example.com/a"}, "https://example.com/b", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasLink(tt.links, tt.url); got != tt.want {
				t.Errorf("hasLink() = %v, want %v", got, tt.want)
			}
		})
	}
}
