package gitengine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

const discardLimit = 64 << 20

type DiscardFile struct {
	Path        string `json:"path"`
	Fingerprint string `json:"fingerprint"`
}
type DiscardPreview struct {
	Path   string        `json:"path"`
	Branch string        `json:"branch"`
	Head   string        `json:"head"`
	Files  []DiscardFile `json:"files"`
}
type DiscardBackup struct {
	ID         string   `json:"id"`
	Repository string   `json:"repository"`
	CreatedAt  string   `json:"createdAt"`
	Files      []string `json:"files"`
	Ready      bool     `json:"ready"`
}
type fileSnapshot struct {
	Path      string
	Exists    bool
	Mode      uint32
	Link      string
	Data      []byte
	IndexMode string
	IndexOID  string
	IndexData []byte
}
type discardManifest struct {
	Version      int
	Info         DiscardBackup
	Branch       string
	Head         string
	Original     []fileSnapshot
	OriginalHash string
	After        []DiscardFile
}

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(data []byte) (int, error) {
	if b.Len()+len(data) > discardLimit {
		return 0, fmt.Errorf("文件内容超过 64 MiB 安全限制")
	}
	return b.Buffer.Write(data)
}

// gitData preserves arbitrary bytes, trailing spaces, and NUL-delimited file names.
func gitData(ctx context.Context, path string, input []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	prefix := []string{"-C", path}
	if proxy, ok := ctx.Value(proxyKey{}).(string); ok && proxy != "" {
		prefix = append(prefix, "-c", "http.proxy="+proxy)
	}
	cmd := exec.CommandContext(ctx, "git", append(prefix, args...)...)
	configureProcess(cmd)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "GIT_OPTIONAL_LOCKS=0")
	cmd.Stdin = bytes.NewReader(input)
	cmd.WaitDelay = 2 * time.Second
	var stdout boundedOutput
	var stderr boundedOutput
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("Git 文件操作失败：%s：%w", Redact(stderr.String()), err)
	}
	return append([]byte{}, stdout.Bytes()...), nil
}

// selectedPath rejects traversal and parent links before any filesystem operation.
func selectedPath(root, name string) (string, error) {
	if name == "" || filepath.IsAbs(name) || strings.ContainsAny(name, "\\\x00\r\n") {
		return "", fmt.Errorf("无效的文件路径")
	}
	parts := strings.Split(name, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.EqualFold(part, ".git") {
			return "", fmt.Errorf("文件路径超出允许范围")
		}
		if runtime.GOOS == "windows" && (strings.Contains(part, ":") || strings.TrimRight(part, " .") != part) {
			return "", fmt.Errorf("文件路径包含 Windows 特殊别名")
		}
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	current := canonical
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("文件父目录不是普通目录")
		}
	}
	return filepath.Join(canonical, filepath.FromSlash(name)), nil
}
func captureFile(ctx context.Context, root, name string) (fileSnapshot, error) {
	s := fileSnapshot{Path: name}
	path, err := selectedPath(root, name)
	if err != nil {
		return s, err
	}
	info, err := os.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		return s, err
	}
	if err == nil {
		s.Exists = true
		s.Mode = uint32(info.Mode().Perm())
		if info.Mode()&os.ModeSymlink != 0 {
			s.Link, err = os.Readlink(path)
		} else if info.Mode().IsRegular() {
			if info.Size() > discardLimit {
				return s, fmt.Errorf("文件超过 64 MiB，未执行撤销")
			}
			var f *os.File
			f, err = os.Open(path)
			if err == nil {
				s.Data, err = io.ReadAll(io.LimitReader(f, discardLimit+1))
				f.Close()
				if len(s.Data) > discardLimit {
					err = fmt.Errorf("文件超过 64 MiB，未执行撤销")
				}
			}
		} else {
			err = fmt.Errorf("不能撤销目录或特殊文件")
		}
		if err != nil {
			return s, err
		}
	}
	entries, err := gitData(ctx, root, nil, "--literal-pathspecs", "ls-files", "--stage", "-z", "--", name)
	if err != nil {
		return s, err
	}
	if len(entries) > 0 {
		entry := strings.TrimSuffix(string(entries), "\x00")
		parts := strings.SplitN(entry, "\t", 2)
		if len(parts) != 2 || parts[1] != name {
			return s, fmt.Errorf("暂存区文件范围无效")
		}
		fields := strings.Fields(parts[0])
		if len(fields) != 3 || fields[2] != "0" {
			return s, fmt.Errorf("文件存在未解决的冲突")
		}
		s.IndexMode, s.IndexOID = fields[0], fields[1]
		if s.IndexMode != "100644" && s.IndexMode != "100755" && s.IndexMode != "120000" {
			return s, fmt.Errorf("不支持撤销子模块或特殊暂存条目")
		}
		s.IndexData, err = gitData(ctx, root, nil, "cat-file", "blob", s.IndexOID)
		if err != nil {
			return s, err
		}
	}
	return s, nil
}
func fingerprint(s fileSnapshot) string {
	data, _ := json.Marshal(s)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func snapshotsHash(files []fileSnapshot) string {
	data, _ := json.Marshal(files)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func discardIdentity(ctx context.Context, path string) (DiscardPreview, error) {
	p := DiscardPreview{Path: path, Files: []DiscardFile{}}
	repo, err := Inspect(ctx, path)
	if err != nil {
		return p, err
	}
	if repo.Bare || repo.Detached {
		return p, fmt.Errorf("此操作需要普通仓库及当前本地分支")
	}
	active, err := operationInProgress(ctx, path)
	if err != nil {
		return p, err
	}
	if active {
		return p, fmt.Errorf("请先完成或中止现有合并、变基或拣选")
	}
	p.Branch = repo.Branch
	p.Head, err = Run(ctx, path, 15*time.Second, "rev-parse", "--verify", "HEAD^{commit}")
	return p, err
}
func PreviewDiscard(ctx context.Context, path string) (DiscardPreview, error) {
	p, err := discardIdentity(ctx, path)
	if err != nil {
		return p, err
	}
	tracked, err := gitData(ctx, path, nil, "ls-tree", "-r", "-z", "HEAD")
	if err != nil {
		return p, err
	}
	allowed := map[string]bool{}
	for _, line := range strings.Split(string(tracked), "\x00") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 && !strings.HasPrefix(parts[0], "160000 ") {
			allowed[parts[1]] = true
		}
	}
	changed, err := gitData(ctx, path, nil, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--name-only", "-z", "HEAD", "--")
	if err != nil {
		return p, err
	}
	staged, err := gitData(ctx, path, nil, "diff", "--cached", "--no-ext-diff", "--no-textconv", "--no-renames", "--name-only", "-z", "HEAD", "--")
	if err != nil {
		return p, err
	}
	changed = append(changed, staged...)
	seen := map[string]bool{}
	for _, name := range strings.Split(string(changed), "\x00") {
		if !allowed[name] || seen[name] {
			continue
		}
		seen[name] = true
		s, err := captureFile(ctx, path, name)
		if err != nil {
			return p, err
		}
		p.Files = append(p.Files, DiscardFile{Path: name, Fingerprint: fingerprint(s)})
	}
	if len(p.Files) > 500 {
		return p, fmt.Errorf("修改文件超过 500 个，请分批处理")
	}
	return p, nil
}
func selectedSnapshots(ctx context.Context, p DiscardPreview, names []string) ([]fileSnapshot, error) {
	fresh, err := PreviewDiscard(ctx, p.Path)
	if err != nil {
		return nil, err
	}
	if fresh.Head != p.Head || fresh.Branch != p.Branch {
		return nil, fmt.Errorf("分支或 HEAD 已改变，请重新预览")
	}
	expected := map[string]string{}
	for _, f := range p.Files {
		expected[f.Path] = f.Fingerprint
	}
	current := map[string]string{}
	for _, f := range fresh.Files {
		current[f.Path] = f.Fingerprint
	}
	seen := map[string]bool{}
	result := []fileSnapshot{}
	total := 0
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		if expected[name] == "" || expected[name] != current[name] {
			return nil, fmt.Errorf("所选文件已改变或不在预览范围：%s", name)
		}
		s, err := captureFile(ctx, p.Path, name)
		if err != nil {
			return nil, err
		}
		if fingerprint(s) != expected[name] {
			return nil, fmt.Errorf("所选文件在读取期间已改变")
		}
		total += len(s.Data) + len(s.IndexData)
		if total > discardLimit {
			return nil, fmt.Errorf("所选文件总内容超过 64 MiB，请分批处理")
		}
		result = append(result, s)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("请至少选择一个已跟踪文件")
	}
	return result, nil
}

func writeManifest(dir string, m discardManifest) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "backup-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, m.Info.ID+".json"))
}

// DiscardFiles writes an explicit backup before mutation; new and untracked paths are excluded.
func DiscardFiles(ctx context.Context, p DiscardPreview, names []string, backupDir string) (DiscardBackup, error) {
	info := DiscardBackup{}
	original, err := selectedSnapshots(ctx, p, names)
	if err != nil {
		return info, err
	}
	selected := []string{}
	for _, f := range original {
		selected = append(selected, f.Path)
	}
	m := discardManifest{Version: 1, Branch: p.Branch, Head: p.Head, Original: original, OriginalHash: snapshotsHash(original)}
	if backupDir != "" {
		m.Info = DiscardBackup{ID: fmt.Sprintf("backup-%d", time.Now().UnixNano()), Repository: p.Path, CreatedAt: time.Now().Format(time.RFC3339Nano), Files: selected}
		if err = writeManifest(backupDir, m); err != nil {
			return info, fmt.Errorf("备份未成功，未执行撤销：%w", err)
		}
		info = m.Info
	}
	if _, err = selectedSnapshots(ctx, p, selected); err != nil {
		return info, err
	}
	args := append([]string{"--literal-pathspecs", "restore", "--source=HEAD", "--staged", "--worktree", "--"}, selected...)
	_, actionErr := gitData(ctx, p.Path, nil, args...)
	if backupDir != "" {
		// Cancellation must still drain the backup finalisation before releasing the repository lease.
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, name := range selected {
			s, err := captureFile(cleanup, p.Path, name)
			if err != nil {
				return info, fmt.Errorf("已保存原始备份，但无法确认恢复基线：%w", err)
			}
			m.After = append(m.After, DiscardFile{Path: name, Fingerprint: fingerprint(s)})
		}
		m.Info.Ready = true
		if err = writeManifest(backupDir, m); err != nil {
			return info, fmt.Errorf("原始备份已保存，恢复基线写入失败：%w", err)
		}
		info = m.Info
	}
	return info, actionErr
}

var backupIDPattern = regexp.MustCompile(`^backup-[0-9]+$`)

func readManifest(dir, id string) (discardManifest, error) {
	m := discardManifest{}
	if !backupIDPattern.MatchString(id) {
		return m, fmt.Errorf("无效的备份 ID")
	}
	f, err := os.Open(filepath.Join(dir, id+".json"))
	if err != nil {
		return m, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 256<<20))
	if err != nil {
		return m, err
	}
	if err = json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	if m.Version != 1 || m.Info.ID != id || len(m.Original) == 0 || len(m.Original) > 500 {
		return m, fmt.Errorf("备份内容无效")
	}
	if len(m.Info.Files) != len(m.Original) || m.OriginalHash != snapshotsHash(m.Original) {
		return m, fmt.Errorf("备份文件范围或内容校验失败")
	}
	for i, f := range m.Original {
		if m.Info.Files[i] != f.Path {
			return m, fmt.Errorf("备份文件范围与保存内容不一致")
		}
	}
	return m, nil
}
func ListDiscardBackups(dir, path string) ([]DiscardBackup, error) {
	result := []DiscardBackup{}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		m, err := readManifest(dir, id)
		if err != nil {
			continue
		}
		if sameRepository(m.Info.Repository, path) {
			result = append(result, m.Info)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt > result[j].CreatedAt })
	return result, nil
}
func sameRepository(a, b string) bool {
	a, _ = filepath.Abs(a)
	b, _ = filepath.Abs(b)
	ca, ea := filepath.EvalSymlinks(a)
	cb, eb := filepath.EvalSymlinks(b)
	if ea != nil || eb != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(ca, cb)
	}
	return ca == cb
}

// RestoreDiscardBackup refuses to overwrite changes made after discard and restores selected index entries only.
func RestoreDiscardBackup(ctx context.Context, path, dir, id string) (returnErr error) {
	m, err := readManifest(dir, id)
	if err != nil {
		return err
	}
	if !m.Info.Ready || !sameRepository(m.Info.Repository, path) || len(m.After) != len(m.Original) {
		return fmt.Errorf("备份未就绪或不属于当前仓库")
	}
	p, err := discardIdentity(ctx, path)
	if err != nil {
		return err
	}
	if p.Head != m.Head || p.Branch != m.Branch {
		return fmt.Errorf("分支或 HEAD 已改变，未覆盖当前修改")
	}
	if m.OriginalHash != snapshotsHash(m.Original) {
		return fmt.Errorf("原始备份内容校验失败")
	}
	tree, err := gitData(ctx, path, nil, "ls-tree", "-r", "-z", "HEAD")
	if err != nil {
		return err
	}
	headFiles := map[string]bool{}
	for _, line := range strings.Split(string(tree), "\x00") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 && !strings.HasPrefix(parts[0], "160000 ") {
			headFiles[parts[1]] = true
		}
	}
	allowed := map[string]string{}
	for _, f := range m.After {
		if allowed[f.Path] != "" {
			return fmt.Errorf("备份路径重复")
		}
		allowed[f.Path] = f.Fingerprint
	}
	seen := map[string]bool{}
	index := bytes.Buffer{}
	for _, f := range m.Original {
		if !headFiles[f.Path] {
			return fmt.Errorf("恢复范围包含新增文件或非普通已跟踪文件")
		}
		if seen[f.Path] || allowed[f.Path] == "" {
			return fmt.Errorf("备份文件范围无效")
		}
		seen[f.Path] = true
		current, err := captureFile(ctx, path, f.Path)
		if err != nil {
			return err
		}
		if fingerprint(current) != allowed[f.Path] {
			return fmt.Errorf("撤销后文件已改变，未覆盖：%s", f.Path)
		}
		if f.IndexOID == "" {
			fmt.Fprintf(&index, "0 %s\t%s\x00", strings.Repeat("0", len(p.Head)), f.Path)
		} else {
			if f.IndexMode != "100644" && f.IndexMode != "100755" && f.IndexMode != "120000" {
				return fmt.Errorf("备份暂存区模式无效")
			}
			oid, err := gitData(ctx, path, f.IndexData, "hash-object", "-w", "--stdin")
			if err != nil {
				return err
			}
			if strings.TrimSpace(string(oid)) != f.IndexOID {
				return fmt.Errorf("备份暂存内容校验失败")
			}
			fmt.Fprintf(&index, "%s %s\t%s\x00", f.IndexMode, f.IndexOID, f.Path)
		}
	}
	// Recheck every selected file once more before changing the index or worktree.
	for _, f := range m.After {
		current, err := captureFile(ctx, path, f.Path)
		if err != nil {
			return err
		}
		if fingerprint(current) != f.Fingerprint {
			return fmt.Errorf("恢复确认期间文件已改变")
		}
	}
	identity, err := discardIdentity(ctx, path)
	if err != nil {
		return err
	}
	if identity.Head != m.Head || identity.Branch != m.Branch {
		return fmt.Errorf("恢复前分支或 HEAD 已改变")
	}
	if _, err = gitData(ctx, path, index.Bytes(), "update-index", "-z", "--index-info"); err != nil {
		return err
	}
	// Keep the original snapshot and advance the guarded baseline after partial/successful restore.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		after := []DiscardFile{}
		for _, f := range m.Original {
			snapshot, err := captureFile(cleanup, path, f.Path)
			if err != nil {
				returnErr = fmt.Errorf("%v；恢复基线读取失败，原始备份仍保留：%w", returnErr, err)
				return
			}
			after = append(after, DiscardFile{Path: f.Path, Fingerprint: fingerprint(snapshot)})
		}
		m.After = after
		if err := writeManifest(dir, m); err != nil {
			returnErr = fmt.Errorf("%v；恢复基线保存失败，原始备份仍保留：%w", returnErr, err)
		}
	}()
	for _, f := range m.Original {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		target, err := selectedPath(path, f.Path)
		if err != nil {
			return err
		}
		if !f.Exists {
			err = os.Remove(target)
			if os.IsNotExist(err) {
				err = nil
			}
		} else if f.Link != "" {
			if err = os.Remove(target); err == nil {
				err = os.Symlink(f.Link, target)
			}
		} else {
			var tmp *os.File
			tmp, err = os.CreateTemp(filepath.Dir(target), ".gitsync-restore-*")
			if err == nil {
				_, err = tmp.Write(f.Data)
				closeErr := tmp.Close()
				if err == nil {
					err = closeErr
				}
				if err == nil {
					err = os.Chmod(tmp.Name(), os.FileMode(f.Mode))
				}
				if err == nil {
					err = os.Rename(tmp.Name(), target)
				}
				os.Remove(tmp.Name())
			}
		}
		if err != nil {
			return fmt.Errorf("恢复部分完成，原始备份仍保留：%w", err)
		}
	}
	return nil
}
