package rename

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func newID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err == nil {
		return hex.EncodeToString(buf)
	}
	// crypto/rand should be available on supported desktop platforms.  The
	// fallback is still unique enough for a temporary filename and avoids
	// making a filesystem transaction fail solely because entropy was delayed.
	return fmt.Sprintf("%d", os.Getpid())
}

func pathExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// pathExistsCaseInsensitive emulates the collision semantics of NTFS even
// when tests run on a case-sensitive Unix filesystem.  It checks the exact
// path first, then enumerates the parent directory for a case-folded match.
func pathExistsCaseInsensitive(path string) (bool, error) {
	exists, err := pathExists(path)
	if err != nil || exists {
		return exists, err
	}
	parent := filepath.Dir(path)
	entries, err := os.ReadDir(parent)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	target := strings.ToLower(filepath.Base(path))
	for _, entry := range entries {
		if strings.ToLower(entry.Name()) == target {
			return true, nil
		}
	}
	return false, nil
}

func findCaseInsensitivePath(path string) (string, bool, error) {
	if exists, err := pathExists(path); err != nil {
		return "", false, err
	} else if exists {
		return path, true, nil
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	target := strings.ToLower(filepath.Base(path))
	for _, entry := range entries {
		if strings.ToLower(entry.Name()) == target {
			return filepath.Join(filepath.Dir(path), entry.Name()), true, nil
		}
	}
	return "", false, nil
}

func combineErrors(primary error, extra []error) error {
	if primary == nil && len(extra) == 0 {
		return nil
	}
	parts := make([]string, 0, len(extra)+1)
	if primary != nil {
		parts = append(parts, primary.Error())
	}
	for _, err := range extra {
		if err != nil {
			parts = append(parts, err.Error())
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(parts, "; "))
}

func ensureDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%q is not a directory", path)
	}
	return nil
}

func removeIfEmpty(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
