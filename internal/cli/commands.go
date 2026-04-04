package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nzmacgeek/dimsim/internal/install"
	"github.com/nzmacgeek/dimsim/internal/pkg"
	"github.com/nzmacgeek/dimsim/internal/repo"
	"github.com/nzmacgeek/dimsim/internal/state"
)

func newUpdateCmd(db *state.DB) *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Refresh TUF metadata for all repositories",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := repo.NewClient(db)
			fmt.Println("Updating repository metadata...")
			if err := client.UpdateAll(); err != nil {
				return err
			}
			fmt.Println("✓ Repository metadata updated.")
			return nil
		},
	}
}

func newSearchCmd(db *state.DB) *cobra.Command {
	return &cobra.Command{
		Use:   "search <query>",
		Short: "Search available packages",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := repo.NewClient(db)
			results, err := client.SearchPackages(args[0])
			if err != nil {
				return err
			}

			if len(results) == 0 {
				fmt.Printf("No packages found matching %q\n", args[0])
				return nil
			}

			fmt.Printf("%-25s %-12s %-10s %s\n", "NAME", "VERSION", "REPO", "DESCRIPTION")
			fmt.Printf("%-25s %-12s %-10s %s\n", "----", "-------", "----", "-----------")
			for _, r := range results {
				if r.Meta.Custom == nil {
					continue
				}
				desc := r.Meta.Custom.Description
				if len(desc) > 50 {
					desc = desc[:47] + "..."
				}
				fmt.Printf("%-25s %-12s %-10s %s\n",
					r.Meta.Custom.Name,
					r.Meta.Custom.Version,
					r.Repo,
					desc,
				)
			}
			return nil
		},
	}
}

func newInfoCmd(db *state.DB) *cobra.Command {
	return &cobra.Command{
		Use:   "info <package>",
		Short: "Show package information",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			// Check if installed
			installed, err := db.GetPackage(name)
			if err != nil {
				return err
			}

			client := repo.NewClient(db)
			available, _ := client.FindPackage(name)

			if installed == nil && available == nil {
				return fmt.Errorf("package not found: %s", name)
			}

			if installed != nil {
				pinned := ""
				if installed.Pinned {
					pinned = " [pinned]"
				}
				auto := ""
				if installed.Auto {
					auto = " [auto]"
				}
				fmt.Printf("Name:        %s\n", installed.Name)
				fmt.Printf("Version:     %s%s%s\n", installed.Version, pinned, auto)
				fmt.Printf("Arch:        %s\n", installed.Arch)
				fmt.Printf("Description: %s\n", installed.Description)
				if len(installed.Depends) > 0 {
					fmt.Printf("Depends:     %s\n", strings.Join(installed.Depends, ", "))
				}
				if len(installed.Provides) > 0 {
					fmt.Printf("Provides:    %s\n", strings.Join(installed.Provides, ", "))
				}
				fmt.Printf("Installed:   %s\n", installed.InstalledAt.Format("2006-01-02 15:04:05"))

				files, _ := db.GetFilesForPackage(name)
				if len(files) > 0 {
					fmt.Printf("Files:       %d file(s)\n", len(files))
				}
			}

			if available != nil && available.Meta.Custom != nil {
				c := available.Meta.Custom
				if installed != nil {
					a := pkg.ParseSemVer(c.Version)
					b := pkg.ParseSemVer(installed.Version)
					if a.Compare(b) > 0 {
						fmt.Printf("Available:   %s (upgrade available)\n", c.Version)
					}
				} else {
					fmt.Printf("Name:        %s\n", c.Name)
					fmt.Printf("Version:     %s\n", c.Version)
					fmt.Printf("Arch:        %s\n", c.Arch)
					fmt.Printf("Description: %s\n", c.Description)
					if len(c.Depends) > 0 {
						fmt.Printf("Depends:     %s\n", strings.Join(c.Depends, ", "))
					}
					fmt.Printf("Repository:  %s\n", available.Repo)
					fmt.Printf("Status:      not installed\n")
				}
			}

			return nil
		},
	}
}

func newInstallCmd(db *state.DB) *cobra.Command {
	return &cobra.Command{
		Use:   "install <package...>",
		Short: "Install packages",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := repo.NewClient(db)
			ins := install.New(db, client)
			return ins.Install(args, false)
		},
	}
}

func newRemoveCmd(db *state.DB) *cobra.Command {
	var purge bool
	cmd := &cobra.Command{
		Use:     "remove <package...>",
		Aliases: []string{"rm"},
		Short:   "Remove packages",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := repo.NewClient(db)
			ins := install.New(db, client)
			return ins.Remove(args, purge)
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "Also remove configuration files")
	return cmd
}

func newUpgradeCmd(db *state.DB) *cobra.Command {
	return &cobra.Command{
		Use:   "upgrade [package...]",
		Short: "Upgrade installed packages",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := repo.NewClient(db)
			ins := install.New(db, client)
			return ins.Upgrade(args)
		},
	}
}

func newAutoremoveCmd(db *state.DB) *cobra.Command {
	return &cobra.Command{
		Use:   "autoremove",
		Short: "Remove automatically installed packages no longer needed",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := repo.NewClient(db)
			ins := install.New(db, client)
			return ins.AutoRemove()
		},
	}
}

func newVerifyCmd(db *state.DB) *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Verify installed files against recorded hashes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := repo.NewClient(db)
			ins := install.New(db, client)
			return ins.Verify()
		},
	}
}

func newPinCmd(db *state.DB) *cobra.Command {
	return &cobra.Command{
		Use:   "pin <package>",
		Short: "Pin a package to prevent upgrades",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := db.SetPinned(args[0], true); err != nil {
				return err
			}
			fmt.Printf("✓ Pinned %s\n", args[0])
			return nil
		},
	}
}

func newUnpinCmd(db *state.DB) *cobra.Command {
	return &cobra.Command{
		Use:   "unpin <package>",
		Short: "Unpin a package to allow upgrades",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := db.SetPinned(args[0], false); err != nil {
				return err
			}
			fmt.Printf("✓ Unpinned %s\n", args[0])
			return nil
		},
	}
}

func newDoctorCmd(db *state.DB) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check system health",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := repo.NewClient(db)
			ins := install.New(db, client)
			return ins.Doctor()
		},
	}
}
