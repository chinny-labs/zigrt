// Package pack holds the content pack this build embeds (ADR 12) and finds
// a newer one an operator dropped in ZIGRT_CONTENT_DIR (plan 4.7).
package pack

import (
	"bufio"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/chinny-labs/zigrt/content"
)

//go:embed pack.lock *.zip
var files embed.FS

// Lock is pack.lock: which release is embedded and its checksum.
type Lock struct {
	Version string
	File    string
	URL     string
	SHA256  string
}

// ReadLock parses the embedded pack.lock.
func ReadLock() (Lock, error) {
	b, err := files.ReadFile("pack.lock")
	if err != nil {
		return Lock{}, err
	}
	return ParseLock(string(b))
}

// ParseLock parses a lock file: "key value" lines, # comments.
func ParseLock(s string) (Lock, error) {
	var l Lock
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, _ := strings.Cut(line, " ")
		v = strings.TrimSpace(v)
		switch k {
		case "version":
			l.Version = v
		case "file":
			l.File = v
		case "url":
			l.URL = v
		case "sha256":
			l.SHA256 = v
		default:
			return l, fmt.Errorf("pack.lock: unknown key %q", k)
		}
	}
	if l.Version == "" || l.File == "" || len(l.SHA256) != 64 {
		return l, errors.New("pack.lock: version, file and a sha256 are required")
	}
	return l, nil
}

// Embedded loads the embedded pack, after checking it is byte for byte
// the release the lock pins.
func Embedded() (*content.Pack, error) {
	l, err := ReadLock()
	if err != nil {
		return nil, err
	}
	b, err := files.ReadFile(l.File)
	if err != nil {
		return nil, fmt.Errorf("embedded pack: %w", err)
	}
	sum := sha256.Sum256(b)
	if got := hex.EncodeToString(sum[:]); got != l.SHA256 {
		return nil, fmt.Errorf("embedded pack %s: sha256 %s, but pack.lock pins %s", l.File, got, l.SHA256)
	}
	p, err := content.LoadFile(b)
	if err != nil {
		return nil, fmt.Errorf("embedded pack %s: %w", l.File, err)
	}
	if p.Manifest.Version != l.Version {
		return nil, fmt.Errorf("embedded pack says version %s; pack.lock says %s", p.Manifest.Version, l.Version)
	}
	return p, nil
}

// Found is a pack Select chose and where it came from.
type Found struct {
	Pack   *content.Pack
	Source string   // "embedded" or a path in the content folder
	Skip   []string // packs in the folder that would not load, and why
}

// Select returns the pack to run: the newest of the embedded pack and the
// packs in dir (pack files and unpacked folders), when a newer one loads.
// A newer pack this app is too old for is an error, not a fallback: the
// operator meant to run it (plan 4.7), so the app must not look ready.
func Select(dir, appVersion string) (Found, error) {
	emb, err := Embedded()
	if err != nil {
		return Found{}, err
	}
	best := Found{Pack: emb, Source: "embedded"}
	if dir == "" {
		return best, nil
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return best, nil
	}
	if err != nil {
		return best, fmt.Errorf("content folder: %w", err)
	}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if strings.HasPrefix(e.Name(), ".") || (!e.IsDir() && !strings.HasSuffix(e.Name(), ".zip")) {
			continue
		}
		if e.IsDir() {
			if _, err := os.Stat(filepath.Join(path, "manifest.yaml")); err != nil {
				continue
			}
		}
		p, err := content.Open(path)
		if err != nil {
			best.Skip = append(best.Skip, fmt.Sprintf("%s: %v", path, firstLine(err)))
			continue
		}
		if p.Manifest.ID != emb.Manifest.ID {
			best.Skip = append(best.Skip, fmt.Sprintf("%s: it is pack %q, not %q", path, p.Manifest.ID, emb.Manifest.ID))
			continue
		}
		if Newer(p.Manifest.Version, best.Pack.Manifest.Version) {
			best.Pack, best.Source = p, path
		}
	}
	if err := best.Pack.CheckApp(appVersion); err != nil {
		return best, err
	}
	return best, nil
}

func firstLine(err error) string {
	s, _, _ := strings.Cut(err.Error(), "\n")
	return s
}

var calRE = regexp.MustCompile(`^(\d{4})\.(\d{2})\.(\d+)$`)

// Newer reports whether calendar version a (2026.11.1) is after b.
func Newer(a, b string) bool {
	pa, pb := calRE.FindStringSubmatch(a), calRE.FindStringSubmatch(b)
	if pa == nil || pb == nil {
		return false
	}
	na, nb := make([]int, 3), make([]int, 3)
	for i := range 3 {
		na[i], _ = strconv.Atoi(pa[i+1])
		nb[i], _ = strconv.Atoi(pb[i+1])
	}
	return slices.Compare(na, nb) > 0
}
