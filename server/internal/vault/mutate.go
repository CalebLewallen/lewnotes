package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Rename moves a file within the vault. It refuses to overwrite an existing
// destination so a moved note can never silently replace another one.
func (v *Vault) Rename(oldRel, newRel string) error {
	oldFull, err := v.Resolve(oldRel)
	if err != nil {
		return err
	}
	newFull, err := v.Resolve(newRel)
	if err != nil {
		return err
	}
	if oldFull == newFull {
		return nil
	}
	if _, err := os.Lstat(newFull); err == nil {
		return fmt.Errorf("%w: %s", fs.ErrExist, newRel)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", newRel, err)
	}

	// Renames can race each other in both directions; lock in a stable order.
	first, second := oldFull, newFull
	if second < first {
		first, second = second, first
	}
	unlockFirst := v.lock(first)
	defer unlockFirst()
	unlockSecond := v.lock(second)
	defer unlockSecond()

	if err := os.MkdirAll(filepath.Dir(newFull), 0o750); err != nil {
		return fmt.Errorf("create directory for %s: %w", newRel, err)
	}
	if err := os.Rename(oldFull, newFull); err != nil {
		return fmt.Errorf("rename %s to %s: %w", oldRel, newRel, err)
	}
	syncDir(filepath.Dir(oldFull))
	syncDir(filepath.Dir(newFull))
	return nil
}

// MoveToTrash moves a file into .lewnotes/trash with a timestamped name and
// returns the vault-relative location of the trashed file. The trash is
// server-owned, so callers must not try to Resolve the returned path.
func (v *Vault) MoveToTrash(rel string) (string, error) {
	oldFull, err := v.Resolve(rel)
	if err != nil {
		return "", err
	}
	unlock := v.lock(oldFull)
	defer unlock()

	trashDir := filepath.Join(v.root, ".lewnotes", "trash")
	if err := os.MkdirAll(trashDir, 0o750); err != nil {
		return "", fmt.Errorf("create trash: %w", err)
	}

	stamp := time.Now().UTC().Format("20060102-150405")
	base := stamp + "-" + filepath.Base(oldFull)
	dest := filepath.Join(trashDir, base)
	for i := 1; ; i++ {
		if _, err := os.Lstat(dest); errors.Is(err, fs.ErrNotExist) {
			break
		} else if err != nil {
			return "", fmt.Errorf("stat trash candidate: %w", err)
		}
		dest = filepath.Join(trashDir, fmt.Sprintf("%s-%d", base, i))
	}

	if err := os.Rename(oldFull, dest); err != nil {
		return "", fmt.Errorf("trash %s: %w", rel, err)
	}
	syncDir(filepath.Dir(oldFull))
	syncDir(trashDir)

	relDest, err := filepath.Rel(v.root, dest)
	if err != nil {
		return "", fmt.Errorf("describe trash location: %w", err)
	}
	return filepath.ToSlash(relDest), nil
}

// IsNotePath reports whether rel names a markdown file.
func IsNotePath(rel string) bool {
	return strings.EqualFold(path.Ext(rel), ".md")
}

func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}
