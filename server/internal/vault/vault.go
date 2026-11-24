// Package vault owns every filesystem access to the notes vault.
//
// All paths are vault-relative and POSIX-separated. Resolution refuses absolute
// paths, traversal, the reserved .lewnotes directory, and symlinks that escape
// the vault. Writes are atomic and serialized per path.
package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"
)

var (
	ErrInvalidPath  = errors.New("invalid vault path")
	ErrEscapesVault = errors.New("path escapes the vault")
	ErrReservedPath = errors.New("path is reserved")
)

// Vault is a directory whose files lewnotes owns.
type Vault struct {
	root    string // absolute, symlink-resolved
	ignores []string
	locks   sync.Map // rel path -> *sync.Mutex
}

// Entry is one directory listing item.
type Entry struct {
	Name    string
	IsDir   bool
	Size    int64
	ModTime int64
}

// Open resolves root, creating it if needed.
func Open(root string, ignores []string) (*Vault, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("vault path: %w", err)
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, fmt.Errorf("create vault %s: %w", abs, err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("resolve vault %s: %w", abs, err)
	}
	return &Vault{root: real, ignores: ignores}, nil
}

// Root is the absolute, resolved vault directory.
func (v *Vault) Root() string { return v.root }

// Resolve turns a vault-relative path into a safe absolute path.
func (v *Vault) Resolve(rel string) (string, error) {
	clean, err := normalizeRel(rel)
	if err != nil {
		return "", err
	}
	full := filepath.Join(v.root, filepath.FromSlash(clean))
	if !within(v.root, full) {
		return "", fmt.Errorf("%w: %q", ErrEscapesVault, rel)
	}
	real, err := resolveExisting(full)
	if err != nil {
		return "", err
	}
	if !within(v.root, real) {
		return "", fmt.Errorf("%w: %q resolves outside the vault", ErrEscapesVault, rel)
	}
	return full, nil
}

func normalizeRel(rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("%w: empty path", ErrInvalidPath)
	}
	if strings.ContainsRune(rel, 0) {
		return "", fmt.Errorf("%w: contains NUL", ErrInvalidPath)
	}
	rel = strings.ReplaceAll(rel, "\\", "/")
	if path.IsAbs(rel) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: absolute paths are not allowed", ErrEscapesVault)
	}
	clean := path.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%w: %q", ErrEscapesVault, rel)
	}
	if clean == ".lewnotes" || strings.HasPrefix(clean, ".lewnotes/") {
		return "", fmt.Errorf("%w: %q", ErrReservedPath, rel)
	}
	return clean, nil
}

func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// resolveExisting evaluates symlinks on the deepest existing ancestor of full
// and re-appends the not-yet-existing tail.
func resolveExisting(full string) (string, error) {
	cur := full
	var rest []string
	for {
		if _, err := os.Lstat(cur); err == nil {
			real, err := filepath.EvalSymlinks(cur)
			if err != nil {
				return "", fmt.Errorf("resolve %s: %w", full, err)
			}
			for i := len(rest) - 1; i >= 0; i-- {
				real = filepath.Join(real, rest[i])
			}
			return real, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("stat %s: %w", cur, err)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return full, nil
		}
		rest = append(rest, filepath.Base(cur))
		cur = parent
	}
}

// Ignored reports whether a relative path should be skipped entirely.
func (v *Vault) Ignored(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if strings.HasPrefix(seg, ".") {
			return true
		}
	}
	for _, pattern := range v.ignores {
		if ok, _ := doublestar.Match(pattern, rel); ok {
			return true
		}
	}
	return false
}

// ignoredDir also prunes directories whose children would match a pattern.
func (v *Vault) ignoredDir(rel string) bool {
	if v.Ignored(rel) {
		return true
	}
	for _, pattern := range v.ignores {
		prefix := literalPrefix(pattern)
		if prefix == "" {
			continue
		}
		if rel == prefix || strings.HasPrefix(prefix, rel+"/") {
			return true
		}
	}
	return false
}

func literalPrefix(pattern string) string {
	if i := strings.IndexAny(pattern, "*?[{"); i >= 0 {
		return strings.TrimSuffix(pattern[:i], "/")
	}
	return pattern
}

// Read returns file bytes plus metadata.
func (v *Vault) Read(rel string) ([]byte, fs.FileInfo, error) {
	full, err := v.Resolve(rel)
	if err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", rel, err)
	}
	info, err := os.Stat(full)
	if err != nil {
		return nil, nil, fmt.Errorf("stat %s: %w", rel, err)
	}
	return data, info, nil
}

// Stat returns metadata for rel.
func (v *Vault) Stat(rel string) (fs.FileInfo, error) {
	full, err := v.Resolve(rel)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", rel, err)
	}
	return info, nil
}

// WriteAtomic writes rel via a temp file, fsync, and rename, serialized against
// other writes to the same path.
func (v *Vault) WriteAtomic(rel string, data []byte, perm fs.FileMode) error {
	full, err := v.Resolve(rel)
	if err != nil {
		return err
	}
	unlock := v.lock(rel)
	defer unlock()

	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create directory for %s: %w", rel, err)
	}

	tmp, err := os.CreateTemp(dir, ".lewnotes-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", rel, err)
	}
	tmpName := tmp.Name()
	closed := false
	defer func() {
		if !closed {
			_ = tmp.Close()
		}
		_ = os.Remove(tmpName)
	}()

	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", rel, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		return fmt.Errorf("chmod %s: %w", rel, err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", rel, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", rel, err)
	}
	closed = true

	if err := os.Rename(tmpName, full); err != nil {
		return fmt.Errorf("rename into %s: %w", rel, err)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// EnsureDir creates a directory inside the vault.
func (v *Vault) EnsureDir(rel string) error {
	full, err := v.Resolve(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(full, 0o750); err != nil {
		return fmt.Errorf("create directory %s: %w", rel, err)
	}
	return nil
}

// List returns the sorted contents of one directory.
func (v *Vault) List(rel string) ([]Entry, error) {
	full, err := v.Resolve(rel)
	if err != nil {
		return nil, err
	}
	items, err := os.ReadDir(full)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", rel, err)
	}
	entries := make([]Entry, 0, len(items))
	for _, item := range items {
		info, err := item.Info()
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", item.Name(), err)
		}
		entries = append(entries, Entry{
			Name:    item.Name(),
			IsDir:   item.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().Unix(),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

// Walk visits every non-ignored regular file with its vault-relative path.
func (v *Vault) Walk(fn func(rel string, info fs.FileInfo) error) error {
	return filepath.WalkDir(v.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == v.root {
			return nil
		}
		rel, err := filepath.Rel(v.root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			if v.ignoredDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if v.Ignored(rel) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return fn(rel, info)
	})
}

func (v *Vault) lock(rel string) func() {
	m, _ := v.locks.LoadOrStore(rel, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}
