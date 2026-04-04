package install

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nzmacgeek/dimsim/internal/pkg"
	"github.com/nzmacgeek/dimsim/internal/repo"
	"github.com/nzmacgeek/dimsim/internal/state"
	"github.com/nzmacgeek/dimsim/internal/world"
)

// Installer handles package installation, removal, and upgrade.
type Installer struct {
	db      *state.DB
	client  *repo.Client
	// RootDir, when non-empty, makes all file operations relative to this
	// directory. Used for offline installs into a non-booted target system.
	RootDir string
}

// New creates a new Installer for the running system.
func New(db *state.DB, client *repo.Client) *Installer {
	return &Installer{db: db, client: client}
}

// NewOffline creates an Installer that operates on a target rootfs at rootDir.
// File operations are performed relative to rootDir; lifecycle scripts are
// staged as claw firstboot services instead of being executed immediately.
func NewOffline(rootDir string, db *state.DB, client *repo.Client) *Installer {
	return &Installer{db: db, client: client, RootDir: rootDir}
}

// isOffline returns true when the installer is operating on a target rootfs.
func (ins *Installer) isOffline() bool {
	return ins.RootDir != ""
}

// rootPath prepends ins.RootDir to the path when in offline mode.
// It uses strings.TrimLeft so that absolute paths (starting with /) are
// correctly joined under RootDir rather than discarding the root prefix.
func (ins *Installer) rootPath(p string) string {
	if !ins.isOffline() {
		return p
	}
	return filepath.Join(ins.RootDir, strings.TrimLeft(p, string(filepath.Separator)))
}

// worldFilePath returns the path to the world file (possibly rooted).
func (ins *Installer) worldFilePath() string {
	return ins.rootPath(state.WorldFile)
}

// stagingDir returns the staging directory (possibly rooted).
func (ins *Installer) stagingDir() string {
	return ins.rootPath(state.StagingDir)
}

// cacheDir returns the cache directory (possibly rooted).
func (ins *Installer) cacheDir() string {
	return ins.rootPath(state.CacheDir)
}

// Install installs the given packages with full dependency resolution.
// If auto is true, packages are marked as automatically installed.
func (ins *Installer) Install(names []string, auto bool) error {
	// Resolve all packages including dependencies
	plan, err := ins.resolveDeps(names)
	if err != nil {
		return fmt.Errorf("dependency resolution: %w", err)
	}

	if len(plan) == 0 {
		fmt.Println("Nothing to install.")
		return nil
	}

	fmt.Printf("The following packages will be installed:\n")
	for _, item := range plan {
		marker := ""
		for _, explicit := range names {
			if explicit == item.Name {
				marker = " [explicit]"
				break
			}
		}
		fmt.Printf("  %s %s%s\n", item.Name, item.Version, marker)
	}

	if ins.isOffline() {
		fmt.Printf("  (offline mode — scripts will be staged for first boot via claw)\n")
	}

	// Download all packages first
	type downloadedPkg struct {
		planItem planItem
		dpkPath  string
		manifest *pkg.Manifest
	}
	var downloaded []downloadedPkg

	for _, item := range plan {
		fmt.Printf("  Downloading %s-%s...\n", item.Name, item.Version)
		dpkPath, err := ins.client.DownloadPackageTo(item.Repo, item.Filename, item.Meta, ins.cacheDir())
		if err != nil {
			return fmt.Errorf("download %s: %w", item.Name, err)
		}
		manifest, err := pkg.ReadDpkManifest(dpkPath)
		if err != nil {
			return fmt.Errorf("read manifest %s: %w", item.Name, err)
		}
		downloaded = append(downloaded, downloadedPkg{item, dpkPath, manifest})
	}

	// Transactional install
	var installed []string
	var backups []backup

	rollback := func() {
		for i := len(installed) - 1; i >= 0; i-- {
			pkgName := installed[i]
			files, _ := ins.db.GetFilesForPackage(pkgName)
			for _, f := range files {
				os.Remove(ins.rootPath(f.Path))
				ins.db.RemoveFile(f.Path)
			}
			ins.db.RemovePackage(pkgName)
		}
		for _, b := range backups {
			b.restore()
		}
	}

	for _, d := range downloaded {
		isExplicit := false
		for _, n := range names {
			if n == d.manifest.Name {
				isExplicit = true
				break
			}
		}
		isAuto := auto && !isExplicit

		pkgStagingDir := filepath.Join(ins.stagingDir(), d.manifest.Name)
		if err := os.MkdirAll(pkgStagingDir, 0755); err != nil {
			rollback()
			return fmt.Errorf("create staging dir: %w", err)
		}

		// Extract to staging
		extracted, err := pkg.ExtractDpkPayload(d.dpkPath, pkgStagingDir)
		if err != nil {
			os.RemoveAll(pkgStagingDir)
			rollback()
			return fmt.Errorf("extract %s: %w", d.manifest.Name, err)
		}

		if ins.isOffline() {
			// Offline: stage preinst as a claw firstboot service (runs before
			// postinst on first boot, after claw-rootfs.target).
			if d.manifest.Scripts.PreInst != "" {
				if err := ins.stageFirstBootScript(
					d.manifest.Name, "preinst",
					d.manifest.Scripts.PreInst,
					nil,
				); err != nil {
					os.RemoveAll(pkgStagingDir)
					rollback()
					return fmt.Errorf("stage preinst for %s: %w", d.manifest.Name, err)
				}
				fmt.Printf("  Staged preinst for %s as claw firstboot service\n", d.manifest.Name)
			}
		} else {
			// Online: run preinst immediately
			if d.manifest.Scripts.PreInst != "" {
				if err := runScript(d.manifest.Scripts.PreInst, "preinst", d.manifest.Name); err != nil {
					os.RemoveAll(pkgStagingDir)
					rollback()
					return fmt.Errorf("preinst for %s: %w", d.manifest.Name, err)
				}
			}
		}

		// Move files from staging to final destination (/ or rootDir/)
		var pkgBackups []backup
		var movedFiles []string

		for _, src := range extracted {
			rel, err := filepath.Rel(pkgStagingDir, src)
			if err != nil {
				continue
			}
			// targetPath is the path on the TARGET system (no rootDir prefix).
			targetPath := "/" + filepath.ToSlash(rel)
			// destPath is the actual path on the HOST filesystem.
			destPath := ins.rootPath(targetPath)

			// Backup existing file
			if _, err := os.Stat(destPath); err == nil {
				b, err := backupFile(destPath)
				if err == nil {
					pkgBackups = append(pkgBackups, b)
				}
			}

			if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
				for _, b := range pkgBackups {
					b.restore()
				}
				os.RemoveAll(pkgStagingDir)
				rollback()
				return fmt.Errorf("create parent dir for %s: %w", destPath, err)
			}

			if err := moveFile(src, destPath); err != nil {
				for _, b := range pkgBackups {
					b.restore()
				}
				os.RemoveAll(pkgStagingDir)
				rollback()
				return fmt.Errorf("move file %s: %w", destPath, err)
			}
			// Store the TARGET path in the DB (not rooted), so it reads
			// correctly when dimsim runs on the target system.
			movedFiles = append(movedFiles, targetPath)
		}

		os.RemoveAll(pkgStagingDir)

		if ins.isOffline() {
			// Offline: stage postinst as a claw firstboot service.
			// If preinst was also staged, ensure postinst runs after it.
			if d.manifest.Scripts.PostInst != "" {
				var afterUnits []string
				if d.manifest.Scripts.PreInst != "" {
					afterUnits = append(afterUnits,
						fmt.Sprintf("dimsim-preinst-%s", d.manifest.Name))
				}
				if err := ins.stageFirstBootScript(
					d.manifest.Name, "postinst",
					d.manifest.Scripts.PostInst,
					afterUnits,
				); err != nil {
					for _, f := range movedFiles {
						os.Remove(ins.rootPath(f))
					}
					for _, b := range pkgBackups {
						b.restore()
					}
					rollback()
					return fmt.Errorf("stage postinst for %s: %w", d.manifest.Name, err)
				}
				fmt.Printf("  Staged postinst for %s as claw firstboot service\n", d.manifest.Name)
			}
		} else {
			// Online: run postinst immediately
			if d.manifest.Scripts.PostInst != "" {
				if err := runScript(d.manifest.Scripts.PostInst, "postinst", d.manifest.Name); err != nil {
					for _, f := range movedFiles {
						os.Remove(f)
					}
					for _, b := range pkgBackups {
						b.restore()
					}
					rollback()
					return fmt.Errorf("postinst for %s: %w", d.manifest.Name, err)
				}
			}
		}

		backups = append(backups, pkgBackups...)

		// Record in state DB
		p := &state.Package{
			Name:        d.manifest.Name,
			Version:     d.manifest.Version,
			Arch:        d.manifest.Arch,
			Description: d.manifest.Description,
			Depends:     d.manifest.Depends,
			Provides:    d.manifest.Provides,
			InstalledAt: time.Now(),
			Pinned:      false,
			Auto:        isAuto,
		}
		if err := ins.db.UpsertPackage(p); err != nil {
			rollback()
			return fmt.Errorf("record package %s: %w", d.manifest.Name, err)
		}

		// Record files (using TARGET paths, no rootDir prefix)
		for _, targetPath := range movedFiles {
			h, err := fileHash(ins.rootPath(targetPath))
			if err != nil {
				rollback()
				return fmt.Errorf("hash installed file %s for package %s: %w", targetPath, d.manifest.Name, err)
			}
			if err := ins.db.AddFile(state.FileRecord{
				Path:    targetPath,
				Package: d.manifest.Name,
				Hash:    h,
			}); err != nil {
				rollback()
				return fmt.Errorf("record installed file %s for package %s: %w", targetPath, d.manifest.Name, err)
			}
		}

		installed = append(installed, d.manifest.Name)

		// Update world file for explicit installs
		if !isAuto {
			if err := world.AddToFile(ins.worldFilePath(), d.manifest.Name); err != nil {
				fmt.Printf("  Warning: could not update world file: %v\n", err)
			}
		}

		fmt.Printf("  ✓ Installed %s %s\n", d.manifest.Name, d.manifest.Version)
	}

	return nil
}

// Remove removes the given packages.
func (ins *Installer) Remove(names []string, purge bool) error {
	// Check for reverse dependencies
	for _, name := range names {
		if err := ins.checkReverseDeps(name); err != nil {
			return err
		}
	}

	for _, name := range names {
		p, err := ins.db.GetPackage(name)
		if err != nil {
			return err
		}
		if p == nil {
			fmt.Printf("  Package %s is not installed.\n", name)
			continue
		}
		if p.Pinned {
			return fmt.Errorf("package %s is pinned; unpin it first", name)
		}

		if err := ins.removeOne(p, purge); err != nil {
			return err
		}
	}
	return nil
}

func (ins *Installer) removeOne(p *state.Package, purge bool) error {
	// Try to load the full manifest (including lifecycle scripts) from the
	// cached .dpk. If the cache is gone, fall back to a bare manifest so that
	// file removal still proceeds — we just won't run the scripts.
	dpkPath := filepath.Join(ins.cacheDir(), fmt.Sprintf("%s-%s-%s.dpk", p.Name, p.Version, p.Arch))
	manifest, err := pkg.ReadDpkManifest(dpkPath)
	if err != nil {
		manifest = &pkg.Manifest{Name: p.Name, Version: p.Version}
	}

	// Get installed files (paths are TARGET-relative, no rootDir prefix)
	files, err := ins.db.GetFilesForPackage(p.Name)
	if err != nil {
		return err
	}

	// Run prerm (skipped in offline mode — scripts run on target boot)
	if !ins.isOffline() && manifest.Scripts.PreRm != "" {
		if err := runScript(manifest.Scripts.PreRm, "prerm", p.Name); err != nil {
			fmt.Printf("  Warning: prerm for %s failed: %v\n", p.Name, err)
		}
	}

	// Remove files
	for _, f := range files {
		actualPath := ins.rootPath(f.Path)
		if err := os.Remove(actualPath); err != nil && !os.IsNotExist(err) {
			fmt.Printf("  Warning: could not remove %s: %v\n", f.Path, err)
		}
		ins.db.RemoveFile(f.Path)
	}

	// Run postrm (skipped in offline mode).
	// When purge is requested, pass DIMSIM_PURGE=1 in the environment so that
	// the script can detect the purge operation without unsafe string injection.
	if !ins.isOffline() && manifest.Scripts.PostRm != "" {
		if err := runScriptEnv(manifest.Scripts.PostRm, "postrm", p.Name, purge); err != nil {
			fmt.Printf("  Warning: postrm for %s failed: %v\n", p.Name, err)
		}
	}

	if err := ins.db.RemovePackage(p.Name); err != nil {
		return err
	}

	if err := world.RemoveFromFile(ins.worldFilePath(), p.Name); err != nil {
		fmt.Printf("  Warning: could not update world file: %v\n", err)
	}

	fmt.Printf("  ✓ Removed %s %s\n", p.Name, p.Version)
	return nil
}

// Upgrade upgrades the given packages (or all if empty).
func (ins *Installer) Upgrade(names []string) error {
	if len(names) == 0 {
		pkgs, err := ins.db.ListPackages()
		if err != nil {
			return err
		}
		for _, p := range pkgs {
			names = append(names, p.Name)
		}
	}

	var toUpgrade []string
	for _, name := range names {
		installed, err := ins.db.GetPackage(name)
		if err != nil || installed == nil {
			continue
		}
		if installed.Pinned {
			fmt.Printf("  Skipping %s (pinned)\n", name)
			continue
		}

		available, err := ins.client.FindPackage(name)
		if err != nil {
			continue
		}
		if available.Meta.Custom == nil {
			continue
		}

		a := pkg.ParseSemVer(available.Meta.Custom.Version)
		b := pkg.ParseSemVer(installed.Version)
		if a.Compare(b) > 0 {
			toUpgrade = append(toUpgrade, name)
			fmt.Printf("  %s: %s -> %s\n", name, installed.Version, available.Meta.Custom.Version)
		}
	}

	if len(toUpgrade) == 0 {
		fmt.Println("All packages are up to date.")
		return nil
	}

	return ins.Install(toUpgrade, false)
}

// AutoRemove removes automatically installed packages no longer needed.
func (ins *Installer) AutoRemove() error {
	pkgs, err := ins.db.ListPackages()
	if err != nil {
		return err
	}

	// Build set of needed auto packages (those depended on by non-auto packages)
	needed := make(map[string]bool)
	for _, p := range pkgs {
		if !p.Auto {
			for _, dep := range p.Depends {
				d := pkg.ParseDep(dep)
				needed[d.Name] = true
			}
		}
	}

	var removed int
	for _, p := range pkgs {
		if p.Auto && !needed[p.Name] {
			fmt.Printf("  Removing auto package: %s\n", p.Name)
			if err := ins.removeOne(p, false); err != nil {
				fmt.Printf("  Error removing %s: %v\n", p.Name, err)
			} else {
				removed++
			}
		}
	}

	if removed == 0 {
		fmt.Println("Nothing to autoremove.")
	}
	return nil
}

// Verify verifies installed files against their recorded hashes.
func (ins *Installer) Verify() error {
	pkgs, err := ins.db.ListPackages()
	if err != nil {
		return err
	}

	ok := true
	for _, p := range pkgs {
		files, err := ins.db.GetFilesForPackage(p.Name)
		if err != nil {
			continue
		}
		for _, f := range files {
			actualPath := ins.rootPath(f.Path)
			h, err := fileHash(actualPath)
			if os.IsNotExist(err) {
				fmt.Printf("  MISSING  %s (from %s)\n", f.Path, p.Name)
				ok = false
				continue
			}
			if err != nil {
				fmt.Printf("  ERROR    %s: %v\n", f.Path, err)
				ok = false
				continue
			}
			if h != f.Hash {
				fmt.Printf("  MODIFIED %s (from %s)\n", f.Path, p.Name)
				ok = false
			}
		}
	}
	if ok {
		fmt.Println("✓ All installed files verified successfully.")
	}
	return nil
}

// Doctor checks system health.
func (ins *Installer) Doctor() error {
	issues := 0

	pkgs, err := ins.db.ListPackages()
	if err != nil {
		return err
	}

	pkgMap := make(map[string]*state.Package)
	for _, p := range pkgs {
		pkgMap[p.Name] = p
		for _, prov := range p.Provides {
			pkgMap[prov] = p
		}
	}

	// Check broken deps
	for _, p := range pkgs {
		for _, dep := range p.Depends {
			d := pkg.ParseDep(dep)
			installed, ok := pkgMap[d.Name]
			if !ok {
				fmt.Printf("  ✗ Broken dependency: %s requires %s (not installed)\n", p.Name, dep)
				issues++
				continue
			}
			if !pkg.SatisfiesDep(installed.Version, d) {
				fmt.Printf("  ✗ Version mismatch: %s requires %s, installed %s\n", p.Name, dep, installed.Version)
				issues++
			}
		}
	}

	// Check missing files
	for _, p := range pkgs {
		files, _ := ins.db.GetFilesForPackage(p.Name)
		for _, f := range files {
			if _, err := os.Stat(ins.rootPath(f.Path)); os.IsNotExist(err) {
				fmt.Printf("  ✗ Missing file: %s (from %s)\n", f.Path, p.Name)
				issues++
			}
		}
	}

	// Check TUF metadata expiry
	repos, _ := ins.db.ListRepos()
	for _, r := range repos {
		tsData, _, _ := ins.db.GetTUFMeta(r.Name, "timestamp")
		if tsData == nil {
			fmt.Printf("  ✗ No TUF metadata for repo %s (run 'dimsim update')\n", r.Name)
			issues++
		}
	}

	if issues == 0 {
		fmt.Println("✓ System is healthy.")
	} else {
		fmt.Printf("  Found %d issue(s).\n", issues)
	}
	return nil
}

// --- dependency resolution ---

type planItem struct {
	Name     string
	Version  string
	Repo     string
	Filename string
	Meta     repo.TUFTargetMeta
}

func (ins *Installer) resolveDeps(names []string) ([]planItem, error) {
	resolved := make(map[string]planItem)
	var order []string

	var resolve func(name string) error
	resolve = func(name string) error {
		if _, ok := resolved[name]; ok {
			return nil
		}

		// Already installed?
		installed, err := ins.db.GetPackage(name)
		if err != nil {
			return err
		}
		// If already installed and we're not explicitly asking to install it, skip
		// (only skip if it's not in the explicit list - handled by caller marking auto)
		if installed != nil {
			alreadyExplicit := false
			for _, n := range names {
				if n == name {
					alreadyExplicit = true
					break
				}
			}
			if !alreadyExplicit {
				return nil
			}
		}

		result, err := ins.client.FindPackage(name)
		if err != nil {
			if installed != nil {
				return nil // already installed, dep satisfied
			}
			return fmt.Errorf("cannot find package %s: %w", name, err)
		}

		if result.Meta.Custom == nil {
			return fmt.Errorf("package %s has no metadata", name)
		}

		// Resolve transitive deps first
		for _, dep := range result.Meta.Custom.Depends {
			d := pkg.ParseDep(dep)
			if err := resolve(d.Name); err != nil {
				return err
			}
		}

		resolved[name] = planItem{
			Name:     name,
			Version:  result.Meta.Custom.Version,
			Repo:     result.Repo,
			Filename: result.Filename,
			Meta:     result.Meta,
		}
		order = append(order, name)
		return nil
	}

	for _, name := range names {
		if err := resolve(name); err != nil {
			return nil, err
		}
	}

	var plan []planItem
	for _, name := range order {
		plan = append(plan, resolved[name])
	}
	return plan, nil
}

func (ins *Installer) checkReverseDeps(name string) error {
	pkgs, err := ins.db.ListPackages()
	if err != nil {
		return err
	}
	var dependents []string
	for _, p := range pkgs {
		if p.Name == name {
			continue
		}
		for _, dep := range p.Depends {
			d := pkg.ParseDep(dep)
			if d.Name == name {
				dependents = append(dependents, p.Name)
				break
			}
		}
	}
	if len(dependents) > 0 {
		return fmt.Errorf("cannot remove %s: required by %s", name, strings.Join(dependents, ", "))
	}
	return nil
}

// --- backup helpers ---

type backup struct {
	original string
	backupTo string
}

func backupFile(path string) (backup, error) {
	backupPath := path + ".dimsim-backup"
	if err := copyFile(path, backupPath); err != nil {
		return backup{}, err
	}
	return backup{original: path, backupTo: backupPath}, nil
}

func (b backup) restore() {
	if b.backupTo == "" {
		return
	}
	os.Rename(b.backupTo, b.original)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	stat, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, stat.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Cross-device move
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
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

func runScript(script, name, pkgName string) error {
	return runScriptEnv(script, name, pkgName, false)
}

// runScriptEnv writes script to a temp file and executes it with /bin/bash.
// When purge is true, DIMSIM_PURGE=1 is added to the environment so postrm
// scripts can detect a purge without unsafe string injection.
func runScriptEnv(script, name, pkgName string, purge bool) error {
	tmpFile := filepath.Join(state.StagingDir, fmt.Sprintf("%s-%s.sh", pkgName, name))
	if err := os.MkdirAll(state.StagingDir, 0755); err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}
	if err := os.WriteFile(tmpFile, []byte(script), 0755); err != nil {
		return fmt.Errorf("write script: %w", err)
	}
	defer os.Remove(tmpFile)

	cmd := exec.Command("/bin/bash", tmpFile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if purge {
		cmd.Env = append(cmd.Env, "DIMSIM_PURGE=1")
	}
	return cmd.Run()
}
