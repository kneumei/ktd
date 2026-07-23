// Command ktd is a fast, native CLI for a personal work-todo tracker.
// Mechanical subcommands (list, close, context, sync) are pure and
// instant; fuzzy subcommands (add, done, edit, weekly) make a single
// direct Anthropic API call to parse freeform input.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"ktd/internal/commands"
	"ktd/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	if cmd == "-h" || cmd == "--help" || cmd == "help" {
		printUsage()
		return
	}

	s, err := store.Open()
	if err != nil {
		fatal(err)
	}
	ctx := context.Background()

	switch cmd {
	case "add":
		runAdd(ctx, s, args)
	case "done":
		runDone(ctx, s, args)
	case "edit":
		runEdit(ctx, s, args)
	case "close":
		runClose(s, args)
	case "list", "ls":
		runList(s, args)
	case "context", "ctx":
		runContext(s, args)
	case "sync":
		runSync(s, args)
	case "weekly":
		runWeekly(ctx, s, args)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func runAdd(ctx context.Context, s *store.Store, args []string) {
	if len(args) == 0 {
		fatal(errors.New("usage: ktd add <text>"))
	}
	if err := commands.Add(ctx, s, strings.Join(args, " ")); err != nil {
		fatal(err)
	}
}

func runDone(ctx context.Context, s *store.Store, args []string) {
	asOf := ""
	rest := parseArgs(args, map[string]*string{"as-of": &asOf}, nil)

	text := strings.Join(rest, " ")
	if text == "" {
		fatal(errors.New("usage: ktd done <text> [--as-of YYYY-MM-DD]"))
	}
	if err := commands.Done(ctx, s, text, asOf); err != nil {
		fatal(err)
	}
}

func runEdit(ctx context.Context, s *store.Store, args []string) {
	if len(args) < 2 {
		fatal(errors.New("usage: ktd edit <id|text> <change>"))
	}
	query := args[0]
	change := strings.Join(args[1:], " ")
	if err := commands.Edit(ctx, s, query, change); err != nil {
		fatal(err)
	}
}

func runClose(s *store.Store, args []string) {
	asOf := ""
	rest := parseArgs(args, map[string]*string{"as-of": &asOf}, nil)

	if len(rest) == 0 {
		fatal(errors.New("usage: ktd close <id|text> [--as-of YYYY-MM-DD]"))
	}
	if err := commands.Close(s, strings.Join(rest, " "), asOf); err != nil {
		fatal(err)
	}
}

func runList(s *store.Store, args []string) {
	status := "open"
	sortBy := "category"
	sinceStr := "0"
	search := ""
	detail := false
	noColor := false

	rest := parseArgs(args,
		map[string]*string{"status": &status, "sort": &sortBy, "since": &sinceStr, "search": &search},
		map[string]*bool{"detail": &detail, "no-color": &noColor},
	)

	since, err := strconv.Atoi(sinceStr)
	if err != nil {
		fatal(fmt.Errorf("invalid --since value %q: expected an integer", sinceStr))
	}

	opts := commands.ListOptions{
		Category: strings.Join(rest, " "),
		Status:   status,
		Sort:     sortBy,
		Since:    since,
		Search:   search,
		Detail:   detail,
		NoColor:  noColor,
	}
	if err := commands.List(s, opts); err != nil {
		fatal(err)
	}
}

func runContext(s *store.Store, args []string) {
	if len(args) == 0 {
		fatal(errors.New("usage: ktd context <id|text>"))
	}
	if err := commands.Context(s, strings.Join(args, " ")); err != nil {
		fatal(err)
	}
}

func runSync(s *store.Store, args []string) {
	remote := ""
	parseArgs(args, map[string]*string{"remote": &remote}, nil)
	if err := commands.Sync(s, remote); err != nil {
		fatal(err)
	}
}

func runWeekly(ctx context.Context, s *store.Store, args []string) {
	weekOf := ""
	parseArgs(args, map[string]*string{"week-of": &weekOf}, nil)
	if err := commands.Weekly(ctx, s, weekOf); err != nil {
		fatal(err)
	}
}

// parseArgs scans args for known --name / --name=value / --name value flags
// in any position (not just before positionals — unlike the stdlib flag
// package, which stops parsing at the first non-flag argument, and would
// otherwise misparse e.g. `ktd close 0002 --as-of 2026-07-23`), writing
// matches into the provided destinations. It returns the remaining
// positional args in their original relative order.
func parseArgs(args []string, stringFlags map[string]*string, boolFlags map[string]*bool) []string {
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]

		matched := false
		for name, dst := range stringFlags {
			flagName := "--" + name
			if a == flagName {
				if i+1 < len(args) {
					*dst = args[i+1]
					i++
				}
				matched = true
				break
			}
			if strings.HasPrefix(a, flagName+"=") {
				*dst = strings.TrimPrefix(a, flagName+"=")
				matched = true
				break
			}
		}
		if matched {
			continue
		}

		for name, dst := range boolFlags {
			if a == "--"+name {
				*dst = true
				matched = true
				break
			}
		}
		if matched {
			continue
		}

		rest = append(rest, a)
	}
	return rest
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

func printUsage() {
	fmt.Println(`ktd - fast CLI for a personal work-todo tracker

Usage:
  ktd add <text>                      Parse freeform text into a new open item (AI)
  ktd done <text> [--as-of DATE]      Same, but files it already closed (AI)
  ktd edit <id|text> <change>         Resolve an item and apply a freeform change (AI)
  ktd close <id|text> [--as-of DATE]  Resolve an item and mark it closed
  ktd list [category] [flags]         List items
  ktd context <id|text>               Print full detail for one item
  ktd sync [--remote URL]             git add/commit/push the data dir
  ktd weekly [--week-of DATE]         Draft a Last/This weekly report (AI)

Flags may appear anywhere in the command (before or after positional args).

list flags:
  --status open|closed|all  (default open)
  --sort category|age|id    (default category)
  --since N                 items closed in the last N days (overrides --status)
  --search TEXT
  --detail
  --no-color`)
}
