// Command ktd is a fast, native CLI for a personal work-todo tracker.
// Mechanical subcommands (list, close, context, sync) are pure and
// instant; fuzzy subcommands (add, done, edit, weekly) make a single
// direct Anthropic API call to parse freeform input.
//
// Command dispatch and flag parsing are handled by cobra
// (github.com/spf13/cobra) — see newRootCmd and the per-command
// constructors below. Shell completion is intentionally not wired up.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"ktd/internal/commands"
	"ktd/internal/store"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "💥 error:", err)
	os.Exit(1)
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "ktd",
		Short: "Fast CLI for a personal work-todo tracker",
		Long: `ktd - fast CLI for a personal work-todo tracker

Mechanical subcommands (list, close, context, sync) are pure and instant;
fuzzy subcommands (add, done, edit, weekly) make a single direct
Anthropic API call to parse freeform input.

Flags may appear anywhere in the command (before or after positional
args), e.g. "ktd close 0002 --as-of 2026-07-23".`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.CompletionOptions.DisableDefaultCmd = true

	root.AddCommand(
		newAddCmd(),
		newDoneCmd(),
		newEditCmd(),
		newCloseCmd(),
		newListCmd(),
		newContextCmd(),
		newSyncCmd(),
		newWeeklyCmd(),
	)
	return root
}

func newAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <text>",
		Short: "Parse freeform text into a new open item (AI)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return err
			}
			return commands.Add(cmd.Context(), s, strings.Join(args, " "))
		},
	}
}

func newDoneCmd() *cobra.Command {
	var asOf string
	cmd := &cobra.Command{
		Use:   "done <text>",
		Short: "Same as add, but files it already closed (AI)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return err
			}
			return commands.Done(cmd.Context(), s, strings.Join(args, " "), asOf)
		},
	}
	cmd.Flags().StringVar(&asOf, "as-of", "", "file the item as closed as of this date (YYYY-MM-DD)")
	return cmd
}

func newEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit <id|text> <change>",
		Short: "Resolve an item and apply a freeform change (AI)",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return err
			}
			return commands.Edit(cmd.Context(), s, args[0], strings.Join(args[1:], " "))
		},
	}
}

func newCloseCmd() *cobra.Command {
	var asOf string
	cmd := &cobra.Command{
		Use:   "close <id|text>",
		Short: "Resolve an item and mark it closed",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return err
			}
			return commands.Close(s, strings.Join(args, " "), asOf)
		},
	}
	cmd.Flags().StringVar(&asOf, "as-of", "", "close as of this date (YYYY-MM-DD)")
	return cmd
}

func newListCmd() *cobra.Command {
	var status, sortBy, search string
	var since int
	var detail, noColor bool

	cmd := &cobra.Command{
		Use:     "list [category]",
		Aliases: []string{"ls"},
		Short:   "List items",
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return err
			}
			opts := commands.ListOptions{
				Category: strings.Join(args, " "),
				Status:   status,
				Sort:     sortBy,
				Since:    since,
				Search:   search,
				Detail:   detail,
				NoColor:  noColor,
			}
			return commands.List(s, opts)
		},
	}

	cmd.Flags().StringVar(&status, "status", "open", "open|closed|all")
	cmd.Flags().StringVar(&sortBy, "sort", "category", "category|age|id")
	cmd.Flags().IntVar(&since, "since", 0, "items closed in the last N days (overrides --status)")
	cmd.Flags().StringVar(&search, "search", "", "substring search across title/categories/body")
	cmd.Flags().BoolVar(&detail, "detail", false, "verbose per-item detail")
	cmd.Flags().BoolVar(&noColor, "no-color", false, "disable ANSI color output")

	return cmd
}

func newContextCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "context <id|text>",
		Aliases: []string{"ctx"},
		Short:   "Print full detail for one item",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return err
			}
			return commands.Context(s, strings.Join(args, " "))
		},
	}
}

func newSyncCmd() *cobra.Command {
	var remote string
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "git add/commit/push the data dir",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return err
			}
			return commands.Sync(s, remote)
		},
	}
	cmd.Flags().StringVar(&remote, "remote", "", "set the data dir's git remote before syncing")
	return cmd
}

func newWeeklyCmd() *cobra.Command {
	var weekOf string
	var noColor bool
	cmd := &cobra.Command{
		Use:   "weekly",
		Short: "Draft a Last/This weekly report (AI)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return err
			}
			return commands.Weekly(cmd.Context(), s, weekOf, noColor)
		},
	}
	cmd.Flags().StringVar(&weekOf, "week-of", "", "draft for the week containing this date (YYYY-MM-DD)")
	cmd.Flags().BoolVar(&noColor, "no-color", false, "disable ANSI bold output (plain markdown asterisks)")
	return cmd
}
