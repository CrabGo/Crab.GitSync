# Crab.GitSync

使用 Wails 3、Go、React 和 TypeScript 构建的 Git 仓库扫描与远端更新桌面工具。

## 当前功能

- 原生目录选择，也可直接输入路径；默认加载上次使用的目录，下拉保留最近 20 个路径。
- 仓库工作台、任务日志、应用更新、使用说明为同一窗口内的独立页面，切换时同步修改窗口标题。
- 螃蟹与 Git 分支结合的新 Logo，统一用于界面、程序图标和托盘。
- 关闭窗口后继续在托盘运行，单击恢复窗口；右键菜单支持页面切换、检查更新、取消任务和退出。
- 扫描/fetch 完成或取消、发现新版、下载校验完成时发送系统通知，点击可打开对应页面。
- 递归发现普通 Git 仓库、嵌套仓库、worktree 和裸仓库，验证有效性。
- 显示分支、全部远端、GitHub 标识、本地修改、跟踪分支、领先/落后提交和最近提交。
- 搜索与筛选，勾选仓库后批量执行 `git fetch --all --no-recurse-submodules`。
- 扫描发现阶段显示不定进度与遍历数量，信息读取和 fetch 阶段显示完成数与百分比。
- 可取消任务；已完成的扫描结果和 fetch 结果保留。
- 实时日志，按级别过滤、自动滚动、清空显示和导出当前可见日志，最多保留 500 条。

批量 Fetch 和右键「拉取」只获取远端更新，不修改当前分支或工作区。右键「合并」可以选择本地或远端分支，右键「拉取并合并」获取远端更新后合并当前跟踪分支；两者都需确认，并要求工作区干净。冲突会保留现场，可用「中止合并」恢复。右键「撤销本地修改」经确认后恢复已跟踪文件的暂存与工作区内容，保留新增/未跟踪文件和本地提交，不递归撤销子模块内部修改。工作区有修改时也可 fetch。领先/落后数根据本地跟踪分支计算；扫描不访问网络，fetch 后刷新比较结果。每个仓库获取全部远端，未配置远端的仓库会跳过，单个仓库失败后继续处理其他仓库。

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
- 当前版本未提供 GitHub 登录、克隆仓库、自动定时 Git 同步、push 或可视化冲突编辑。冲突需在外部工具中解决并提交，或在右键菜单中中止合并。
- 状态通过 Wails 绑定轮询：任务运行时约 300ms，空闲时约 1s。
- 取消不会回滚已获取的远端引用；取消正在运行的 Git 命令后，正在使用的 SSH/凭据辅助进程可能需稍后退出。

## 目录

- `main.go`：桌面应用入口。
- `desktopservice.go`：窗口标题、单实例、系统托盘和通知。
- `gitservice.go`：异步任务、状态快照、日志和原生目录选择。
- `updateservice.go`：启动检查、定时检查、更新状态、下载与重启。
- `internal/updatefeed`：公开 GitHub Releases 检查、文件下载与校验值读取；保留可选的私有源 provider。
- `internal/gitengine`：仓库发现、Git 信息读取、fetch 和跨平台进程配置。
- `frontend/src`：仓库工作台界面。
- `frontend/bindings`：自动生成的 Go/TypeScript 绑定。
- `build`：Wails 平台构建与打包配置。

## 应用自动更新

发布源为 [CrabGo/Crab.GitSync Releases](https://github.com/CrabGo/Crab.GitSync/releases)，当前是公开仓库，无需 GitHub 登录或配置令牌。应用启动时检查一次，此后每 6 小时检查稳定版本。在侧栏「应用更新」中也可手动检查。发现新版后，点击下载更新，查看下载进度；SHA-256 校验成功后，点击「重启应用更新」完成程序替换。正在运行的扫描或 fetch 会阻止重启。取消或下载/校验失败不会替换当前程序。

应用通过公开 Releases 的 `/releases/latest` 稳定版本重定向发现新版，并直接下载发布文件，不使用匿名 GitHub API，不受 API 配额影响，也不读取本机令牌。Windows 下默认使用系统代理，环境变量代理优先。

当前发布与自动更新支持 Windows amd64，文件名固定为 `crab-gitsync-windows-amd64.exe`，同一 Release 必须包含 `SHA256SUMS`。更新必须提供有效的 SHA-256 校验值，下载后验证文件内容。这是文件完整性校验；未配置独立的发布签名或 Windows Authenticode 签名。

更新要求安装目录可写；如果放在需要管理员权限的目录中，可从 Releases 手动下载覆盖。更新替换、失败恢复与重启使用 Wails 内置 updater helper。扫描路径保留在应用的本地存储中。

### 代理配置

侧栏「网络设置」可启用并保存 HTTP / SOCKS5 代理。默认启用 `http://127.0.0.1:33210`，需先启动本机代理程序。HTTPS 仓库远端获取、应用更新检查与下载共享该配置；SSH 仓库沿用本机 SSH 配置。关闭应用代理后沿用原有 Git、系统及环境变量配置。配置保存在用户配置目录的 `Crab.GitSync/network.json`，不会修改 Git 或系统代理设置。新请求使用保存后的配置，运行中的 Git 任务保留启动时配置。

### 发布新版

```powershell
./build/release.ps1 -Version 0.4.2
```

发布前必须填写 `releases/v版本号.md`，分别列出「新增」「修改」「删除」「优化」的实际功能变化，无变化的栏目写 `- 无。`。脚本会先校验该版本的 ChangeLog，缺失或栏目为空时停止发布。GitHub Release 正文直接使用该文件，不使用自动生成的提交列表。

该脚本构建前端、生成绑定、执行 Go 测试和静态检查，再构建带版本信息的 Windows 程序，输出到 `bin/release`。版本通过 `-X main.Version` 注入程序，同时写入 Windows 文件元数据。

完成代码提交后，推送稳定版本标签，例如：

```powershell
git tag v0.4.2
git push origin main
git push origin v0.4.2
```

`.github/workflows/release.yml` 在 main/PR 上执行构建检查，在版本标签上创建草稿 Release，上传程序与校验文件后一起发布，避免应用读取到文件不完整的 Release。不能覆盖已经发布的版本；修复请发布新标签。

## 可继续讨论

多扫描路径、可配置排除目录、并发 fetch、定时获取、失败重试、GitHub 仓库克隆，以及未来按仓库选择同步策略。
