package gitengine

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MergePreview pins both sides of a confirmation to immutable commit IDs.
type MergePreview struct {
	Path       string `json:"path"`
	Action     string `json:"action"`
	Branch     string `json:"branch"`
	Head       string `json:"head"`
	Target     string `json:"target"`
	TargetHead string `json:"targetHead"`
	Ahead      int    `json:"ahead"`
	Behind     int    `json:"behind"`
	Diverged   bool   `json:"diverged"`
}

func PreviewMerge(ctx context.Context, path, action, target string) (MergePreview, error) {
	p := MergePreview{Path: path, Action: action}
	if action != "merge" && action != "pull-merge" {
		return p, fmt.Errorf("未知合并操作")
	}
	r, err := Inspect(ctx, path)
	if err != nil {
		return p, err
	}
	if r.Bare || r.Detached {
		return p, fmt.Errorf("此操作需要普通仓库及当前本地分支")
	}
	if r.Changed > 0 {
		return p, fmt.Errorf("工作区或暂存区有修改，请先处理本地修改")
	}
	active, err := operationInProgress(ctx, path)
	if err != nil {
		return p, err
	}
	if active {
		return p, fmt.Errorf("仓库正在合并、变基或拣选提交，请先完成或中止现有操作")
	}
	if action == "pull-merge" {
		if r.Upstream == "" {
			return p, fmt.Errorf("当前分支没有跟踪分支")
		}
		target, err = Run(ctx, path, 15*time.Second, "rev-parse", "--symbolic-full-name", "@{upstream}")
		if err != nil {
			return p, err
		}
	}
	refs, err := Branches(ctx, path)
	if err != nil {
		return p, err
	}
	valid := false
	for _, ref := range refs {
		if ref == target {
			valid = true
			break
		}
	}
	if !valid || target == "refs/heads/"+r.Branch {
		return p, fmt.Errorf("请选择其他有效的本地或远端分支")
	}
	p.Branch, p.Target = r.Branch, target
	p.Head, err = Run(ctx, path, 15*time.Second, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return p, err
	}
	p.TargetHead, err = Run(ctx, path, 15*time.Second, "rev-parse", "--verify", target+"^{commit}")
	if err != nil {
		return p, err
	}
	counts, err := Run(ctx, path, 15*time.Second, "rev-list", "--left-right", "--count", p.Head+"..."+p.TargetHead)
	if err != nil {
		return p, err
	}
	parts := strings.Fields(counts)
	if len(parts) != 2 {
		return p, fmt.Errorf("无法读取合并提交差异")
	}
	p.Ahead, err = strconv.Atoi(parts[0])
	if err != nil {
		return p, err
	}
	p.Behind, err = strconv.Atoi(parts[1])
	if err != nil {
		return p, err
	}
	p.Diverged = p.Ahead > 0 && p.Behind > 0
	return p, nil
}

// ExecuteMerge rechecks confirmation before fetching and immediately before writing.
// The returned flag records a complete fetch even when the following merge is rejected.
func ExecuteMerge(ctx context.Context, p MergePreview, strategy string) (string, bool, error) {
	return executeMerge(ctx, p, strategy, Fetch)
}

func executeMerge(ctx context.Context, p MergePreview, strategy string, fetch func(context.Context, string) error) (string, bool, error) {
	if strategy != "ff-only" && strategy != "merge" {
		return "", false, fmt.Errorf("未知合并策略")
	}
	fresh, err := PreviewMerge(ctx, p.Path, p.Action, p.Target)
	if err != nil {
		return "", false, err
	}
	if fresh.Branch != p.Branch || fresh.Head != p.Head || fresh.Target != p.Target || fresh.TargetHead != p.TargetHead {
		return "", false, fmt.Errorf("分支或提交已变化，请重新预览并确认")
	}
	fetched := false
	if p.Action == "pull-merge" {
		if err := fetch(ctx, p.Path); err != nil {
			return "", false, err
		}
		fetched = true
	}
	fresh, err = PreviewMerge(ctx, p.Path, p.Action, p.Target)
	if err != nil {
		return "", fetched, err
	}
	if fresh.Branch != p.Branch || fresh.Head != p.Head || fresh.Target != p.Target {
		return "", fetched, fmt.Errorf("当前分支或跟踪目标已变化，请重新预览并确认")
	}
	if strategy == "merge" && fresh.TargetHead != p.TargetHead {
		return "", fetched, fmt.Errorf("获取后目标提交已变化，普通合并需要重新预览并确认")
	}
	if strategy == "ff-only" && fresh.Diverged {
		return "", fetched, fmt.Errorf("双方分叉，无法仅快进；已保留 HEAD 和工作区，请重新预览并明确选择普通合并")
	}
	args := []string{"-c", "merge.autoStash=false", "merge", "--no-edit", "--no-overwrite-ignore"}
	if strategy == "ff-only" {
		args = append(args, "--ff-only")
	}
	// Pin the target to the freshly validated commit, preventing ref changes mid-command.
	args = append(args, "--", fresh.TargetHead)
	out, err := Run(ctx, p.Path, 2*time.Minute, args...)
	if err != nil {
		if _, mergeErr := Run(ctx, p.Path, 15*time.Second, "rev-parse", "--verify", "MERGE_HEAD"); mergeErr == nil {
			return "", fetched, fmt.Errorf("合并未完成，已保留现场。请解决冲突后提交，或使用「中止合并」：%w", err)
		}
	}
	return out, fetched, err
}
