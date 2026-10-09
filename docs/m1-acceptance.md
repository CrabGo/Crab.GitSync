# M1 可靠同步验收记录

日期：2026-10-09。测试仅使用临时仓库、临时配置和本地网络服务。公开发布检查及下载在 P08 执行。

| 场景 | 证据 | 验证范围 |
| --- | --- | --- |
| 代理端口不可达、协议不匹配、连接正常、诊断取消 | `internal/diagnostics/diagnostics_test.go`；P02 浏览器验收 | 默认 TCP/HTTP/Git 探针经本地代理；端口成功不能代表协议正确；取消不派发后续探针 |
| GitHub 请求超时、DNS、认证、TLS、未知错误 | `internal/taskresult/error_test.go`；诊断独立步骤测试 | 类型及 stderr 分类；未知错误不可自动重试；HTTP/代理测试均隔离 |
| 混合成功/失败/无远端、仅重试失败 | `m1integration_test.go`、`retryfailed_test.go`；P03 界面验收 | 真实本地 Git 远端及失败路径；排除成功、跳过、取消；来源结果保留 |
| 自动重试上限、默认关闭、取消等待 | `autoretry_test.go`；P04 浏览器验收 | 最大 3 次实际尝试，2/5 秒退避；认证、TLS、仓库状态等不自动重试 |
| 网络成功但本地刷新失败 | `taskresults_test.go`、`fetchtime_test.go` | 独立阶段及警告；手动重试只读取本地信息；不重复网络获取 |
| 已同步/领先/落后/分叉/无 upstream/未知 | `internal/gitengine/syncstatus_test.go`；P05 界面验收 | 真实临时引用；空仓库、分离 HEAD、跟踪引用缺失；工作区单独显示 |
| 完整成功 fetch 的时间 | `fetchtime_test.go`；P05 界面验收 | 当前会话有效；重扫保留；失败、部分成功及本地刷新不推进时间 |
| 快进、分叉拒绝、普通合并、冲突、中止 | `internal/gitengine/merge_test.go`、`actions_test.go`；P06 界面验收 | 默认仅快进；普通合并需明确选择和勾选确认；冲突现场保留 |
| 陈旧确认、外部切换分支、脏工作区 | `merge_test.go`、`mergeconfirmation_test.go` | 执行前及 fetch 后重验；普通合并目标变化需重新确认；按提交 ID 合并 |
| 批量 fetch 与右键拉取安全 | `m1integration_test.go`、`git_test.go` | 实际任务 API 前后 HEAD、已跟踪内容、新文件不变，远端跟踪引用更新 |
| 连续检查更新及失败后再检查 | `updateservice_test.go`；P07 浏览器三次检查 | 更新器只初始化一次，忙碌按钮禁用，失败详情可展开；未调用 Restart |
| 保存代理后生效 | `internal/updatefeed/proxy_test.go`、`internal/gitengine/proxy_test.go` | 后续 HTTP 读取当前代理；Git 固定任务代理，不修改持久 Git 配置 |
| 页面标题、桌面路由、通知结果 | `desktopservice_test.go`；P07 独立页面切换 | 五个路由的标题/恢复/聚焦/页面切换顺序；任务汇总通知数据 |

## 验证边界

- 浏览器验收连接真实 Wails server 服务，使用隔离 APPDATA。P02–P06 已记录逐项截图；P07 核对独立说明页和重复更新失败界面。
- 桌面路由行为用窗口替身验证，Windows 原生可执行文件构建通过。托盘创建、菜单、关闭隐藏、恢复及通知响应路径已核对源码，接口仍调用原有 Wails 原生能力。
- 当前工具不提供原生桌面交互。没有声称验证 Windows 托盘弹出位置、通知横幅、通知点击或真实更新替换重启；这些视觉和系统集成检查不能由浏览器或替身测试替代。
- 没有对开发者主应用调用更新 Restart，没有改动开发者或用户仓库。更新下载及 SHA256 用本地测试服务器验证，线上资产检查另见 P08 记录。
- 知识图谱 MCP 工具当前不可用，核对基于上述源码与实际运行结果，未声称图谱覆盖完整。

## 集成阶段修正

- 诊断测试隔离系统/全局 Git 配置，避免本机凭据助手介入本地 401 场景。
- 错误 URL 脱敏保留外层引号，避免将日志标点误编码到地址中；Git 和 HTTP 共用脱敏逻辑。
- 无效桌面页面路由不恢复窗口或执行 JavaScript，窗口尚未创建时安全返回。

## P08 线上发布结果

v0.5.0 已正式发布：[发布页面](https://github.com/CrabGo/Crab.GitSync/releases/tag/v0.5.0)，[构建流程](https://github.com/CrabGo/Crab.GitSync/actions/runs/37913769259) 成功。发布正文与 `releases/v0.5.0.md` 完全一致。公开 provider 无凭据识别新版、下载 EXE，实际 SHA256 与线上 SHA256SUMS 一致：`fd2d6902c663ac3ffe4ba0d256ce8cc7354176a2bcbfac9e57e3645b77ade96f`。在线 EXE 数值文件版本为 0.5.0.0；当前版本检查返回无更新，没有执行安装替换或 Restart。
