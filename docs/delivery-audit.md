# M1–M3 交付核对

依据 docs/development-plan.md 的 P01–P16 及发布门槛核对。代码图谱 MCP 在本会话未提供可调用工具，采用指定源码及测试读取；下表是任务范围的证据映射，不声称知识图谱完整覆盖。

## 功能与验收映射

| 任务 | 当前实现 | 验证证据 |
| --- | --- | --- |
| P01 结果与错误 | taskresult 分类/脱敏；逐仓库阶段、状态、尝试次数与耗时；fetch/刷新错误独立 | internal/taskresult/error_test.go、taskresults_test.go、m1integration_test.go |
| P02 连接诊断 | 保存配置快照，代理端口/HTTPS/Git 只读分步探针，SSH 范围说明与取消 | internal/diagnostics/diagnostics_test.go；M1 隔离界面错误详情 |
| P03 失败重试 | 按来源失败项、当前扫描范围重验；刷新失败只重读 | retryfailed_test.go、m1integration_test.go；M1 两项任务只重试一项 |
| P04 自动重试 | 默认关闭；网络类别最多两次追加，2/5 秒等待可取消 | autoretry_test.go；M1 真实失败端口三次尝试 |
| P05 同步状态 | 提交关系/工作区分别表达，缺失引用为未知；完整成功 fetch 时间 | internal/gitengine/syncstatus_test.go、fetchtime_test.go；M1 分叉及获取新鲜度界面 |
| P06 合并 | 预览提交关系，默认仅快进；普通合并明确确认；执行前/fetch 后重验，冲突保留 | internal/gitengine/merge_test.go、mergeconfirmation_test.go；M1 策略/确认界面 |
| P07 集成 | 临时真实仓库批量/重试/右键 fetch 保留 HEAD、脏工作区及新文件；取消收尾与路由 | m1integration_test.go、desktopservice_test.go、scheduler_test.go；docs/m1-acceptance.md |
| P08 M1 发布 | 四节中文说明、构建、公开更新及摘要 | releases/v0.5.0.md、docs/m1-acceptance.md；稳定 v0.5.0 |
| P09 多路径 | 命名列表/根目录/排除，规范化去重；不可读继续，不跟随子目录链接 | scanlists_test.go、internal/scansettings、internal/gitengine/multiscan_test.go；M2 隔离多根扫描 |
| P10 调度互斥 | 主任务/读取租约准入，公共 Git 目录锁；取消等待、重启冻结 | scheduler_test.go、internal/taskqueue/scheduler_test.go；M2 取消现场 |
| P11 并发 | 默认 3、范围 1–5；获取/重试/刷新共用名额；取消停止派发并等待 | concurrency_test.go、internal/taskqueue；M2 延迟本地 HTTP 与五仓库界面 |
| P12 历史 | 原子保存、脱敏、数量/时间清理、损坏备份/告警、写入失败不改变 Git 成功 | historyservice_test.go、internal/taskhistory、internal/tasksettings；M2 独立实例重启/重扫/导出 |
| P13 ChangeLog | 同标签附件，大小/UTF-8/超时限制，纯文本显示，加载失败回退且保留校验 | internal/updatefeed/public_test.go、正式说明校验脚本；docs/m2-acceptance.md |
| P14 定时 | 默认关闭、1–1440 分钟，忙碌顺延/不补跑/公平调度；只 fetch，变化/失败通知 | schedules_test.go、internal/scansettings；M3 一分钟真实运行、重启及关闭开关 |
| P15 快捷/远端 | 参数调用/范围重验；GitHub 链接限制、地址脱敏；所选远端及重试保持 | repositorylinks_test.go、平台进程参数测试；M3 双远端选 origin 实际成功 |
| P16 文件撤销 | 明确选择/确认；可选原始内容备份、Ready 状态、受保护恢复；其他/新增文件保留 | discardservice_test.go、internal/gitengine/discardfiles_test.go；docs/m3-acceptance.md 的字节/暂存/界面验收 |

所有任务表状态已完成；发布完成状态必须单独结合各里程碑验收记录，不以状态表代替线上证据。

## 保留行为

- 批量、定时、右键拉取和指定远端获取均为 fetch，测试验证 HEAD/工作区保留；合并、撤销及恢复均为单独明确确认操作。
- 普通合并冲突保留现场，不自动 stash、rebase、force、提交或清理未跟踪文件。
- 代理只作为 Git 命令参数和动态 HTTP 请求配置；不写全局/系统 Git 设置。internal/updatefeed/proxy_test.go 验证保存配置切换，updateservice_test.go 验证连续检查及失败后检查不重复 Init。
- Git 测试只在临时仓库、连接测试只用本地测试服务；公开线上验证只检查/下载，不调用开发者主应用 Restart。
- 生成绑定由 Wails CLI 处理，源码构建/测试要求先生成前端嵌入资源；版本、PE 数值元数据、正式说明和资产摘要在发布门槛中核对。

## 发布证据

- M1 v0.5.0：工作流 37913769259，公开 EXE/SHA256SUMS、中文正文一致、匿名 provider 下载摘要验证通过，详见 docs/m1-acceptance.md。
- M2 v0.5.1：工作流 37922146571，公开 EXE/SHA256SUMS/CHANGELOG.md、正文与同标签说明一致、匿名下载摘要验证通过，详见 docs/m2-acceptance.md。
- M3 v0.6.0：版本提交 29a900a；本地发布脚本通过 npm ci/build、无缓存全量 Go 测试、vet 和 Windows 构建，M3 根包竞态通过，PE 数值版本 0.6.0.0，说明附件与源文件逐字一致。工作流 37928816102 构建/发布成功，稳定正式 Release 三附件齐全，正文一致；实际匿名 provider 检查、同标签日志、下载及 SHA256 验证通过，当前版本返回无更新，详见 docs/m3-acceptance.md。

P01–P16 的开发、测试、隔离界面、文档及 M1–M3 线上发布已完成。构建和验收证据对应上述任务范围；以下原生系统交互与明确延后功能的边界继续保留，不把这些项目声称为已完成实机验收。

## 验证边界

真实界面采用隔离 APPDATA 的 Wails server 与浏览器，测试实际服务及渲染交互；窗口路由/标题/托盘菜单回调和通知内容有自动化证据。Windows 原生托盘位置、系统通知横幅/点击、外部目录/终端弹窗、系统剪贴板和实际更新替换重启尚未进行原生交互验收，不能从浏览器或回调测试推断这些结果。签名、跨平台发布、GitHub 登录、clone、push、rebase、force 及冲突编辑器仍为计划明确延后范围。
