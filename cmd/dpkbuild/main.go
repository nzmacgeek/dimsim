package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nzmacgeek/dimsim/internal/pkg"
	"github.com/nzmacgeek/dimsim/internal/repo"
)

func main() {
	root := &cobra.Command{
		Use:          "dpkbuild",
		Short:        "Build and scaffold .dpk packages",
		SilenceUsage: true,
	}

	root.AddCommand(newBuildCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newRepoCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func newBuildCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "build [dir]",
		Short: "Build a .dpk package from a directory",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}

			dir, err := filepath.Abs(dir)
			if err != nil {
				return fmt.Errorf("resolve path: %w", err)
			}

			manifestPath := filepath.Join(dir, "meta", "manifest.json")
			manifestData, err := os.ReadFile(manifestPath)
			if err != nil {
				return fmt.Errorf("read manifest: %w\nExpected at: %s", err, manifestPath)
			}

			manifest, err := pkg.ParseManifest(manifestData)
			if err != nil {
				return fmt.Errorf("parse manifest: %w", err)
			}

			if manifest.Arch == "" {
				manifest.Arch = runtime.GOARCH
			}

			payloadDir := filepath.Join(dir, "payload")
			if _, err := os.Stat(payloadDir); os.IsNotExist(err) {
				return fmt.Errorf("payload directory not found at %s", payloadDir)
			}

			files, err := pkg.BuildManifestFileEntries(payloadDir)
			if err != nil {
				return fmt.Errorf("walk payload: %w", err)
			}
			manifest.Files = files

			// Load scripts
			scriptsDir := filepath.Join(dir, "meta", "scripts")
			loadScript := func(name string) string {
				data, err := os.ReadFile(filepath.Join(scriptsDir, name))
				if err != nil {
					return ""
				}
				return string(data)
			}
			manifest.Scripts = pkg.Scripts{
				PreInst:  loadScript("preinst"),
				PostInst: loadScript("postinst"),
				PreRm:    loadScript("prerm"),
				PostRm:   loadScript("postrm"),
			}

			outFile := manifest.Filename()
			fmt.Printf("Building %s...\n", outFile)

			if err := pkg.WriteDpk(outFile, manifest, payloadDir); err != nil {
				return fmt.Errorf("write dpk: %w", err)
			}

			stat, err := os.Stat(outFile)
			if err != nil {
				return err
			}

			// Print the hash
			data, err := os.ReadFile(outFile)
			if err != nil {
				return err
			}
			h := sha256.Sum256(data)
			fmt.Printf("✓ Built %s (%d bytes)\n", outFile, stat.Size())
			fmt.Printf("  SHA256: %s\n", hex.EncodeToString(h[:]))

			return nil
		},
	}
}

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init <name>",
		Short: "Scaffold a new package directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			// Validate name
			if strings.ContainsAny(name, " /\\:") {
				return fmt.Errorf("package name must not contain spaces or special characters")
			}

			dirs := []string{
				filepath.Join(name, "meta", "scripts"),
				filepath.Join(name, "payload"),
			}
			for _, d := range dirs {
				if err := os.MkdirAll(d, 0755); err != nil {
					return fmt.Errorf("create directory %s: %w", d, err)
				}
			}

			manifestPath := filepath.Join(name, "meta", "manifest.json")
			if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
				manifest := pkg.Manifest{
					Name:        name,
					Version:     "0.1.0",
					Arch:        runtime.GOARCH,
					Description: "Description of " + name,
					Depends:     []string{},
					Maintainer:  "Your Name <your@email.com>",
					Homepage:    "https://example.com/" + name,
				}
				data, err := json.MarshalIndent(manifest, "", "  ")
				if err != nil {
					return err
				}
				if err := os.WriteFile(manifestPath, append(data, '\n'), 0644); err != nil {
					return fmt.Errorf("write manifest: %w", err)
				}
			}

			scripts := map[string]string{
				"preinst":  templatePreinst,
				"postinst": templatePostinst,
				"prerm":    templatePreRm,
				"postrm":   templatePostRm,
			}
			for scriptName, content := range scripts {
				path := filepath.Join(name, "meta", "scripts", scriptName)
				if _, err := os.Stat(path); os.IsNotExist(err) {
					if err := os.WriteFile(path, []byte(content), 0755); err != nil {
						return fmt.Errorf("write %s: %w", scriptName, err)
					}
				}
			}

			gitkeep := filepath.Join(name, "payload", ".gitkeep")
			if _, err := os.Stat(gitkeep); os.IsNotExist(err) {
				os.WriteFile(gitkeep, nil, 0644)
			}

			fmt.Printf("✓ Scaffolded package directory: %s/\n", name)
			fmt.Printf("  Edit %s to configure your package.\n", manifestPath)
			fmt.Printf("  Add files to %s/payload/ to include them in the package.\n", name)
			fmt.Printf("  Run 'dpkbuild build %s' to build the package.\n", name)
			return nil
		},
	}
}

// --- repo subcommand ---

// keyFile is the path to the private key file inside a repo directory.
const keyFile = "keys/root.key"

// targetsExpiry is how long targets.json stays valid after publishing.
const targetsExpiry = 365 * 24 * time.Hour

// snapshotExpiry is how long snapshot.json stays valid.
const snapshotExpiry = 30 * 24 * time.Hour

// timestampExpiry is how long timestamp.json stays valid before needing a refresh.
const timestampExpiry = 7 * 24 * time.Hour

func newRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Manage a dimsim TUF repository",
	}
	cmd.AddCommand(newRepoInitCmd())
	cmd.AddCommand(newRepoAddPackageCmd())
	cmd.AddCommand(newRepoRefreshCmd())
	return cmd
}

// newRepoInitCmd creates a new TUF repository directory.
func newRepoInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init <dir>",
		Short: "Initialise a new TUF repository",
		Long: `Creates a repository directory with the following layout:

  <dir>/
    root.json        TUF root metadata (self-signed)
    targets.json     Package index (initially empty)
    snapshot.json    Hash of targets.json
    timestamp.json   Hash of snapshot.json (refresh weekly)
    packages/        Place .dpk files here
    keys/
      root.key       Private signing key — keep this secret!
`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}

			for _, d := range []string{filepath.Join(dir, "packages"), filepath.Join(dir, "keys")} {
				if err := os.MkdirAll(d, 0755); err != nil {
					return fmt.Errorf("create %s: %w", d, err)
				}
			}

			keyPath := filepath.Join(dir, keyFile)
			if _, err := os.Stat(keyPath); err == nil {
				return fmt.Errorf("repository already initialised at %s (key exists)", dir)
			}

			rk, tufKey, err := repo.GenerateKey()
			if err != nil {
				return err
			}

			// Write private key (mode 0600)
			rkData, _ := json.MarshalIndent(rk, "", "  ")
			if err := os.WriteFile(keyPath, rkData, 0600); err != nil {
				return fmt.Errorf("write key: %w", err)
			}

			now := time.Now().UTC()

			// Empty targets.json (version 1)
			targetsData, err := repo.BuildTargets(nil, 1, now.Add(targetsExpiry), rk)
			if err != nil {
				return fmt.Errorf("build targets: %w", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "targets.json"), targetsData, 0644); err != nil {
				return err
			}

			snapshotData, err := repo.BuildSnapshot(targetsData, 1, 1, now.Add(snapshotExpiry), rk)
			if err != nil {
				return fmt.Errorf("build snapshot: %w", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "snapshot.json"), snapshotData, 0644); err != nil {
				return err
			}

			timestampData, err := repo.BuildTimestamp(snapshotData, 1, 1, now.Add(timestampExpiry), rk)
			if err != nil {
				return fmt.Errorf("build timestamp: %w", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "timestamp.json"), timestampData, 0644); err != nil {
				return err
			}

			rootData, err := repo.BuildRoot(tufKey, rk.KeyID, 1, now.Add(targetsExpiry), rk)
			if err != nil {
				return fmt.Errorf("build root: %w", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "root.json"), rootData, 0644); err != nil {
				return err
			}

			fmt.Printf("✓ Initialised repository at %s\n", dir)
			fmt.Printf("  Key ID: %s\n", rk.KeyID)
			fmt.Printf("  Private key: %s\n", keyPath)
			fmt.Printf("  ⚠  Keep %s secret — it is used to sign all repository metadata.\n", keyFile)
			fmt.Printf("\nServe the directory over HTTP and add it with:\n")
			fmt.Printf("  dimsim repo add <name> http://<host>/<path>\n")
			return nil
		},
	}
}

// newRepoAddPackageCmd adds a .dpk file to an existing TUF repository.
func newRepoAddPackageCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add-package <repo-dir> <package.dpk>",
		Short: "Add a .dpk to the repository and re-sign metadata",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			dpkPath, err := filepath.Abs(args[1])
			if err != nil {
				return err
			}

			rk, err := loadKey(filepath.Join(dir, keyFile))
			if err != nil {
				return err
			}

			// Read package
			dpkData, err := os.ReadFile(dpkPath)
			if err != nil {
				return fmt.Errorf("read dpk: %w", err)
			}
			manifest, err := pkg.ReadDpkManifest(dpkPath)
			if err != nil {
				return fmt.Errorf("read manifest: %w", err)
			}

			// Load existing targets
			targets, targetsVer, err := loadTargets(dir, rk)
			if err != nil {
				return err
			}

			// Build target entry
			filename := manifest.Filename()
			custom := &repo.TUFTargetCustom{
				Name:        manifest.Name,
				Version:     manifest.Version,
				Arch:        manifest.Arch,
				Description: manifest.Description,
				Depends:     manifest.Depends,
				Provides:    manifest.Provides,
			}
			targets[filename] = repo.DpkTargetMeta(dpkData, custom)

			// Copy .dpk into packages/
			destDpk := filepath.Join(dir, "packages", filename)
			if err := copyFile(dpkData, destDpk); err != nil {
				return fmt.Errorf("copy dpk: %w", err)
			}

			// Re-sign everything
			targetsVer++
			now := time.Now().UTC()
			if err := rewriteMetadata(dir, targets, targetsVer, now, rk); err != nil {
				return err
			}

			fmt.Printf("✓ Added %s to repository\n", filename)
			fmt.Printf("  targets.json version: %d\n", targetsVer)
			return nil
		},
	}
}

// newRepoRefreshCmd re-signs snapshot.json and timestamp.json with fresh expiry.
func newRepoRefreshCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh <repo-dir>",
		Short: "Re-sign snapshot and timestamp to extend their expiry",
		Long:  "Run at least once a week to keep timestamp.json from expiring.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}

			rk, err := loadKey(filepath.Join(dir, keyFile))
			if err != nil {
				return err
			}

			targets, targetsVer, err := loadTargets(dir, rk)
			if err != nil {
				return err
			}

			// Bump only snapshot + timestamp versions, keep targets as-is
			now := time.Now().UTC()
			if err := rewriteMetadata(dir, targets, targetsVer, now, rk); err != nil {
				return err
			}

			fmt.Printf("✓ Repository metadata refreshed\n")
			fmt.Printf("  timestamp.json valid until: %s\n", now.Add(timestampExpiry).Format("2006-01-02"))
			return nil
		},
	}
}

// --- helpers ---

func loadKey(path string) (repo.RepoKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return repo.RepoKey{}, fmt.Errorf("read key file %s: %w\nHint: run 'dpkbuild repo init <dir>' first", path, err)
	}
	var rk repo.RepoKey
	if err := json.Unmarshal(data, &rk); err != nil {
		return repo.RepoKey{}, fmt.Errorf("parse key file: %w", err)
	}
	return rk, nil
}

func loadTargets(dir string, rk repo.RepoKey) (map[string]repo.TUFTargetMeta, int, error) {
	targetsPath := filepath.Join(dir, "targets.json")
	targetsRaw, err := os.ReadFile(targetsPath)
	if err != nil {
		return nil, 0, fmt.Errorf("read targets.json: %w", err)
	}

	// We need to extract version and targets map without full TUF verification
	// (we own this repo, so we just parse it directly).
	var envelope struct {
		Signed struct {
			Version int                           `json:"version"`
			Targets map[string]repo.TUFTargetMeta `json:"targets"`
		} `json:"signed"`
	}
	if err := json.Unmarshal(targetsRaw, &envelope); err != nil {
		return nil, 0, fmt.Errorf("parse targets.json: %w", err)
	}

	targets := envelope.Signed.Targets
	if targets == nil {
		targets = map[string]repo.TUFTargetMeta{}
	}
	return targets, envelope.Signed.Version, nil
}

func rewriteMetadata(dir string, targets map[string]repo.TUFTargetMeta, targetsVer int, now time.Time, rk repo.RepoKey) error {
	targetsData, err := repo.BuildTargets(targets, targetsVer, now.Add(targetsExpiry), rk)
	if err != nil {
		return fmt.Errorf("build targets: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "targets.json"), targetsData, 0644); err != nil {
		return err
	}

	// Read current snapshot to determine its version
	snapVer := 1
	if raw, err := os.ReadFile(filepath.Join(dir, "snapshot.json")); err == nil {
		var s struct {
			Signed struct {
				Version int `json:"version"`
			} `json:"signed"`
		}
		if json.Unmarshal(raw, &s) == nil {
			snapVer = s.Signed.Version + 1
		}
	}

	snapshotData, err := repo.BuildSnapshot(targetsData, targetsVer, snapVer, now.Add(snapshotExpiry), rk)
	if err != nil {
		return fmt.Errorf("build snapshot: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "snapshot.json"), snapshotData, 0644); err != nil {
		return err
	}

	// Read current timestamp to determine its version
	tsVer := 1
	if raw, err := os.ReadFile(filepath.Join(dir, "timestamp.json")); err == nil {
		var t struct {
			Signed struct {
				Version int `json:"version"`
			} `json:"signed"`
		}
		if json.Unmarshal(raw, &t) == nil {
			tsVer = t.Signed.Version + 1
		}
	}

	timestampData, err := repo.BuildTimestamp(snapshotData, snapVer, tsVer, now.Add(timestampExpiry), rk)
	if err != nil {
		return fmt.Errorf("build timestamp: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "timestamp.json"), timestampData, 0644); err != nil {
		return err
	}

	return nil
}

func copyFile(data []byte, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

// --- embedded template scripts ---

const templatePreinst = `#!/bin/bash
# Pre-installation script
# Called before files are placed.
set -e

# Add your pre-installation steps here.
# Example: create users, stop services, etc.

exit 0
`

const templatePostinst = `#!/bin/bash
# Post-installation script
# Called after files are placed.
set -e

# Add your post-installation steps here.
# Example: enable services, update configs, etc.

exit 0
`

const templatePreRm = `#!/bin/bash
# Pre-removal script
# Called before files are removed.
set -e

# Add your pre-removal steps here.
# Example: stop services, backup configs, etc.

exit 0
`

const templatePostRm = `#!/bin/bash
# Post-removal script
# Called after files are removed.
set -e

# Add your post-removal steps here.
# Example: remove users, clean up data, etc.

exit 0
`
