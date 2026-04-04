package pkg

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// ReadDpk opens a .dpk file and returns the manifest and a map of payload paths to their
// data. The payload map keys are paths relative to the root (e.g. "usr/bin/foo").
func ReadDpk(dpkPath string) (*Manifest, map[string][]byte, error) {
	f, err := os.Open(dpkPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open dpk: %w", err)
	}
	defer f.Close()

	zr, err := zstd.NewReader(f)
	if err != nil {
		return nil, nil, fmt.Errorf("zstd reader: %w", err)
	}
	defer zr.Close()

	tr := tar.NewReader(zr)

	var manifest *Manifest
	payload := make(map[string][]byte)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read tar: %w", err)
		}

		switch {
		case hdr.Name == "meta/manifest.json":
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, nil, fmt.Errorf("read manifest: %w", err)
			}
			manifest, err = ParseManifest(data)
			if err != nil {
				return nil, nil, err
			}

		case strings.HasPrefix(hdr.Name, "payload/"):
			if hdr.Typeflag == tar.TypeDir {
				continue
			}
			rel := strings.TrimPrefix(hdr.Name, "payload/")
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, nil, fmt.Errorf("read payload file %s: %w", rel, err)
			}
			payload[rel] = data
		}
	}

	if manifest == nil {
		return nil, nil, fmt.Errorf("dpk missing meta/manifest.json")
	}

	return manifest, payload, nil
}

// ReadDpkManifest opens a .dpk file and returns only its manifest (faster than ReadDpk).
func ReadDpkManifest(dpkPath string) (*Manifest, error) {
	f, err := os.Open(dpkPath)
	if err != nil {
		return nil, fmt.Errorf("open dpk: %w", err)
	}
	defer f.Close()

	zr, err := zstd.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("zstd reader: %w", err)
	}
	defer zr.Close()

	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}
		if hdr.Name == "meta/manifest.json" {
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("read manifest: %w", err)
			}
			return ParseManifest(data)
		}
	}
	return nil, fmt.Errorf("dpk missing meta/manifest.json")
}

// WriteDpk creates a .dpk archive from a manifest and payload directory.
// payloadDir is the directory whose contents will be packed under payload/.
func WriteDpk(outPath string, manifest *Manifest, payloadDir string) error {
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create dpk: %w", err)
	}
	defer f.Close()

	zw, err := zstd.NewWriter(f)
	if err != nil {
		return fmt.Errorf("zstd writer: %w", err)
	}
	defer zw.Close()

	tw := tar.NewWriter(zw)
	defer tw.Close()

	// Write manifest
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	hdr := &tar.Header{
		Name: "meta/manifest.json",
		Mode: 0644,
		Size: int64(len(manifestData)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("write manifest header: %w", err)
	}
	if _, err := tw.Write(manifestData); err != nil {
		return fmt.Errorf("write manifest data: %w", err)
	}

	// Write payload files
	if err := addDirToTar(tw, payloadDir, "payload"); err != nil {
		return fmt.Errorf("add payload: %w", err)
	}

	return nil
}

func addDirToTar(tw *tar.Writer, srcDir, prefix string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		tarPath := prefix + "/" + filepath.ToSlash(rel)

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("file info header %s: %w", path, err)
		}
		hdr.Name = tarPath

		if info.IsDir() {
			hdr.Name += "/"
			return tw.WriteHeader(hdr)
		}

		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("write header %s: %w", tarPath, err)
		}

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open file %s: %w", path, err)
		}
		defer f.Close()

		if _, err := io.Copy(tw, f); err != nil {
			return fmt.Errorf("copy file %s: %w", path, err)
		}

		return nil
	})
}

// ExtractDpkPayload extracts the payload of a .dpk to destDir.
// Returns a list of extracted file paths (absolute, under destDir).
func ExtractDpkPayload(dpkPath, destDir string) ([]string, error) {
	f, err := os.Open(dpkPath)
	if err != nil {
		return nil, fmt.Errorf("open dpk: %w", err)
	}
	defer f.Close()

	zr, err := zstd.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("zstd reader: %w", err)
	}
	defer zr.Close()

	tr := tar.NewReader(zr)
	var extracted []string

	// Ensure destDir is cleaned and absolute so containment checks are reliable.
	destDir = filepath.Clean(destDir)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}

		if !strings.HasPrefix(hdr.Name, "payload/") {
			continue
		}

		rel := strings.TrimPrefix(hdr.Name, "payload/")
		if rel == "" {
			continue
		}

		// Reject any path that is absolute or contains ".." segments to prevent
		// directory traversal attacks from crafted .dpk archives.
		if filepath.IsAbs(rel) {
			return nil, fmt.Errorf("payload entry has absolute path: %s", hdr.Name)
		}
		cleaned := filepath.Clean(filepath.FromSlash(rel))
		if strings.HasPrefix(cleaned, "..") {
			return nil, fmt.Errorf("payload entry escapes staging directory: %s", hdr.Name)
		}

		dest := filepath.Join(destDir, cleaned)

		// Verify the resolved destination is actually inside destDir using
		// filepath.Rel, which is reliable across platforms regardless of
		// separator differences.
		rel2, err := filepath.Rel(destDir, dest)
		if err != nil || strings.HasPrefix(rel2, "..") {
			return nil, fmt.Errorf("payload entry resolves outside staging directory: %s", hdr.Name)
		}

		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(dest, os.FileMode(hdr.Mode)|0755); err != nil {
				return nil, fmt.Errorf("mkdir %s: %w", dest, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return nil, fmt.Errorf("mkdir parent %s: %w", dest, err)
		}

		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
		if err != nil {
			return nil, fmt.Errorf("create file %s: %w", dest, err)
		}

		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return nil, fmt.Errorf("extract file %s: %w", dest, err)
		}
		out.Close()
		extracted = append(extracted, dest)
	}

	return extracted, nil
}
