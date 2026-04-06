package repo

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nzmacgeek/dimsim/internal/state"
)

// ErrPackageNotFound is returned by FindPackage when no configured repository
// contains the requested package. Callers can use errors.Is to distinguish
// this from operational failures (e.g. database or network errors).
var ErrPackageNotFound = errors.New("package not found")

// Client handles TUF-secured repository interactions.
type Client struct {
	db   *state.DB
	http *http.Client
}

// NewClient creates a new repository client.
func NewClient(db *state.DB) *Client {
	return &Client{
		db:   db,
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// AddRepo adds a new repository by fetching and trusting its root.json.
func (c *Client) AddRepo(name, url string, priority int) error {
	url = strings.TrimRight(url, "/")

	rootData, err := c.fetch(url + "/root.json")
	if err != nil {
		return fmt.Errorf("fetch root.json: %w", err)
	}

	root, _, err := ParseRoot(rootData)
	if err != nil {
		return fmt.Errorf("parse root.json: %w", err)
	}

	if err := c.db.SaveTUFMeta(name, "root", rootData, root.Version); err != nil {
		return fmt.Errorf("save root metadata: %w", err)
	}

	repo := &state.Repo{
		Name:     name,
		URL:      url,
		Enabled:  true,
		Priority: priority,
	}
	if err := c.db.UpsertRepo(repo); err != nil {
		return fmt.Errorf("save repo: %w", err)
	}

	fmt.Printf("✓ Added repository %q (%s)\n", name, url)
	return nil
}

// UpdateRepo refreshes TUF metadata for a single repository.
func (c *Client) UpdateRepo(repoName string) error {
	r, err := c.db.GetRepo(repoName)
	if err != nil {
		return fmt.Errorf("get repo %s: %w", repoName, err)
	}
	if r == nil {
		return fmt.Errorf("unknown repository: %s", repoName)
	}
	if !r.Enabled {
		return nil
	}
	return c.refreshTUF(r)
}

// UpdateAll refreshes TUF metadata for all enabled repositories.
func (c *Client) UpdateAll() error {
	repos, err := c.db.ListRepos()
	if err != nil {
		return err
	}
	var errs []string
	for _, r := range repos {
		if !r.Enabled {
			continue
		}
		fmt.Printf("  Updating %s...\n", r.Name)
		if err := c.refreshTUF(r); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", r.Name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("update errors:\n  %s", strings.Join(errs, "\n  "))
	}
	return nil
}

// refreshTUF implements the TUF update sequence for one repo.
func (c *Client) refreshTUF(r *state.Repo) error {
	// 1. Load trusted root from DB
	rootData, _, err := c.db.GetTUFMeta(r.Name, "root")
	if err != nil || rootData == nil {
		return fmt.Errorf("no trusted root for repo %s", r.Name)
	}
	root, _, err := ParseRoot(rootData)
	if err != nil {
		return fmt.Errorf("parse cached root: %w", err)
	}

	// 2. Download timestamp.json
	tsData, err := c.fetch(r.URL + "/timestamp.json")
	if err != nil {
		return fmt.Errorf("fetch timestamp.json: %w", err)
	}
	ts, err := ParseTimestamp(tsData, root)
	if err != nil {
		return fmt.Errorf("verify timestamp.json: %w", err)
	}
	if err := c.db.SaveTUFMeta(r.Name, "timestamp", tsData, ts.Version); err != nil {
		return err
	}

	// 3. Check if snapshot.json needs update
	snapshotRef, ok := ts.Meta["snapshot.json"]
	if !ok {
		return fmt.Errorf("timestamp missing snapshot.json reference")
	}

	cachedSnap, _, _ := c.db.GetTUFMeta(r.Name, "snapshot")
	needsSnap := true
	if cachedSnap != nil {
		if err := VerifyHash(cachedSnap, snapshotRef.Hashes); err == nil {
			needsSnap = false
		}
	}

	var snap *TUFSnapshotSigned
	if needsSnap {
		snapData, err := c.fetch(r.URL + "/snapshot.json")
		if err != nil {
			return fmt.Errorf("fetch snapshot.json: %w", err)
		}
		if err := VerifyHash(snapData, snapshotRef.Hashes); err != nil {
			return fmt.Errorf("snapshot.json hash mismatch: %w", err)
		}
		snap, err = ParseSnapshot(snapData, root)
		if err != nil {
			return fmt.Errorf("verify snapshot.json: %w", err)
		}
		if err := c.db.SaveTUFMeta(r.Name, "snapshot", snapData, snap.Version); err != nil {
			return err
		}
	} else {
		snap, err = ParseSnapshot(cachedSnap, root)
		if err != nil {
			return fmt.Errorf("parse cached snapshot: %w", err)
		}
	}

	// 4. Check if targets.json needs update
	targetsRef, ok := snap.Meta["targets.json"]
	if !ok {
		return fmt.Errorf("snapshot missing targets.json reference")
	}

	cachedTargets, _, _ := c.db.GetTUFMeta(r.Name, "targets")
	needsTargets := true
	if cachedTargets != nil {
		if err := VerifyHash(cachedTargets, targetsRef.Hashes); err == nil {
			needsTargets = false
		}
	}

	if needsTargets {
		targetsData, err := c.fetch(r.URL + "/targets.json")
		if err != nil {
			return fmt.Errorf("fetch targets.json: %w", err)
		}
		if err := VerifyHash(targetsData, targetsRef.Hashes); err != nil {
			return fmt.Errorf("targets.json hash mismatch: %w", err)
		}
		targets, err := ParseTargets(targetsData, root)
		if err != nil {
			return fmt.Errorf("verify targets.json: %w", err)
		}
		if err := c.db.SaveTUFMeta(r.Name, "targets", targetsData, targets.Version); err != nil {
			return err
		}
	}

	return nil
}

// GetTargets returns the parsed targets for a repo from cache.
func (c *Client) GetTargets(repoName string) (*TUFTargetsSigned, error) {
	rootData, _, err := c.db.GetTUFMeta(repoName, "root")
	if err != nil || rootData == nil {
		return nil, fmt.Errorf("no root metadata for repo %s", repoName)
	}
	root, _, err := ParseRoot(rootData)
	if err != nil {
		return nil, err
	}

	targetsData, _, err := c.db.GetTUFMeta(repoName, "targets")
	if err != nil || targetsData == nil {
		return nil, fmt.Errorf("no targets metadata for repo %s, run 'dimsim update' first", repoName)
	}

	targets, err := ParseTargets(targetsData, root)
	if err != nil {
		return nil, fmt.Errorf("parse targets for %s: %w", repoName, err)
	}
	return targets, nil
}

// SearchPackages returns matching targets across all repos, ordered by priority.
func (c *Client) SearchPackages(query string) ([]SearchResult, error) {
	repos, err := c.db.ListRepos()
	if err != nil {
		return nil, err
	}

	query = strings.ToLower(query)
	seen := make(map[string]bool)
	var results []SearchResult

	for _, r := range repos {
		if !r.Enabled {
			continue
		}
		targets, err := c.GetTargets(r.Name)
		if err != nil {
			continue
		}
		for filename, meta := range targets.Targets {
			if meta.Custom == nil {
				continue
			}
			name := meta.Custom.Name
			if seen[name] {
				continue
			}
			if query == "" ||
				strings.Contains(strings.ToLower(name), query) ||
				strings.Contains(strings.ToLower(meta.Custom.Description), query) {
				seen[name] = true
				results = append(results, SearchResult{
					Repo:     r.Name,
					Filename: filename,
					Meta:     meta,
				})
			}
		}
	}
	return results, nil
}

// SearchResult is a package found in a repository.
type SearchResult struct {
	Repo     string
	Filename string
	Meta     TUFTargetMeta
}

// FindPackage finds a specific package by name across all repos.
func (c *Client) FindPackage(name string) (*SearchResult, error) {
	repos, err := c.db.ListRepos()
	if err != nil {
		return nil, err
	}

	for _, r := range repos {
		if !r.Enabled {
			continue
		}
		targets, err := c.GetTargets(r.Name)
		if err != nil {
			continue
		}
		for filename, meta := range targets.Targets {
			if meta.Custom != nil && meta.Custom.Name == name {
				res := &SearchResult{Repo: r.Name, Filename: filename, Meta: meta}
				return res, nil
			}
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrPackageNotFound, name)
}

// DownloadPackage downloads a package file to the default cache directory and verifies its hash.
func (c *Client) DownloadPackage(repoName, filename string, meta TUFTargetMeta) (string, error) {
	return c.DownloadPackageTo(repoName, filename, meta, state.CacheDir)
}

// DownloadPackageTo downloads a package file to cacheDir and verifies its hash.
// This variant is used for offline installs where the cache lives inside the
// target rootfs rather than /var/lib/dimsim/cache.
func (c *Client) DownloadPackageTo(repoName, filename string, meta TUFTargetMeta, cacheDir string) (string, error) {
	r, err := c.db.GetRepo(repoName)
	if err != nil || r == nil {
		return "", fmt.Errorf("repo not found: %s", repoName)
	}

	destPath := filepath.Join(cacheDir, filename)

	// Check if already cached and valid
	if data, err := os.ReadFile(destPath); err == nil {
		if err := VerifyHash(data, meta.Hashes); err == nil {
			return destPath, nil
		}
	}

	url := r.URL + "/packages/" + filename
	data, err := c.fetch(url)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", filename, err)
	}

	if len(data) != meta.Length {
		return "", fmt.Errorf("size mismatch for %s: got %d, want %d", filename, len(data), meta.Length)
	}

	if err := VerifyHash(data, meta.Hashes); err != nil {
		return "", fmt.Errorf("hash verification failed for %s: %w", filename, err)
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return "", fmt.Errorf("write cached package: %w", err)
	}

	return destPath, nil
}

func (c *Client) fetch(url string) ([]byte, error) {
	resp, err := c.http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response from %s: %w", url, err)
	}
	return data, nil
}
