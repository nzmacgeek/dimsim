package pkg

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

const (
	FileTypeRegular = "file"
	FileTypeSymlink = "symlink"
)

// HashPath returns the SHA-256 hash for a filesystem entry.
// Symlinks are hashed from their link target so they can be tracked without
// following the referenced path.
func HashPath(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}

	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256([]byte(target))
		return hex.EncodeToString(sum[:]), nil
	}

	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("unsupported payload entry type: %s", path)
	}

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

// BuildManifestFileEntries walks payloadDir and builds manifest entries for its
// files and symlinks.
func BuildManifestFileEntries(payloadDir string) ([]FileEntry, error) {
	var files []FileEntry

	err := filepath.Walk(payloadDir, func(path string, info os.FileInfo, err error) error {
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

		entry, err := manifestFileEntry(path, "/"+filepath.ToSlash(rel), info)
		if err != nil {
			return err
		}
		files = append(files, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files, nil
}

func manifestFileEntry(path, installPath string, info os.FileInfo) (FileEntry, error) {
	hash, err := HashPath(path)
	if err != nil {
		return FileEntry{}, fmt.Errorf("hash %s: %w", path, err)
	}

	entry := FileEntry{
		Path: installPath,
		Hash: hash,
		Mode: fmt.Sprintf("%04o", info.Mode()&0777),
	}

	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return FileEntry{}, fmt.Errorf("read symlink %s: %w", path, err)
		}
		entry.Type = FileTypeSymlink
		entry.Target = target
		entry.Size = int64(len(target))
		return entry, nil
	}

	if !info.Mode().IsRegular() {
		return FileEntry{}, fmt.Errorf("unsupported payload entry type: %s", path)
	}

	entry.Size = info.Size()
	return entry, nil
}
