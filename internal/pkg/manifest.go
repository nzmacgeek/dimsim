package pkg

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// FileEntry represents a file entry in the manifest.
type FileEntry struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
	Size int64  `json:"size"`
	Mode string `json:"mode"`
}

// Scripts holds optional install/remove lifecycle scripts.
type Scripts struct {
	PreInst  string `json:"preinst,omitempty"`
	PostInst string `json:"postinst,omitempty"`
	PreRm    string `json:"prerm,omitempty"`
	PostRm   string `json:"postrm,omitempty"`
}

// Manifest is the parsed contents of meta/manifest.json inside a .dpk.
type Manifest struct {
	Name        string      `json:"name"`
	Version     string      `json:"version"`
	Arch        string      `json:"arch"`
	Description string      `json:"description"`
	Depends     []string    `json:"depends,omitempty"`
	Recommends  []string    `json:"recommends,omitempty"`
	Conflicts   []string    `json:"conflicts,omitempty"`
	Provides    []string    `json:"provides,omitempty"`
	Maintainer  string      `json:"maintainer,omitempty"`
	Homepage    string      `json:"homepage,omitempty"`
	Files       []FileEntry `json:"files,omitempty"`
	Scripts     Scripts     `json:"scripts,omitempty"`
	// Core marks a package as a foundational system package. When true,
	// offline installs into a sysroot bypass the /bin/bash presence check,
	// allowing packages like bash itself to be installed into a fresh root.
	// Lifecycle scripts for core packages are skipped when bash is absent.
	Core        bool        `json:"core,omitempty"`
}

// identifierAlwaysAllowed is the set of non-alphanumeric characters that are
// always permitted in manifest identifier fields (name, version, arch).
const identifierAlwaysAllowed = "._+-"

// validateManifestIdentifier checks that a manifest field value is safe to use
// as a filesystem path component. It must be non-empty, must not be "." or "..",
// must not contain path separators, and may only contain alphanumeric characters
// plus identifierAlwaysAllowed and extraAllowed.
func validateManifestIdentifier(field, value, extraAllowed string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("manifest missing required field: %s", field)
	}
	if value == "." || value == ".." {
		return "", fmt.Errorf("manifest field %q must not be %q", field, value)
	}
	if strings.ContainsAny(value, `/\`) {
		return "", fmt.Errorf("manifest field %q contains path separator", field)
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case strings.ContainsRune(identifierAlwaysAllowed, r):
		case extraAllowed != "" && strings.ContainsRune(extraAllowed, r):
		default:
			return "", fmt.Errorf("manifest field %q contains invalid character %q", field, r)
		}
	}
	return value, nil
}

// ParseManifest parses a manifest from JSON bytes.
func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	var err error
	if m.Name, err = validateManifestIdentifier("name", m.Name, ""); err != nil {
		return nil, err
	}
	// Version fields allow "~" and ":" which are common in Debian-style versions.
	if m.Version, err = validateManifestIdentifier("version", m.Version, "~:"); err != nil {
		return nil, err
	}
	// Arch is optional; validate if present.
	if m.Arch != "" {
		if m.Arch, err = validateManifestIdentifier("arch", m.Arch, ""); err != nil {
			return nil, err
		}
	}
	return &m, nil
}

// MarshalJSON returns the manifest as formatted JSON bytes.
func (m *Manifest) MarshalJSON() ([]byte, error) {
	type Alias Manifest
	return json.MarshalIndent((*Alias)(m), "", "  ")
}

// Filename returns the canonical .dpk filename for this package.
func (m *Manifest) Filename() string {
	return fmt.Sprintf("%s-%s-%s.dpk", m.Name, m.Version, m.Arch)
}

// Dep represents a parsed dependency specification.
type Dep struct {
	Name    string
	Op      string // "", ">=", "=", "<=", ">", "<"
	Version string
}

// ParseDep parses a dependency string like "pkg>=1.0.0" or "pkg".
func ParseDep(s string) Dep {
	for _, op := range []string{">=", "<=", "!=", "=", ">", "<"} {
		if idx := strings.Index(s, op); idx >= 0 {
			return Dep{
				Name:    strings.TrimSpace(s[:idx]),
				Op:      op,
				Version: strings.TrimSpace(s[idx+len(op):]),
			}
		}
	}
	return Dep{Name: strings.TrimSpace(s)}
}

// SemVer is a simple version representation.
type SemVer struct {
	Major, Minor, Patch int
	Pre                 string
	Raw                 string
}

// ParseSemVer parses a version string into a SemVer.
func ParseSemVer(v string) SemVer {
	s := SemVer{Raw: v}
	// strip leading 'v'
	v = strings.TrimPrefix(v, "v")
	// split pre-release
	if idx := strings.IndexAny(v, "-+~"); idx >= 0 {
		s.Pre = v[idx:]
		v = v[:idx]
	}
	parts := strings.SplitN(v, ".", 3)
	if len(parts) >= 1 {
		s.Major, _ = strconv.Atoi(parts[0])
	}
	if len(parts) >= 2 {
		s.Minor, _ = strconv.Atoi(parts[1])
	}
	if len(parts) >= 3 {
		s.Patch, _ = strconv.Atoi(parts[2])
	}
	return s
}

// Compare returns -1, 0, or 1.
func (a SemVer) Compare(b SemVer) int {
	if a.Major != b.Major {
		return cmpInt(a.Major, b.Major)
	}
	if a.Minor != b.Minor {
		return cmpInt(a.Minor, b.Minor)
	}
	if a.Patch != b.Patch {
		return cmpInt(a.Patch, b.Patch)
	}
	// Both have no pre-release or same pre-release
	if a.Pre == b.Pre {
		return 0
	}
	// no pre-release > pre-release
	if a.Pre == "" {
		return 1
	}
	if b.Pre == "" {
		return -1
	}
	if a.Pre < b.Pre {
		return -1
	}
	return 1
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// SatisfiesDep returns true if version v satisfies dependency dep.
func SatisfiesDep(v string, dep Dep) bool {
	if dep.Op == "" || dep.Version == "" {
		return true
	}
	a := ParseSemVer(v)
	b := ParseSemVer(dep.Version)
	cmp := a.Compare(b)
	switch dep.Op {
	case ">=":
		return cmp >= 0
	case "<=":
		return cmp <= 0
	case "=":
		return cmp == 0
	case "!=":
		return cmp != 0
	case ">":
		return cmp > 0
	case "<":
		return cmp < 0
	}
	return false
}
