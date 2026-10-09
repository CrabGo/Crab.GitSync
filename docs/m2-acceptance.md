# M2 v0.5.1 验收记录

## 范围与证据

- P09：命名扫描列表、多根目录、路径规范化去重及排除规则；配置和扫描用临时目录测试，测试见 scanlists_test.go、internal/scansettings/settings_test.go、internal/gitengine/multiscan_test.go。
- P10：任务准入、状态读取租约、仓库共享 Git 目录锁与取消等待；scheduler_test.go 和 internal/taskqueue/scheduler_test.go 覆盖取消期间禁止新任务/重启、同仓库不重叠、相反锁顺序及读取租约。
- P11：concurrency_test.go 覆盖 1/3/5 并发上限、配置快照、单调进度、独立失败、取消停止派发并等待全部工作、刷新持续占用名额。隔离浏览器实例已验证五个临时仓库在并发 2 时两个运行、三个等待，取消后回到终态。
- P12：historyservice_test.go 和 internal/taskhistory/history_test.go 覆盖进程恢复后的获取时间、重新扫描不刷新时间、保留策略、脱敏、损坏备份、写入失败不影响 Git 成功、进行中任务不清理。隔离浏览器实例验证历史结果和日志、重启/重扫、调整策略与主动导出文件内容。
- P13：internal/updatefeed/public_test.go 覆盖无认证、同标签说明、缺失、超大、无效 UTF-8、超时回退且保留程序校验值。隔离 Wails 服务的种子说明验证四节中文排版和 HTML 原文显示，日志容器没有 script 子元素；再次检查无说明版本后下载入口保留。发布流程要求标题版本匹配且不超过 64 KiB，并对附件与源说明逐字比较。

## 发布前验证

2026-10-09 执行 `./build/release.ps1 -Version 0.5.1` 成功，包含前端 npm ci/build、绑定生成、Go 全量测试、vet 和 Windows 桌面构建。Go 测试强制重新执行并设置 150 秒包级超时。另执行根包、taskqueue、taskhistory、scansettings、tasksettings、updatefeed 的竞态测试，全部通过。

本地 EXE 数值文件版本为 0.5.1.0；发布包包含 crab-gitsync-windows-amd64.exe、SHA256SUMS、CHANGELOG.md。版本准备提交为 93ae98e，v0.5.1 为新标签，不覆盖已有版本。

## 线上验证

[GitHub Actions](https://github.com/CrabGo/Crab.GitSync/actions/runs/37922146571) 的构建及发布任务均成功。[v0.5.1 发布页](https://github.com/CrabGo/Crab.GitSync/releases/tag/v0.5.1) 为非草稿、非预发布，正文与 releases/v0.5.1.md 一致。附件 CHANGELOG.md 为 1614 字节、EXE 为 12871680 字节、SHA256SUMS 为 98 字节。公开 provider 使用应用代理且不带 GitHub 凭据，从 0.5.0 检查识别 0.5.1、读取同标签日志并与源说明比较一致、下载 EXE 后实际 SHA256 与线上摘要一致：a5e92587f640f44533fc3d2622e783762e6de4667ef151df80563df4fb740021。在线 EXE 数值文件版本为 0.5.1.0；以 0.5.1 检查返回无更新。没有执行安装替换或 Restart。

## 验证边界

Git 测试只操作临时仓库和本地服务。浏览器验证不代替原生 Windows 托盘位置、系统通知横幅/点击及实际更新替换重启；这些系统交互未验证。没有调用开发者主应用的 Restart，也没有修改用户仓库。
