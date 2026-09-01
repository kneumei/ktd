package commands

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"ktd/internal/model"
	"ktd/internal/store"
)

// confirmItems prints the proposed title/categories/links for each item
// and drives the shared y/N/e confirm loop before the caller writes them.
// Every AI-driven write command (add/done/edit) goes through this (edit
// via its own render/applyEdit pair — see edit.go's Edit).
func confirmItems(ctx context.Context, s *store.Store, noFetch bool, verb string, items []*model.Todo) bool {
	render := func() { printItems(verb, items) }
	applyEdit := func(idx int, instruction string) {
		diff, err := aiEditCard(ctx, s, items[idx], instruction, noFetch)
		if err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  edit failed: %v\n", err)
			return
		}
		if len(items) > 1 {
			fmt.Printf("  ✏️  item %d updated:\n", idx+1)
		} else {
			fmt.Println("  ✏️  updated:")
		}
		for _, l := range diff {
			fmt.Println("    " + l)
		}
	}
	return confirmLoop(items, "💾 Write?", render, applyEdit)
}

// printItems prints the proposed title/body/categories/links for each item.
func printItems(verb string, items []*model.Todo) {
	if len(items) == 1 {
		fmt.Printf("📝 Proposed %s:\n", verb)
	} else {
		fmt.Printf("📝 Proposed %s (%d items):\n", verb, len(items))
	}
	for _, t := range items {
		fmt.Printf("  Title:      %s\n", t.Title)
		if t.Body != "" {
			fmt.Printf("  Body:       %s\n", truncateForDisplay(t.Body, 300))
		}
		fmt.Printf("  Categories: %s\n", formatCatsInline(t.Categories))
		fmt.Printf("  Created:    %s\n", t.Created)
		if t.Status == "closed" {
			fmt.Printf("  Closed:     %s\n", t.Closed)
		}
		if len(t.Links) > 0 {
			fmt.Println("  Links:")
			for _, l := range t.Links {
				fmt.Println("    - " + l)
			}
		}
		if len(items) > 1 {
			fmt.Println()
		}
	}
}

// confirmLoop drives the shared y/N/e prompt. render prints the current
// proposed state — called once up front and again after every accepted
// 'e' edit. applyEdit is invoked with the chosen item index (always 0
// when len(items)==1) and the user's freeform instruction; it should
// mutate items in place and print/report anything it needs to. Returns
// true only once the user answers y.
func confirmLoop(items []*model.Todo, promptLabel string, render func(), applyEdit func(idx int, instruction string)) bool {
	render()
	for {
		fmt.Printf("%s [y/N/e] ", promptLabel)
		switch readAnswer() {
		case answerYes:
			return true
		case answerEdit:
			idx := 0
			if len(items) > 1 {
				n, ok := promptItemIndex(len(items))
				if !ok {
					continue // reprompt y/n/e
				}
				idx = n
			}
			fmt.Print("Describe the change: ")
			instruction := readLine()
			if instruction == "" {
				continue // cancel this edit, back to y/n/e
			}
			applyEdit(idx, instruction)
			render()
		default:
			return false
		}
	}
}

// promptItemIndex asks which of n items (1-based) to edit; blank cancels.
func promptItemIndex(n int) (int, bool) {
	for {
		fmt.Printf("Which item? [1-%d, blank to cancel] ", n)
		line := readLine()
		if line == "" {
			return 0, false
		}
		i, err := strconv.Atoi(line)
		if err != nil || i < 1 || i > n {
			fmt.Println("  invalid number")
			continue
		}
		return i - 1, true
	}
}

// answer is a parsed response to a y/N/e prompt.
type answer int

const (
	answerNo answer = iota
	answerYes
	answerEdit
)

// readAnswer reads a single line from stdin and classifies it as
// yes/edit/no (anything not recognized as y/yes or e/edit is "no").
func readAnswer() answer {
	switch strings.ToLower(readLine()) {
	case "y", "yes":
		return answerYes
	case "e", "edit":
		return answerEdit
	default:
		return answerNo
	}
}

// stdinReader is shared across every readLine call in a process run. A
// fresh bufio.Reader per call would silently drop already-buffered input
// whenever more than one line is read per invocation (as confirmLoop
// does) — bufio.Reader.Read pulls a whole chunk from the underlying
// stdin, and discarding the reader discards whatever of that chunk wasn't
// yet consumed.
var stdinReader = bufio.NewReader(os.Stdin)

// readLine reads a single line from stdin, trimmed.
func readLine() string {
	line, _ := stdinReader.ReadString('\n')
	return strings.TrimSpace(line)
}

func formatCatsInline(cats []string) string {
	if len(cats) == 0 {
		return "[]"
	}
	return "[" + strings.Join(cats, ", ") + "]"
}

// truncateForDisplay collapses newlines to spaces and clips s to at most n
// runes (appending an ellipsis) so a long AI-derived body summary doesn't
// blow up the confirm prompt.
func truncateForDisplay(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
