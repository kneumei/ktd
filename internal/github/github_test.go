package github

import "testing"

func TestRepoFromLinks(t *testing.T) {
	tests := []struct {
		name  string
		links []string
		want  string
	}{
		{"pull request", []string{"https://github.com/CareEvolution/file-transfer-service/pull/76"}, "file-transfer-service"},
		{"issue", []string{"https://github.com/o/r/issues/12"}, "r"},
		{"first GitHub ref wins", []string{"https://example.com/x", "https://github.com/a/b/pull/1", "https://github.com/c/d/issues/2"}, "b"},
		{"repo root link is not an issue or PR", []string{"https://github.com/o/r"}, ""},
		{"no links", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RepoFromLinks(tt.links); got != tt.want {
				t.Errorf("RepoFromLinks() = %q, want %q", got, tt.want)
			}
		})
	}
}
