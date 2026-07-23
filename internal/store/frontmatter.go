package store

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"ktd/internal/model"
)

var (
	frontmatterRe = regexp.MustCompile(`(?s)^---\r?\n(.*?)\r?\n---\r?\n?(.*)$`)
	logSplitRe    = regexp.MustCompile(`(?s)^(.*?)\r?\n##\s*Log\s*\r?\n?(.*)$`)
	logEntryRe    = regexp.MustCompile(`^\s*-\s*(\d{4}-\d{2}-\d{2}):\s*(.*)$`)
)

// frontmatterYAML is the wire shape of the YAML frontmatter block. Real
// todo-list data mixes inline ([a, b]) and block (- a\n- b) forms for
// categories/links, and sometimes leaves categories blank — yaml.v3
// unmarshals all of these into []string (nil when blank) without special
// casing, which is why we lean on it for reads instead of hand-parsing.
type frontmatterYAML struct {
	ID         string   `yaml:"id"`
	Title      string   `yaml:"title"`
	Status     string   `yaml:"status"`
	Categories []string `yaml:"categories"`
	Created    string   `yaml:"created"`
	Closed     string   `yaml:"closed"`
	Plan       string   `yaml:"plan"`
	Links      []string `yaml:"links"`
}

// ParseFile parses a full todo .md file (frontmatter + body + optional
// "## Log" section) into a model.Todo.
func ParseFile(raw string) (*model.Todo, error) {
	m := frontmatterRe.FindStringSubmatch(raw)
	if m == nil {
		return nil, fmt.Errorf("no YAML frontmatter found")
	}
	var fm frontmatterYAML
	if err := yaml.Unmarshal([]byte(m[1]), &fm); err != nil {
		return nil, fmt.Errorf("parsing frontmatter: %w", err)
	}

	t := &model.Todo{
		ID:         fm.ID,
		Title:      fm.Title,
		Status:     fm.Status,
		Categories: fm.Categories,
		Created:    fm.Created,
		Closed:     fm.Closed,
		Plan:       fm.Plan,
		Links:      fm.Links,
	}

	rest := strings.TrimLeft(m[2], "\n")
	body, log := splitBodyAndLog(rest)
	t.Body = strings.TrimRight(body, "\n")
	t.Log = parseLogEntries(log)
	return t, nil
}

func splitBodyAndLog(rest string) (body, log string) {
	m := logSplitRe.FindStringSubmatch(rest)
	if m == nil {
		return rest, ""
	}
	return m[1], m[2]
}

// parseLogEntries parses "- YYYY-MM-DD: text" bullets. Unlike the original
// PowerShell parser, a wrapped indented continuation line is preserved by
// appending it to the previous entry's text rather than being dropped.
func parseLogEntries(log string) []model.LogEntry {
	var entries []model.LogEntry
	for _, line := range strings.Split(log, "\n") {
		if m := logEntryRe.FindStringSubmatch(line); m != nil {
			entries = append(entries, model.LogEntry{Date: m[1], Text: m[2]})
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || len(entries) == 0 {
			continue
		}
		last := &entries[len(entries)-1]
		last.Text = strings.TrimRight(last.Text, " ") + " " + trimmed
	}
	return entries
}

// Serialize renders a model.Todo back into the canonical .md file format:
// YAML frontmatter in a fixed field order (categories inline/flow, links
// one-per-line), then the body, then an optional "## Log" section. This is
// the one true output format — every write goes through here, regardless
// of how the source file was originally formatted.
func Serialize(t *model.Todo) (string, error) {
	fm := &yaml.Node{Kind: yaml.MappingNode}

	addScalar := func(key, value string) {
		k := &yaml.Node{}
		_ = k.Encode(key)
		v := &yaml.Node{}
		_ = v.Encode(value)
		fm.Content = append(fm.Content, k, v)
	}
	addSeq := func(key string, values []string, flow bool) {
		k := &yaml.Node{}
		_ = k.Encode(key)
		seq := &yaml.Node{Kind: yaml.SequenceNode}
		if flow {
			seq.Style = yaml.FlowStyle
		}
		for _, val := range values {
			item := &yaml.Node{}
			_ = item.Encode(val)
			seq.Content = append(seq.Content, item)
		}
		fm.Content = append(fm.Content, k, seq)
	}

	addScalar("id", t.ID)
	addScalar("title", t.Title)
	addScalar("status", t.Status)
	addSeq("categories", t.Categories, true)
	addScalar("created", t.Created)
	addScalar("closed", t.Closed)
	addScalar("plan", t.Plan)
	addSeq("links", t.Links, false)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(fm); err != nil {
		return "", fmt.Errorf("encoding frontmatter: %w", err)
	}
	_ = enc.Close()

	var out strings.Builder
	out.WriteString("---\n")
	out.WriteString(buf.String())
	out.WriteString("---\n\n")
	out.WriteString(strings.TrimRight(t.Body, "\n"))
	out.WriteString("\n")

	if len(t.Log) > 0 {
		out.WriteString("\n## Log\n")
		for _, e := range t.Log {
			fmt.Fprintf(&out, "- %s: %s\n", e.Date, e.Text)
		}
	}

	return out.String(), nil
}
