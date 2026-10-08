//go:build !windows

package gitengine

import (
	"os/exec"
	"path/filepath"
)

func configureProcess(cmd *exec.Cmd) {}
func samePath(a, b string) bool      { return filepath.Clean(a) == filepath.Clean(b) }
