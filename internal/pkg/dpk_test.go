package pkg

import (
	"archive/tar"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestBuildManifestFileEntriesIncludesSymlinks(t *testing.T) {
	payloadDir := t.TempDir()
	binDir := filepath.Join(payloadDir, "usr", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	target := filepath.Join(binDir, "tool")
	if err := os.WriteFile(target, []byte("hello"), 0755); err != nil {
		t.Fatalf("write file: %v", err)
	}

	linkPath := filepath.Join(binDir, "tool-link")
	if err := os.Symlink("tool", linkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	files, err := BuildManifestFileEntries(payloadDir)
	if err != nil {
		t.Fatalf("BuildManifestFileEntries: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 manifest entries, got %d", len(files))
	}

	var symlinkEntry *FileEntry
	for i := range files {
		if files[i].Path == "/usr/bin/tool-link" {
			symlinkEntry = &files[i]
			break
		}
	}
	if symlinkEntry == nil {
		t.Fatalf("missing symlink entry")
	}
	if symlinkEntry.Type != FileTypeSymlink {
		t.Fatalf("expected symlink type, got %q", symlinkEntry.Type)
	}
	if symlinkEntry.Target != "tool" {
		t.Fatalf("expected symlink target %q, got %q", "tool", symlinkEntry.Target)
	}
	if symlinkEntry.Size != int64(len("tool")) {
		t.Fatalf("expected symlink size %d, got %d", len("tool"), symlinkEntry.Size)
	}

	wantHash, err := HashPath(linkPath)
	if err != nil {
		t.Fatalf("HashPath symlink: %v", err)
	}
	if symlinkEntry.Hash != wantHash {
		t.Fatalf("expected symlink hash %q, got %q", wantHash, symlinkEntry.Hash)
	}
}

func TestWriteAndExtractDpkPreservesSymlinkPayloads(t *testing.T) {
	payloadDir := t.TempDir()
	shareDir := filepath.Join(payloadDir, "usr", "share", "example")
	if err := os.MkdirAll(shareDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	readmePath := filepath.Join(shareDir, "README")
	if err := os.WriteFile(readmePath, []byte("docs"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	linkPath := filepath.Join(shareDir, "README.link")
	if err := os.Symlink("README", linkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	files, err := BuildManifestFileEntries(payloadDir)
	if err != nil {
		t.Fatalf("BuildManifestFileEntries: %v", err)
	}

	manifest := &Manifest{
		Name:        "example",
		Version:     "1.0.0",
		Arch:        "amd64",
		Description: "example package",
		Files:       files,
	}

	dpkPath := filepath.Join(t.TempDir(), manifest.Filename())
	if err := WriteDpk(dpkPath, manifest, payloadDir); err != nil {
		t.Fatalf("WriteDpk: %v", err)
	}

	f, err := os.Open(dpkPath)
	if err != nil {
		t.Fatalf("open dpk: %v", err)
	}
	defer f.Close()

	zr, err := zstd.NewReader(f)
	if err != nil {
		t.Fatalf("zstd reader: %v", err)
	}
	defer zr.Close()

	tr := tar.NewReader(zr)
	foundLinkHeader := false
	for {
		hdr, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("read tar entry: %v", err)
		}
		if hdr.Name == "payload/usr/share/example/README.link" {
			foundLinkHeader = true
			if hdr.Typeflag != tar.TypeSymlink {
				t.Fatalf("expected symlink tar entry, got %v", hdr.Typeflag)
			}
			if hdr.Linkname != "README" {
				t.Fatalf("expected tar link target %q, got %q", "README", hdr.Linkname)
			}
		}
	}
	if !foundLinkHeader {
		t.Fatalf("missing symlink entry in tar archive")
	}

	extractDir := t.TempDir()
	extracted, err := ExtractDpkPayload(dpkPath, extractDir)
	if err != nil {
		t.Fatalf("ExtractDpkPayload: %v", err)
	}
	if len(extracted) != 2 {
		t.Fatalf("expected 2 extracted entries, got %d", len(extracted))
	}

	extractedLink := filepath.Join(extractDir, "usr", "share", "example", "README.link")
	info, err := os.Lstat(extractedLink)
	if err != nil {
		t.Fatalf("lstat extracted symlink: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected extracted path to be a symlink")
	}
	target, err := os.Readlink(extractedLink)
	if err != nil {
		t.Fatalf("read extracted symlink: %v", err)
	}
	if target != "README" {
		t.Fatalf("expected extracted symlink target %q, got %q", "README", target)
	}

	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	parsed, err := ParseManifest(manifestData)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if len(parsed.Files) != len(manifest.Files) {
		t.Fatalf("expected %d manifest entries after roundtrip, got %d", len(manifest.Files), len(parsed.Files))
	}
}
