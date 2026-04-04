package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nzmacgeek/dimsim/internal/pkg"
)

func main() {
	root := &cobra.Command{
		Use:          "dpkbuild",
		Short:        "Build and scaffold .dpk packages",
		SilenceUsage: true,
	}

	root.AddCommand(newBuildCmd())
	root.AddCommand(newInitCmd())

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

			// Build file list with hashes
			var files []pkg.FileEntry
			err = filepath.Walk(payloadDir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if info.IsDir() {
					return nil
				}

				rel, err := filepath.Rel(payloadDir, path)
				if err != nil {
					return err
				}
				installPath := "/" + filepath.ToSlash(rel)

				hash, err := fileHash(path)
				if err != nil {
					return fmt.Errorf("hash %s: %w", path, err)
				}

				mode := fmt.Sprintf("%04o", info.Mode()&0777)

				files = append(files, pkg.FileEntry{
					Path: installPath,
					Hash: hash,
					Size: info.Size(),
					Mode: mode,
				})
				return nil
			})
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

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// --- embedded template scripts ---

const templatePreinst = `#!/bin/sh
# Pre-installation script
# Called before files are placed.
set -e

# Add your pre-installation steps here.
# Example: create users, stop services, etc.

exit 0
`

const templatePostinst = `#!/bin/sh
# Post-installation script
# Called after files are placed.
set -e

# Add your post-installation steps here.
# Example: enable services, update configs, etc.

exit 0
`

const templatePreRm = `#!/bin/sh
# Pre-removal script
# Called before files are removed.
set -e

# Add your pre-removal steps here.
# Example: stop services, backup configs, etc.

exit 0
`

const templatePostRm = `#!/bin/sh
# Post-removal script
# Called after files are removed.
set -e

# Add your post-removal steps here.
# Example: remove users, clean up data, etc.

exit 0
`
