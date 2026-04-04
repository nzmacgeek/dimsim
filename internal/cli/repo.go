package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/nzmacgeek/dimsim/internal/repo"
	"github.com/nzmacgeek/dimsim/internal/state"
)

// newRepoCmd returns the repo subcommand tree.
func newRepoCmd(db *state.DB) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Manage repositories",
	}
	cmd.AddCommand(newRepoAddCmd(db))
	cmd.AddCommand(newRepoListCmd(db))
	cmd.AddCommand(newRepoRemoveCmd(db))
	return cmd
}

func newRepoAddCmd(db *state.DB) *cobra.Command {
	var priority int

	cmd := &cobra.Command{
		Use:   "add <name> <url>",
		Short: "Add a repository",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			url := args[1]

			existing, err := db.GetRepo(name)
			if err != nil {
				return fmt.Errorf("check repo: %w", err)
			}
			if existing != nil {
				return fmt.Errorf("repository %q already exists (url: %s)", name, existing.URL)
			}

			client := repo.NewClient(db)
			return client.AddRepo(name, url, priority)
		},
	}

	cmd.Flags().IntVar(&priority, "priority", 100, "Repository priority (higher = preferred)")
	return cmd
}

func newRepoListCmd(db *state.DB) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured repositories",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repos, err := db.ListRepos()
			if err != nil {
				return err
			}

			if len(repos) == 0 {
				fmt.Println("No repositories configured. Use 'dimsim repo add <name> <url>' to add one.")
				return nil
			}

			fmt.Printf("%-20s %-8s %-8s %s\n", "NAME", "ENABLED", "PRIORITY", "URL")
			fmt.Printf("%-20s %-8s %-8s %s\n", "----", "-------", "--------", "---")
			for _, r := range repos {
				enabled := "yes"
				if !r.Enabled {
					enabled = "no"
				}
				fmt.Printf("%-20s %-8s %-8s %s\n", r.Name, enabled, strconv.Itoa(r.Priority), r.URL)
			}
			return nil
		},
	}
}

func newRepoRemoveCmd(db *state.DB) *cobra.Command {
	return &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm", "del"},
		Short:   "Remove a repository",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			r, err := db.GetRepo(name)
			if err != nil {
				return err
			}
			if r == nil {
				return fmt.Errorf("repository %q not found", name)
			}
			if err := db.RemoveRepo(name); err != nil {
				return err
			}
			fmt.Printf("✓ Removed repository %q\n", name)
			return nil
		},
	}
}
