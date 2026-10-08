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

需要 Go 1.24+、Node.js 22.12+、npm、系统 Git 和 Wails 3 CLI。当前固定使用 Wails `v3.0.0-beta.28`，Windows 运行需要 WebView2 Runtime。

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
- `internal/gitengine`：仓库发现、Git 信息读取、fetch 和跨平台进程配置。
- `frontend/src`：仓库工作台界面。
- `frontend/bindings`：自动生成的 Go/TypeScript 绑定。
- `build`：Wails 平台构建与打包配置。

## 可继续讨论

多扫描路径、可配置排除目录、并发 fetch、定时获取、失败重试、GitHub 仓库克隆，以及未来按仓库选择同步策略。
