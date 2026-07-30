package commands

import (
	"context"
	"fmt"
	"os"

	"ktd/internal/github"
)

// buildReference detects GitHub issue/PR links among the already-extracted
// links and fetches each via the `gh` CLI, returning a formatted block
// suitable for passing to ai.ParseAdd/ai.ParseEdit as reference context.
// Returns "" when noFetch is set, `gh` isn't available, or no GitHub refs
// are found. Per-ref fetch errors are warned to stderr and skipped rather
// than treated as fatal — enrichment is always best-effort, never required
// for the command to succeed.
func buildReference(ctx context.Context, links []string, noFetch bool) string {
	if noFetch {
		return ""
	}
	refs := github.DetectRefs(links)
	if len(refs) == 0 {
		return ""
	}
	if !github.Available() {
		fmt.Fprintln(os.Stderr, "warning: gh CLI not found on PATH — skipping issue/PR summary")
		return ""
	}

	var fetched []github.Fetched
	for _, r := range refs {
		f, err := github.Fetch(ctx, r)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: fetching %s: %v\n", r.URL, err)
			continue
		}
		fetched = append(fetched, f)
	}
	return github.FormatContext(fetched)
}
