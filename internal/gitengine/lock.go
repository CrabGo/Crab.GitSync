package gitengine

import (
	"os"
	"path/filepath"
	"strings"
)

// CommonDirectory resolves shared Git metadata without invoking Git or changing a repository.
// Linked worktrees must share a lease when fetching or updating references.
func CommonDirectory(path string) string {
	dir := filepath.Join(path, ".git")
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return path
	} // Bare repository, or a stale candidate.
	if err != nil {
		return dir
	}
	if !info.IsDir() {
		data, err := os.ReadFile(dir)
		if err != nil {
			return dir
		}
		value := strings.TrimSpace(string(data))
		if !strings.HasPrefix(value, "gitdir:") {
			return dir
		}
		dir = strings.TrimSpace(strings.TrimPrefix(value, "gitdir:"))
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(path, dir)
		}
	}
	if data, err := os.ReadFile(filepath.Join(dir, "commondir")); err == nil {
		common := strings.TrimSpace(string(data))
		if common != "" {
			if filepath.IsAbs(common) {
				return common
			}
			return filepath.Join(dir, common)
		}
	}
	return dir
}
