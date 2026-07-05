package tools

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeFileAtomic writes data to a uniquely-named temp file in the target's own
// directory, then atomically renames it over path. Two guarantees:
//   - Unique temp name (os.CreateTemp) so concurrent writers to the same target
//     never share/clobber one temp file.
//   - The temp is removed on ANY failure (write, chmod, close, rename) so no
//     orphan is ever left — including the disk-full case, where the write itself
//     fails mid-way.
//
// The temp name is dot-prefixed and .tmp-suffixed so an Obsidian vault sync
// ignores it during the sub-millisecond rename window.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmp := f.Name()
	// Any failure below must remove the temp; the rename success path renames it
	// away so the later Remove becomes a harmless no-op.
	defer os.Remove(tmp)

	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("finalize: %w", err)
	}
	return nil
}
