// Package gitengine discovers local repositories and reads their Git metadata.
package gitengine

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Remote struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	GitHub bool   `json:"github"`
}

type Repository struct {
	Path        string   `json:"path"`
	Name        string   `json:"name"`
	Branch      string   `json:"branch"`
	Upstream    string   `json:"upstream"`
	Remotes     []Remote `json:"remotes"`
	Changed     int      `json:"changed"`
	Ahead       int      `json:"ahead"`
	Behind      int      `json:"behind"`
	Bare        bool     `json:"bare"`
	Detached    bool     `json:"detached"`
	LastCommit  string   `json:"lastCommit"`
	FetchStatus string   `json:"fetchStatus"`
	Error       string   `json:"error"`
}

// Run invokes Git without a shell, with a deadline and interactive prompts disabled.
func Run(ctx context.Context, path string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", path}, args...)...)
	configureProcess(cmd)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "GIT_SSH_COMMAND=ssh -o BatchMode=yes -o ConnectTimeout=15", "GIT_OPTIONAL_LOCKS=0")
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", fmt.Errorf("git %s: %s", args[0], Redact(strings.TrimSpace(string(out))))
	}
	return strings.TrimSpace(string(out)), nil
}

var urlPattern = regexp.MustCompile(`https?://[^\s]+`)

// Redact removes credentials in HTTP remote URLs before displaying them.
func Redact(value string) string {
	return urlPattern.ReplaceAllStringFunc(value, func(s string) string {
		u, err := url.Parse(s)
		if err != nil {
			return "[远端地址已隐藏]"
		}
		u.User = nil
		u.RawQuery = ""
		u.Fragment = ""
		return u.String()
	})
}

func isGitHub(value string) bool {
	if strings.HasPrefix(value, "git@github.com:") {
		return true
	}
	u, err := url.Parse(value)
	return err == nil && strings.EqualFold(u.Hostname(), "github.com")
}

// Discover walks subdirectories, including nested repos and worktrees. It never follows symlinks.
// The visit callback receives an increasing directory count while total work is still unknown.
func Discover(ctx context.Context, root string, visit func(int, string), warn func(string)) ([]string, error) {
	paths := []string{}
	visited := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			if path == root {
				return walkErr
			}
			warn(fmt.Sprintf("无法读取 %s：%v", path, walkErr))
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		if path != root {
			switch entry.Name() {
			case ".git", "node_modules", ".venv":
				return filepath.SkipDir
			}
		}
		visited++
		visit(visited, path)
		if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
			paths = append(paths, path)
			return nil
		}
		// Bare repos have HEAD, objects and refs directly at their root.
		if _, err := os.Stat(filepath.Join(path, "HEAD")); err == nil {
			objects, e1 := os.Stat(filepath.Join(path, "objects"))
			refs, e2 := os.Stat(filepath.Join(path, "refs"))
			if e1 == nil && e2 == nil && objects.IsDir() && refs.IsDir() {
				paths = append(paths, path)
				return filepath.SkipDir
			}
		}
		return nil
	})
	return paths, err
}

// Inspect validates a candidate and reads local status without accessing the network.
func Inspect(ctx context.Context, path string) (Repository, error) {
	r := Repository{Path: path, Name: filepath.Base(path), Remotes: []Remote{}, FetchStatus: "idle"}
	run := func(args ...string) (string, error) { return Run(ctx, path, 15*time.Second, args...) }
	bare, err := run("rev-parse", "--is-bare-repository")
	if err != nil {
		return r, err
	}
	r.Bare = bare == "true"
	if !r.Bare {
		top, err := run("rev-parse", "--show-toplevel")
		if err != nil {
			return r, err
		}
		actual, e1 := filepath.EvalSymlinks(top)
		expected, e2 := filepath.EvalSymlinks(path)
		if e1 != nil || e2 != nil || !samePath(actual, expected) {
			return r, fmt.Errorf("不是有效的仓库根目录")
		}
	}
	r.Branch, err = run("symbolic-ref", "--short", "HEAD")
	if err != nil {
		r.Detached = true
		r.Branch, err = run("rev-parse", "--short", "HEAD")
		if err != nil {
			return r, err
		}
	}
	if !r.Bare {
		status, err := run("status", "--porcelain=v1", "--untracked-files=normal")
		if err != nil {
			return r, err
		}
		if status != "" {
			r.Changed = len(strings.Split(status, "\n"))
		}
	}
	remoteNames, err := run("remote")
	if err != nil {
		return r, err
	}
	for _, name := range strings.Fields(remoteNames) {
		remoteURL, err := run("remote", "get-url", name)
		if err != nil {
			return r, err
		}
		r.Remotes = append(r.Remotes, Remote{Name: name, URL: Redact(remoteURL), GitHub: isGitHub(remoteURL)})
	}
	if !r.Detached {
		tracking, err := run("for-each-ref", "--format=%(upstream:short)", "refs/heads/"+r.Branch)
		if err != nil {
			return r, err
		}
		r.Upstream = tracking
		if r.Upstream != "" {
			counts, err := run("rev-list", "--left-right", "--count", "HEAD...@{upstream}")
			if err == nil {
				parts := strings.Fields(counts)
				if len(parts) == 2 {
					r.Ahead, _ = strconv.Atoi(parts[0])
					r.Behind, _ = strconv.Atoi(parts[1])
				}
			} else {
				r.Error = "无法比较跟踪分支：" + err.Error()
			}
		}
	}
	// An empty repository has no commit yet; this is a valid state.
	r.LastCommit, _ = run("log", "-1", "--format=%h %s")
	if ctx.Err() != nil {
		return r, ctx.Err()
	}
	return r, nil
}

// Fetch downloads every configured remote without merging or changing working files.
func Fetch(ctx context.Context, path string) error {
	_, err := Run(ctx, path, 2*time.Minute, "fetch", "--all", "--no-recurse-submodules")
	return err
}
