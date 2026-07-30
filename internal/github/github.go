// Package github detects GitHub issue/PR links and fetches their
// title/body/state via the `gh` CLI, so that content can be folded into an
// AI prompt as reference context. Deliberately shells out to `gh` (mirroring
// this project's existing precedent of shelling out to `git`) rather than
// requiring a personal access token — it reuses whatever `gh auth login`
// session the user already has, with no token to rotate. Every function here
// is best-effort: callers are expected to treat a missing `gh`, an
// unauthenticated session, or a fetch error as "no enrichment available" and
// carry on, never as a hard failure.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// maxBodyChars caps how much of an issue/PR body gets pulled into the AI
// prompt, so one huge issue can't blow up the request.
const maxBodyChars = 4000

// fetchTimeout bounds how long a single `gh` invocation may run, so a hung
// or slow network call can't stall the command indefinitely.
const fetchTimeout = 15 * time.Second

// refRe matches GitHub issue/PR URLs. The host must contain "github" (so
// this never fires `gh` against an unrelated URL); "pull" maps to a PR,
// "issues" to an issue.
var refRe = regexp.MustCompile(`^https?://[^/]*github[^/]*/([^/]+)/([^/]+)/(issues|pull)/(\d+)`)

// Ref identifies a single GitHub issue or PR referenced by URL.
type Ref struct {
	URL    string
	Kind   string // "issue" or "pr"
	Number string
}

// DetectRefs scans an already-extracted link slice (see ai.ExtractLinks) and
// returns the GitHub issue/PR references found among them, in order.
func DetectRefs(links []string) []Ref {
	var refs []Ref
	for _, l := range links {
		m := refRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		kind := "issue"
		if m[3] == "pull" {
			kind = "pr"
		}
		refs = append(refs, Ref{URL: l, Kind: kind, Number: m[4]})
	}
	return refs
}

// Fetched holds the details of a Ref pulled from `gh`.
type Fetched struct {
	Ref
	Title  string
	Body   string
	State  string
	Author string
}

// ghView is the shape of `gh issue/pr view --json title,body,state,number,author`.
type ghView struct {
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
}

// Available reports whether the `gh` CLI is on PATH.
func Available() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// Fetch runs `gh issue view` or `gh pr view` for r and returns the parsed
// result. Errors (gh missing, unauthenticated, not found, no access,
// timeout) are all returned as plain errors — callers should treat any
// error here as "skip enrichment for this ref", not a fatal condition.
func Fetch(ctx context.Context, r Ref) (Fetched, error) {
	sub := "issue"
	if r.Kind == "pr" {
		sub = "pr"
	}

	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gh", sub, "view", r.URL, "--json", "title,body,state,number,author")
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return Fetched{}, fmt.Errorf("gh %s view %s: %w: %s", sub, r.URL, err, strings.TrimSpace(string(ee.Stderr)))
		}
		return Fetched{}, fmt.Errorf("gh %s view %s: %w", sub, r.URL, err)
	}

	var v ghView
	if err := json.Unmarshal(out, &v); err != nil {
		return Fetched{}, fmt.Errorf("parsing gh %s view output: %w", sub, err)
	}

	body := v.Body
	if len(body) > maxBodyChars {
		body = body[:maxBodyChars] + "…"
	}

	return Fetched{
		Ref:    r,
		Title:  v.Title,
		Body:   body,
		State:  v.State,
		Author: v.Author.Login,
	}, nil
}

// FormatContext renders fetched items into a compact block suitable for
// appending to an AI prompt as reference material.
func FormatContext(fs []Fetched) string {
	if len(fs) == 0 {
		return ""
	}
	var b strings.Builder
	for _, f := range fs {
		label := "Issue"
		if f.Kind == "pr" {
			label = "PR"
		}
		fmt.Fprintf(&b, "%s #%s (%s) %q", label, f.Number, strings.ToLower(f.State), f.Title)
		if f.Author != "" {
			fmt.Fprintf(&b, " by %s", f.Author)
		}
		b.WriteString(":\n")
		if f.Body != "" {
			b.WriteString(f.Body)
		} else {
			b.WriteString("(no description)")
		}
		b.WriteString("\n\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
