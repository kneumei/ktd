package commands

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"ktd/internal/model"
)

// confirmItems prints the proposed title/categories/links for each item
// and prompts once before the caller writes them. Every AI-driven write
// command (add/done/edit) goes through this — see the plan's
// confirm-before-write decision.
func confirmItems(verb string, items []*model.Todo) bool {
	if len(items) == 1 {
		fmt.Printf("📝 Proposed %s:\n", verb)
	} else {
		fmt.Printf("📝 Proposed %s (%d items):\n", verb, len(items))
	}
	for _, t := range items {
		fmt.Printf("  Title:      %s\n", t.Title)
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
	fmt.Print("💾 Write? [y/N] ")
	return readYesNo()
}

// readYesNo reads a single line from stdin and reports whether it was an
// affirmative response.
func readYesNo() bool {
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}

func formatCatsInline(cats []string) string {
	if len(cats) == 0 {
		return "[]"
	}
	return "[" + strings.Join(cats, ", ") + "]"
}
