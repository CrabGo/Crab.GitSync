package gitengine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Branches returns full ref names so local and remote branches cannot be confused.
func Branches(ctx context.Context, path string) ([]string, error) {
	out, err := Run(ctx, path, 15*time.Second, "for-each-ref", "--format=%(refname)", "refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	refs := []string{}
	for _, ref := range strings.Split(out, "\n") {
		if ref != "" && !strings.HasSuffix(ref, "/HEAD") {
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

func operationInProgress(ctx context.Context, path string) (bool, error) {
	for _, marker := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-merge", "rebase-apply", "sequencer"} {
		value, err := Run(ctx, path, 15*time.Second, "rev-parse", "--git-path", marker)
		if err != nil {
			return false, err
		}
		if !filepath.IsAbs(value) {
			value = filepath.Join(path, value)
		}
		if _, err := os.Stat(value); err == nil {
			return true, nil
		} else if !os.IsNotExist(err) {
			return false, err
		}
	}
	return false, nil
}

// Action revalidates the repository immediately before changing it. Fetch remains worktree-free.
func Action(ctx context.Context, path, action, target string, expectedBranch ...string) (string, error) {
	repo, err := Inspect(ctx, path)
	if err != nil {
		return "", err
	}
	if action == "fetch" {
		return "远端更新已获取", Fetch(ctx, path)
	}
	if len(expectedBranch) > 0 && repo.Branch != expectedBranch[0] {
		return "", fmt.Errorf("当前分支已改变，请重新扫描并确认目标分支")
	}
	if repo.Bare || repo.Detached {
		return "", fmt.Errorf("此操作需要普通仓库及当前本地分支")
	}
	if action == "abort-merge" {
		return Run(ctx, path, 30*time.Second, "merge", "--abort")
	}
	active, err := operationInProgress(ctx, path)
	if err != nil {
		return "", err
	}
	if active {
		return "", fmt.Errorf("仓库正在合并、变基或拣选提交，请先完成或中止现有操作")
	}
	switch action {
	case "discard":
		if _, err := Run(ctx, path, 15*time.Second, "rev-parse", "--verify", "HEAD"); err != nil {
			return "", fmt.Errorf("仓库尚无提交，不能撤销到 HEAD")
		}
		// Reset the index first, preserving newly added files as untracked files.
		staged, err := Run(ctx, path, 15*time.Second, "diff", "--cached", "--name-only", "HEAD", "--")
		if err != nil {
			return "", err
		}
		if staged != "" {
			if _, err := Run(ctx, path, 30*time.Second, "restore", "--source=HEAD", "--staged", "--", "."); err != nil {
				return "", err
			}
		}
		changed, err := Run(ctx, path, 15*time.Second, "diff", "--name-only", "--")
		if err != nil {
			return "", err
		}
		if changed != "" {
			if _, err := Run(ctx, path, 30*time.Second, "restore", "--worktree", "--", "."); err != nil {
				return "", err
			}
		}
		return "已撤销已跟踪文件的暂存与工作区修改；未跟踪文件、新增文件和本地提交均保留", nil
	case "merge", "pull-merge":
		if repo.Changed > 0 {
			return "", fmt.Errorf("工作区或暂存区有修改，请先提交、暂存保存或撤销修改")
		}
		if action == "pull-merge" {
			if repo.Upstream == "" {
				return "", fmt.Errorf("当前分支没有跟踪分支，请先在 Git 中配置 upstream")
			}
			if err := Fetch(ctx, path); err != nil {
				return "", err
			}
			target, err = Run(ctx, path, 15*time.Second, "rev-parse", "--symbolic-full-name", "@{upstream}")
			if err != nil {
				return "", err
			}
		}
		refs, err := Branches(ctx, path)
		if err != nil {
			return "", err
		}
		valid := false
		for _, ref := range refs {
			if ref == target {
				valid = true
				break
			}
		}
		if !valid || target == "refs/heads/"+repo.Branch {
			return "", fmt.Errorf("请选择其他有效的本地或远端分支")
		}
		out, err := Run(ctx, path, 2*time.Minute, "-c", "merge.autoStash=false", "merge", "--no-edit", "--no-overwrite-ignore", "--", target)
		if err != nil {
			if _, mergeErr := Run(ctx, path, 15*time.Second, "rev-parse", "--verify", "MERGE_HEAD"); mergeErr == nil {
				return "", fmt.Errorf("合并未完成，已保留现场。请解决冲突后提交，或使用「中止合并」：%w", err)
			}
		}
		return out, err
	default:
		return "", fmt.Errorf("未知仓库操作")
	}
}
