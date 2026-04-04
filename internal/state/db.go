package state

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const StateDir = "/var/lib/dimsim"
const DBPath = "/var/lib/dimsim/state.db"
const CacheDir = "/var/lib/dimsim/cache"
const StagingDir = "/var/lib/dimsim/staging"
const WorldFile = "/var/lib/dimsim/world"

// DB wraps a SQLite database for dimsim state.
type DB struct {
	sql *sql.DB
}

// Package represents an installed package record.
type Package struct {
	Name        string
	Version     string
	Arch        string
	Description string
	Depends     []string
	Provides    []string
	InstalledAt time.Time
	Pinned      bool
	Auto        bool
}

// Repo represents a configured repository.
type Repo struct {
	Name     string
	URL      string
	Enabled  bool
	Priority int
}

// FileRecord represents a tracked installed file.
type FileRecord struct {
	Path    string
	Package string
	Hash    string
}

// Open opens (or creates) the state database, creating required directories.
func Open() (*DB, error) {
	for _, dir := range []string{StateDir, CacheDir, StagingDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create state dir %s: %w", dir, err)
		}
	}

	db, err := sql.Open("sqlite", DBPath)
	if err != nil {
		return nil, fmt.Errorf("open state db: %w", err)
	}

	d := &DB{sql: db}
	if err := d.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate state db: %w", err)
	}
	return d, nil
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	return d.sql.Close()
}

// SQL returns the underlying *sql.DB for direct queries if needed.
func (d *DB) SQL() *sql.DB {
	return d.sql
}

func (d *DB) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS packages (
			name        TEXT PRIMARY KEY,
			version     TEXT NOT NULL,
			arch        TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			depends     TEXT NOT NULL DEFAULT '',
			provides    TEXT NOT NULL DEFAULT '',
			installed_at TEXT NOT NULL DEFAULT '',
			pinned      INTEGER NOT NULL DEFAULT 0,
			auto        INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS repos (
			name     TEXT PRIMARY KEY,
			url      TEXT NOT NULL,
			enabled  INTEGER NOT NULL DEFAULT 1,
			priority INTEGER NOT NULL DEFAULT 100
		)`,
		`CREATE TABLE IF NOT EXISTS files (
			path    TEXT PRIMARY KEY,
			package TEXT NOT NULL,
			hash    TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tuf_meta (
			repo    TEXT NOT NULL,
			role    TEXT NOT NULL,
			content BLOB NOT NULL,
			version INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY(repo, role)
		)`,
	}
	for _, stmt := range stmts {
		if _, err := d.sql.Exec(stmt); err != nil {
			return fmt.Errorf("exec migration: %w\nSQL: %s", err, stmt)
		}
	}
	return nil
}

// --- Package operations ---

// GetPackage returns the installed package with the given name, or nil if not found.
func (d *DB) GetPackage(name string) (*Package, error) {
	row := d.sql.QueryRow(
		`SELECT name, version, arch, description, depends, provides, installed_at, pinned, auto FROM packages WHERE name = ?`,
		name,
	)
	return scanPackage(row)
}

// ListPackages returns all installed packages.
func (d *DB) ListPackages() ([]*Package, error) {
	rows, err := d.sql.Query(
		`SELECT name, version, arch, description, depends, provides, installed_at, pinned, auto FROM packages ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list packages: %w", err)
	}
	defer rows.Close()
	return scanPackages(rows)
}

// UpsertPackage inserts or updates a package record.
func (d *DB) UpsertPackage(p *Package) error {
	_, err := d.sql.Exec(
		`INSERT INTO packages (name, version, arch, description, depends, provides, installed_at, pinned, auto)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
		   version=excluded.version, arch=excluded.arch, description=excluded.description,
		   depends=excluded.depends, provides=excluded.provides, installed_at=excluded.installed_at,
		   pinned=excluded.pinned, auto=excluded.auto`,
		p.Name, p.Version, p.Arch, p.Description,
		strings.Join(p.Depends, ","),
		strings.Join(p.Provides, ","),
		p.InstalledAt.UTC().Format(time.RFC3339),
		boolToInt(p.Pinned),
		boolToInt(p.Auto),
	)
	if err != nil {
		return fmt.Errorf("upsert package %s: %w", p.Name, err)
	}
	return nil
}

// RemovePackage deletes the package record.
func (d *DB) RemovePackage(name string) error {
	_, err := d.sql.Exec(`DELETE FROM packages WHERE name = ?`, name)
	return err
}

// SetPinned sets the pinned flag on a package.
func (d *DB) SetPinned(name string, pinned bool) error {
	res, err := d.sql.Exec(`UPDATE packages SET pinned = ? WHERE name = ?`, boolToInt(pinned), name)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("package not installed: %s", name)
	}
	return nil
}

// --- Repo operations ---

// GetRepo returns the repo with the given name, or nil if not found.
func (d *DB) GetRepo(name string) (*Repo, error) {
	row := d.sql.QueryRow(`SELECT name, url, enabled, priority FROM repos WHERE name = ?`, name)
	return scanRepo(row)
}

// ListRepos returns all configured repos ordered by priority.
func (d *DB) ListRepos() ([]*Repo, error) {
	rows, err := d.sql.Query(`SELECT name, url, enabled, priority FROM repos ORDER BY priority DESC, name`)
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	defer rows.Close()
	var repos []*Repo
	for rows.Next() {
		r, err := scanRepoRow(rows)
		if err != nil {
			return nil, err
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

// UpsertRepo inserts or updates a repo record.
func (d *DB) UpsertRepo(r *Repo) error {
	_, err := d.sql.Exec(
		`INSERT INTO repos (name, url, enabled, priority) VALUES (?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET url=excluded.url, enabled=excluded.enabled, priority=excluded.priority`,
		r.Name, r.URL, boolToInt(r.Enabled), r.Priority,
	)
	return err
}

// RemoveRepo removes a repo and its TUF metadata.
func (d *DB) RemoveRepo(name string) error {
	_, err := d.sql.Exec(`DELETE FROM repos WHERE name = ?`, name)
	if err != nil {
		return err
	}
	_, err = d.sql.Exec(`DELETE FROM tuf_meta WHERE repo = ?`, name)
	return err
}

// --- File operations ---

// AddFile records a file as belonging to a package.
func (d *DB) AddFile(rec FileRecord) error {
	_, err := d.sql.Exec(
		`INSERT INTO files (path, package, hash) VALUES (?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET package=excluded.package, hash=excluded.hash`,
		rec.Path, rec.Package, rec.Hash,
	)
	return err
}

// RemoveFile removes a file record.
func (d *DB) RemoveFile(path string) error {
	_, err := d.sql.Exec(`DELETE FROM files WHERE path = ?`, path)
	return err
}

// GetFilesForPackage returns all file records for a given package.
func (d *DB) GetFilesForPackage(pkgName string) ([]FileRecord, error) {
	rows, err := d.sql.Query(`SELECT path, package, hash FROM files WHERE package = ? ORDER BY path`, pkgName)
	if err != nil {
		return nil, fmt.Errorf("get files for %s: %w", pkgName, err)
	}
	defer rows.Close()
	var records []FileRecord
	for rows.Next() {
		var r FileRecord
		if err := rows.Scan(&r.Path, &r.Package, &r.Hash); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// GetFile returns the file record for a given path, or nil if not found.
func (d *DB) GetFile(path string) (*FileRecord, error) {
	row := d.sql.QueryRow(`SELECT path, package, hash FROM files WHERE path = ?`, path)
	var r FileRecord
	err := row.Scan(&r.Path, &r.Package, &r.Hash)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// --- TUF metadata operations ---

// SaveTUFMeta saves TUF metadata content for a repo and role.
func (d *DB) SaveTUFMeta(repo, role string, content []byte, version int) error {
	_, err := d.sql.Exec(
		`INSERT INTO tuf_meta (repo, role, content, version) VALUES (?, ?, ?, ?)
		 ON CONFLICT(repo, role) DO UPDATE SET content=excluded.content, version=excluded.version`,
		repo, role, content, version,
	)
	return err
}

// GetTUFMeta returns cached TUF metadata for a repo and role.
func (d *DB) GetTUFMeta(repo, role string) (content []byte, version int, err error) {
	row := d.sql.QueryRow(`SELECT content, version FROM tuf_meta WHERE repo = ? AND role = ?`, repo, role)
	err = row.Scan(&content, &version)
	if err == sql.ErrNoRows {
		return nil, 0, nil
	}
	return content, version, err
}

// --- helpers ---

func scanPackage(row *sql.Row) (*Package, error) {
	var p Package
	var dependsStr, providesStr, installedAt string
	var pinned, auto int
	err := row.Scan(&p.Name, &p.Version, &p.Arch, &p.Description, &dependsStr, &providesStr, &installedAt, &pinned, &auto)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Depends = splitComma(dependsStr)
	p.Provides = splitComma(providesStr)
	p.Pinned = pinned != 0
	p.Auto = auto != 0
	p.InstalledAt, _ = time.Parse(time.RFC3339, installedAt)
	return &p, nil
}

func scanPackages(rows *sql.Rows) ([]*Package, error) {
	var pkgs []*Package
	for rows.Next() {
		var p Package
		var dependsStr, providesStr, installedAt string
		var pinned, auto int
		if err := rows.Scan(&p.Name, &p.Version, &p.Arch, &p.Description, &dependsStr, &providesStr, &installedAt, &pinned, &auto); err != nil {
			return nil, err
		}
		p.Depends = splitComma(dependsStr)
		p.Provides = splitComma(providesStr)
		p.Pinned = pinned != 0
		p.Auto = auto != 0
		p.InstalledAt, _ = time.Parse(time.RFC3339, installedAt)
		pkgs = append(pkgs, &p)
	}
	return pkgs, rows.Err()
}

func scanRepo(row *sql.Row) (*Repo, error) {
	var r Repo
	var enabled int
	err := row.Scan(&r.Name, &r.URL, &enabled, &r.Priority)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.Enabled = enabled != 0
	return &r, nil
}

func scanRepoRow(rows *sql.Rows) (*Repo, error) {
	var r Repo
	var enabled int
	if err := rows.Scan(&r.Name, &r.URL, &enabled, &r.Priority); err != nil {
		return nil, err
	}
	r.Enabled = enabled != 0
	return &r, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func splitComma(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
