// Package model defines the core Todo data structure shared across the
// store, resolve, ai, and commands packages.
package model

// LogEntry is one dated bullet under a todo's "## Log" section.
type LogEntry struct {
	Date string // YYYY-MM-DD
	Text string
}

// Todo mirrors the schema documented in the original todo-list project:
// YAML frontmatter (id, title, status, categories, created, closed, plan,
// links) followed by a freeform body and an optional "## Log" section.
type Todo struct {
	ID         string // 4-digit zero-padded, e.g. "0007"
	Title      string
	Status     string // "open" or "closed"
	Categories []string
	Created    string // YYYY-MM-DD
	Closed     string // YYYY-MM-DD, or "" while open
	Plan       string // "this-week" or ""
	Links      []string
	Body       string // freeform description, excluding the "## Log" section
	Log        []LogEntry
}

// IsOpen reports whether the todo's status is "open".
func (t *Todo) IsOpen() bool {
	return t.Status == "open"
}
