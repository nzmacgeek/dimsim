package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nzmacgeek/dimsim/internal/pkg"
)

func TestCopyFilePreservesSymlink(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "target")
	if err := os.WriteFile(target, []byte("hello"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	srcLink := filepath.Join(tmpDir, "source-link")
	if err := os.Symlink("target", srcLink); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	dstLink := filepath.Join(tmpDir, "copied-link")
	if err := copyFile(srcLink, dstLink); err != nil {
		t.Fatalf("copyFile: %v", err)
	}

	info, err := os.Lstat(dstLink)
	if err != nil {
		t.Fatalf("lstat copied link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected copied path to remain a symlink")
	}
	targetPath, err := os.Readlink(dstLink)
	if err != nil {
		t.Fatalf("read copied link: %v", err)
	}
	if targetPath != "target" {
		t.Fatalf("expected link target %q, got %q", "target", targetPath)
	}

	hash, err := pkg.HashPath(dstLink)
	if err != nil {
		t.Fatalf("HashPath: %v", err)
	}
	if hash == "" {
		t.Fatalf("expected non-empty symlink hash")
	}
}
