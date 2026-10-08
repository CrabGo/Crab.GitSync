# Crab.GitSync

使用 Wails 3、Go、React 和 TypeScript 构建的 Git 仓库扫描与远端更新桌面工具。

## 当前功能

- 原生目录选择，也可直接输入路径；记住最近一次选择的路径。
- 递归发现普通 Git 仓库、嵌套仓库、worktree 和裸仓库，验证有效性。
- 显示分支、全部远端、GitHub 标识、本地修改、跟踪分支、领先/落后提交和最近提交。
- 搜索与筛选，勾选仓库后批量执行 `git fetch --all --no-recurse-submodules`。
- 扫描发现阶段显示不定进度与遍历数量，信息读取和 fetch 阶段显示完成数与百分比。
- 可取消任务；已完成的扫描结果和 fetch 结果保留。
- 实时日志，按级别过滤、自动滚动、清空显示和导出当前可见日志，最多保留 500 条。

Fetch 只获取远端更新，不执行 pull、merge、push 或切换分支。工作区有修改时也可 fetch。领先/落后数根据本地跟踪分支计算；扫描不访问网络，fetch 后刷新比较结果。每个仓库获取全部远端，未配置远端的仓库会跳过，单个仓库失败后继续处理其他仓库。

## 环境与运行

需要 Go 1.25+、Node.js 22.12+、npm、系统 Git 和 Wails 3 CLI。当前固定使用 Wails `v3.0.0-beta.28`，Windows 运行需要 WebView2 Runtime。

```powershell
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.28
wails3 dev
```

生产构建：

```powershell
wails3 build
```

Windows 输出：`bin/crab-gitsync.exe`。

## 验证

首次验证需先安装前端依赖并构建资源，Go 的 embed 依赖该目录：

```powershell
Set-Location frontend
npm ci
npm run build
Set-Location ..
go test ./...
go vet ./...
wails3 build
```

修改 Go 服务 API 后重新生成绑定：

```powershell
wails3 generate bindings -ts -i
```

集成测试使用临时的本地 Git 仓库，覆盖嵌套仓库、worktree、裸仓库、空仓库、无效仓库、取消、远端凭据脱敏、任务互斥、错误隔离，以及 fetch 后工作区和 HEAD 保持不变。

## 行为与限制

- 扫描跳过 `.git`、`node_modules`、`.venv`，不跟随子目录符号链接；不可读目录记录警告。裸仓库不进入内部对象目录。
- Git 扫描命令超时 15 秒，单个仓库 fetch 超时 2 分钟；批量任务顺序执行。
- 使用本机 Git 的凭据管理器与 SSH 配置。禁用交互式凭据提示，SSH 使用 BatchMode，需先在终端确认认证可用。HTTP 远端 URL 中的凭据和查询参数不显示在界面或日志中。
- 当前版本未提供 GitHub 登录、克隆仓库、自动定时任务、pull/push 或冲突处理。
- 状态通过 Wails 绑定轮询：任务运行时约 300ms，空闲时约 1s。
- 取消不会回滚已获取的远端引用；取消正在运行的 Git 命令后，正在使用的 SSH/凭据辅助进程可能需稍后退出。

## 目录

- `main.go`：桌面应用入口。
- `gitservice.go`：异步任务、状态快照、日志和原生目录选择。
- `updateservice.go`：启动检查、定时检查、更新状态、下载与重启。
- `internal/updatefeed`：私有 GitHub Releases 认证、文件选择与校验值读取。
- `internal/gitengine`：仓库发现、Git 信息读取、fetch 和跨平台进程配置。
- `frontend/src`：仓库工作台界面。
- `frontend/bindings`：自动生成的 Go/TypeScript 绑定。
- `build`：Wails 平台构建与打包配置。

## 应用自动更新

发布源为 [CrabGo/Crab.GitSync Releases](https://github.com/CrabGo/Crab.GitSync/releases)，当前是私有仓库。应用启动时检查一次，此后每 6 小时检查稳定版本。在侧栏「应用更新」中也可手动检查。发现新版后，点击下载更新，查看下载进度；SHA-256 校验成功后，点击「重启应用更新」完成程序替换。正在运行的扫描或 fetch 会阻止重启。取消或下载/校验失败不会替换当前程序。

认证按顺序读取 `GITSYNC_GITHUB_TOKEN`、`GH_TOKEN`、`GITHUB_TOKEN`，最后尝试本机 `gh auth token --hostname github.com`。推荐安装 GitHub CLI 并执行 `gh auth login`。令牌须具备读取本仓库 Releases 的权限；程序不保存令牌，也不会把令牌交给界面或打进发布文件。私有文件通过认证后的 GitHub API 下载，重定向到 CDN 时移除认证头。Windows 下默认使用系统代理，环境变量代理优先。

当前发布与自动更新支持 Windows amd64，文件名固定为 `crab-gitsync-windows-amd64.exe`，同一 Release 必须包含 `SHA256SUMS`。下载校验同时核对 GitHub 返回的文件 digest（如有）。这是文件完整性校验；未配置独立的发布签名或 Windows Authenticode 签名。

更新要求安装目录可写；如果放在需要管理员权限的目录中，可从 Releases 手动下载覆盖。更新替换、失败恢复与重启使用 Wails 内置 updater helper。扫描路径保留在应用的本地存储中。

### 发布新版

```powershell
./build/release.ps1 -Version 0.2.0
```

该脚本构建前端、生成绑定、执行 Go 测试和静态检查，再构建带版本信息的 Windows 程序，输出到 `bin/release`。版本通过 `-X main.Version` 注入程序，同时写入 Windows 文件元数据。

完成代码提交后，推送稳定版本标签，例如：

```powershell
git tag v0.3.0
git push origin main
git push origin v0.3.0
```

`.github/workflows/release.yml` 在 main/PR 上执行构建检查，在版本标签上创建草稿 Release，上传程序与校验文件后一起发布，避免应用读取到文件不完整的 Release。不能覆盖已经发布的版本；修复请发布新标签。

## 可继续讨论

多扫描路径、可配置排除目录、并发 fetch、定时获取、失败重试、GitHub 仓库克隆，以及未来按仓库选择同步策略。
