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
}

// ParseManifest parses a manifest from JSON bytes.
func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if m.Name == "" {
		return nil, fmt.Errorf("manifest missing required field: name")
	}
	if m.Version == "" {
		return nil, fmt.Errorf("manifest missing required field: version")
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
