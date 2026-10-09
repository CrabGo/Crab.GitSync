package gitengine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"crab.gitsync/internal/scansettings"
)

// DiscoverMany keeps progress increasing across roots and deduplicates canonical repository paths.
func DiscoverMany(ctx context.Context, roots, excludes []string, visit func(int, string), warn func(string)) ([]string, error) {
	paths := []string{}
	seen := map[string]bool{}
	visited := 0
	for _, root := range roots {
		if ctx.Err() != nil {
			return paths, ctx.Err()
		}
		actual, err := filepath.EvalSymlinks(root)
		if err != nil {
			warn(fmt.Sprintf("无法读取扫描根目录 %s：%v", root, err))
			continue
		}
		abs, err := filepath.Abs(actual)
		if err != nil {
			warn(err.Error())
			continue
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			warn(fmt.Sprintf("扫描根目录不是可访问目录：%s", root))
			continue
		}
		base := visited
		found, err := discover(ctx, abs, excludes, func(n int, path string) { visited = base + n; visit(visited, path) }, warn)
		if ctx.Err() != nil {
			return paths, ctx.Err()
		}
		if err != nil {
			warn(fmt.Sprintf("无法扫描 %s：%v", root, err))
			continue
		}
		for _, path := range found {
			key := scansettings.PathKey(path)
			if !seen[key] {
				paths = append(paths, path)
				seen[key] = true
			}
		}
	}
	return paths, nil
}

func excluded(root, path string, excludes []string) bool {
	for _, exclude := range excludes {
		if !filepath.IsAbs(exclude) {
			exclude = filepath.Join(root, exclude)
		} else if actual, err := filepath.EvalSymlinks(exclude); err == nil {
			exclude = actual
		}
		rel, err := filepath.Rel(scansettings.PathKey(exclude), scansettings.PathKey(path))
		if err == nil && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
