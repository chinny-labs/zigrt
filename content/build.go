// SPDX-License-Identifier: Apache-2.0

package content

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"
)

// packFiles reports whether a file belongs in a pack file.
func packFiles(name string, d fs.DirEntry) bool {
	base := d.Name()
	if strings.HasPrefix(base, ".") && name != "." {
		return false
	}
	switch {
	case d.IsDir():
		return name == "." || name == "topics" || name == "figures"
	case strings.Contains(name, "/"):
		return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".svg")
	}
	return strings.HasSuffix(name, ".yaml") || base == "released-ids.json" || strings.HasPrefix(base, "LICENSE") || base == "README.md"
}

// Build writes a pack file: a zip archive of the pack, byte-for-byte the same
// for the same input, so its checksum can be pinned. It returns the SHA-256
// of what it wrote.
func Build(fsys fs.FS, w io.Writer) (string, error) {
	h := sha256.New()
	zw := zip.NewWriter(io.MultiWriter(w, h))
	epoch := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !packFiles(name, d) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		f, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: epoch})
		if err != nil {
			return err
		}
		_, err = f.Write(b)
		return err
	})
	if err != nil {
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Open loads a pack from a folder or a pack file.
func Open(name string) (*Pack, error) {
	st, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return Load(os.DirFS(name))
	}
	b, err := os.ReadFile(name) //nolint:gosec // the operator or author names the pack file
	if err != nil {
		return nil, err
	}
	return LoadFile(b)
}

// LoadFile loads a pack file held in memory, such as an upload.
func LoadFile(b []byte) (*Pack, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, fmt.Errorf("not a pack file: %w", err)
	}
	return Load(zr)
}
