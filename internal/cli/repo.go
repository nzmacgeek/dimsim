package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/nzmacgeek/dimsim/internal/state"
)

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
