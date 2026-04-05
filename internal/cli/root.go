package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nzmacgeek/dimsim/internal/install"
	"github.com/nzmacgeek/dimsim/internal/repo"
	"github.com/nzmacgeek/dimsim/internal/state"
)

// Execute is the main entry point for the dimsim CLI.
func Execute() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// withDB opens the state DB at the system default path, calls f, then closes it.
func withDB(f func(db *state.DB) error) error {
	return withDBAt("", f)
}

// withDBAt opens (or creates) the state DB rooted at rootDir, calls f, then
// closes it. When rootDir is empty the standard system paths are used.
func withDBAt(rootDir string, f func(db *state.DB) error) error {
	db, err := state.OpenAt(rootDir)
	if err != nil {
		hint := "/var/lib/dimsim/"
		if rootDir != "" {
			hint = rootDir + hint
		}
		return fmt.Errorf("open state database: %w\nHint: ensure %s is writable or run as root", err, hint)
	}
	defer db.Close()
	return f(db)
}

// rootDirFromCmd reads the --root persistent flag value from the root command.
func rootDirFromCmd(cmd *cobra.Command) string {
	v, _ := cmd.Root().PersistentFlags().GetString("root")
	return v
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "dimsim",
		Short:        "dimsim - BlueyOS package manager",
		Long:         "dimsim is the package manager for BlueyOS, providing TUF-secured package management.",
		SilenceUsage: true,
	}

	// --root makes every subcommand operate on a target root filesystem
	// rather than the running system. Useful for offline provisioning.
	cmd.PersistentFlags().String("root", "", "Operate on a target root filesystem (offline mode)")

	cmd.AddCommand(buildRepoCmd())
	cmd.AddCommand(buildUpdateCmd())
	cmd.AddCommand(buildSearchCmd())
	cmd.AddCommand(buildInfoCmd())
	cmd.AddCommand(buildInstallCmd())
	cmd.AddCommand(buildRemoveCmd())
	cmd.AddCommand(buildUpgradeCmd())
	cmd.AddCommand(buildAutoremoveCmd())
	cmd.AddCommand(buildVerifyCmd())
	cmd.AddCommand(buildPinCmd())
	cmd.AddCommand(buildUnpinCmd())
	cmd.AddCommand(buildDoctorCmd())

	return cmd
}

func buildRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Manage repositories",
	}

	var priority int
	addCmd := &cobra.Command{
		Use:          "add <name> <url>",
		Short:        "Add a repository",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, _ := cmd.Flags().GetInt("priority")
			return withDBAt(rootDirFromCmd(cmd), func(db *state.DB) error {
				existing, err := db.GetRepo(args[0])
				if err != nil {
					return err
				}
				if existing != nil {
					return fmt.Errorf("repository %q already exists (url: %s)", args[0], existing.URL)
				}
				client := repo.NewClient(db)
				return client.AddRepo(args[0], args[1], p)
			})
		},
	}
	addCmd.Flags().IntVar(&priority, "priority", 100, "Repository priority (higher = preferred)")

	listCmd := &cobra.Command{
		Use:          "list",
		Short:        "List configured repositories",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDBAt(rootDirFromCmd(cmd), func(db *state.DB) error {
				return newRepoListCmd(db).RunE(cmd, args)
			})
		},
	}

	removeCmd := &cobra.Command{
		Use:          "remove <name>",
		Aliases:      []string{"rm", "del"},
		Short:        "Remove a repository",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDBAt(rootDirFromCmd(cmd), func(db *state.DB) error {
				return newRepoRemoveCmd(db).RunE(cmd, args)
			})
		},
	}

	cmd.AddCommand(addCmd, listCmd, removeCmd)
	return cmd
}

func buildUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "update",
		Short:        "Refresh TUF metadata for all repositories",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDBAt(rootDirFromCmd(cmd), func(db *state.DB) error {
				return newUpdateCmd(db).RunE(cmd, args)
			})
		},
	}
}

func buildSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "search <query>",
		Short:        "Search available packages",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDBAt(rootDirFromCmd(cmd), func(db *state.DB) error {
				return newSearchCmd(db).RunE(cmd, args)
			})
		},
	}
}

func buildInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "info <package>",
		Short:        "Show package information",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDBAt(rootDirFromCmd(cmd), func(db *state.DB) error {
				return newInfoCmd(db).RunE(cmd, args)
			})
		},
	}
}

// isLocalFilePath reports whether s looks like a local .dpk file path rather
// than a package name. An argument is treated as a local file if it ends with
// ".dpk" or begins with a path separator prefix ("/", "./", "../").
func isLocalFilePath(s string) bool {
	return strings.HasSuffix(s, ".dpk") ||
		strings.HasPrefix(s, "/") ||
		strings.HasPrefix(s, "./") ||
		strings.HasPrefix(s, "../")
}

func buildInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "install <package...>",
		Short:        "Install packages",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			rootDir := rootDirFromCmd(cmd)
			return withDBAt(rootDir, func(db *state.DB) error {
				client := repo.NewClient(db)
				ins := install.NewOffline(rootDir, db, client)

				var filePaths, pkgNames []string
				for _, arg := range args {
					if isLocalFilePath(arg) {
						filePaths = append(filePaths, arg)
					} else {
						pkgNames = append(pkgNames, arg)
					}
				}

				if len(filePaths) > 0 {
					if err := ins.InstallFiles(filePaths, false); err != nil {
						return err
					}
				}
				if len(pkgNames) > 0 {
					if err := ins.Install(pkgNames, false); err != nil {
						return err
					}
				}
				return nil
			})
		},
	}
}

func buildRemoveCmd() *cobra.Command {
	var purge bool
	cmd := &cobra.Command{
		Use:          "remove <package...>",
		Aliases:      []string{"rm"},
		Short:        "Remove packages",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, _ := cmd.Flags().GetBool("purge")
			rootDir := rootDirFromCmd(cmd)
			return withDBAt(rootDir, func(db *state.DB) error {
				client := repo.NewClient(db)
				ins := install.NewOffline(rootDir, db, client)
				return ins.Remove(args, p)
			})
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "Also remove configuration files")
	return cmd
}

func buildUpgradeCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "upgrade [package...]",
		Short:        "Upgrade installed packages",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			rootDir := rootDirFromCmd(cmd)
			return withDBAt(rootDir, func(db *state.DB) error {
				client := repo.NewClient(db)
				ins := install.NewOffline(rootDir, db, client)
				return ins.Upgrade(args)
			})
		},
	}
}

func buildAutoremoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "autoremove",
		Short:        "Remove automatically installed packages no longer needed",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			rootDir := rootDirFromCmd(cmd)
			return withDBAt(rootDir, func(db *state.DB) error {
				client := repo.NewClient(db)
				ins := install.NewOffline(rootDir, db, client)
				return ins.AutoRemove()
			})
		},
	}
}

func buildVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "verify",
		Short:        "Verify installed files against recorded hashes",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			rootDir := rootDirFromCmd(cmd)
			return withDBAt(rootDir, func(db *state.DB) error {
				client := repo.NewClient(db)
				ins := install.NewOffline(rootDir, db, client)
				return ins.Verify()
			})
		},
	}
}

func buildPinCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "pin <package>",
		Short:        "Pin a package to prevent upgrades",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDBAt(rootDirFromCmd(cmd), func(db *state.DB) error {
				if err := db.SetPinned(args[0], true); err != nil {
					return err
				}
				fmt.Printf("✓ Pinned %s\n", args[0])
				return nil
			})
		},
	}
}

func buildUnpinCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "unpin <package>",
		Short:        "Unpin a package to allow upgrades",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDBAt(rootDirFromCmd(cmd), func(db *state.DB) error {
				if err := db.SetPinned(args[0], false); err != nil {
					return err
				}
				fmt.Printf("✓ Unpinned %s\n", args[0])
				return nil
			})
		},
	}
}

func buildDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "doctor",
		Short:        "Check system health",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			rootDir := rootDirFromCmd(cmd)
			return withDBAt(rootDir, func(db *state.DB) error {
				client := repo.NewClient(db)
				ins := install.NewOffline(rootDir, db, client)
				return ins.Doctor()
			})
		},
	}
}
