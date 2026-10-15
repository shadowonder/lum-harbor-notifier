# 运行时与故障排查

[项目手册](../README.zh-CN.md) · [English](../en/runtime-and-troubleshooting.md)

## 运行时位置

Lum Harbor 使用操作系统的用户配置目录，并在其下创建 `lum_harbor`：

```text
<UserConfigDir>/lum_harbor/
├── config.json
├── lum_harbor.db
├── lum_harbor.db-wal / lum_harbor.db-shm   SQLite WAL 活跃时存在
├── logs/lum-harbor.log
├── plugins/<extensionID>/
├── plugin-data/<extensionID>/
├── plugin-cache/<extensionID>/
└── credentials/builtin-openai-credential.secret  仅 macOS fallback store
```

macOS 上 `<UserConfigDir>` 通常是 `~/Library/Application Support`；Windows 上是当前用户的配置/AppData 位置。应以以上符号结构为准，具体平台路径由 Go 解析。

- `config.json` 保存下次启动的期望配置。文件不存在时使用默认值；malformed、未知字段、尾随 JSON 或非法值都会导致启动失败。
- `lum_harbor.db` 保存权威产品真值与运行历史。
- 日志按 `config.json` 轮转；默认文件为 `logs/lum-harbor.log`，1 MB、5 个备份、保留 7 天。
- `plugins/` 是受管理的安装代码；`plugin-data/` 与 `plugin-cache/` 由 Plugin 使用，并在卸载后保留。
- 内置 OpenAI credential 不在 `config.json` 中：macOS 默认使用 Keychain，Windows 使用 Windows Credential Manager；macOS 也可显式选择私有目录中的 `0600` fallback 文件。Plugin secret persistence 尚未实现。

公开提交 issue 前，不要直接粘贴数据库、配置、credential 或日志内容；必须移除个人数据、路径、prompt、Provider response 与 secret。

## 为什么需要重启

Lum Harbor 只在启动时加载 Runtime Config、发现 Plugin、解析已启用 Instance 并组合 runtime。安装/移除 Plugin、添加/配置/禁用/重新启用 Instance、切换有效 Provider 或修改 next-start Provider 配置后，都必须完整退出并重新打开；没有 hot reload。

## 普通用户排障

### AI Provider 不可用或 Assistant 失败

1. 打开 **Settings → AI**，查看有效 Provider 与 readiness。
2. 内置 OpenAI：确认 endpoint、model、timeout、credential 状态，保存后重启。
3. `lh-plugin-ai-runtime`：确认 Instance 已启用，重启，并在日志中查找启动/RPC 错误。
4. 使用 **Verify AI Runtime**。Provider 错误会 fail closed：不会 Apply Proposal，也不会修改权威状态。
5. 本地 Ollama 可能耗时数分钟；确认 OpenAI-compatible endpoint（常见为 `http://127.0.0.1:11434/v1`）和精确 model name。

相对日期受模型质量影响。若 Proposal 日期错误，请 Reject，并用绝对日期、精确时间和 IANA timezone 重新描述。

### Plugin 不启动或 RPC 失败

- 每次生命周期或配置变更后重启。
- 检查 manifest、executable、OS/arch、Instance Capability 与 schema validation error。
- Plugin stdout 必须只有协议，日志写 stderr。
- Malformed response、未知/重复 response ID、同时返回 result 与 error 会使进程被终止，并让 pending call 失败。
- Timeout 不会远程取消 Plugin 工作；迟到 response 只会在 tombstone 仍保留时被安全丢弃。
- V1 不会自动重启退出或 unhealthy 的 Plugin process。

### Apple Reminders 不同步

- 确认 `source.fetch` Instance 已启用并重启。
- 在 macOS System Settings 中给负责运行的 Lum Harbor 应用身份授予 Reminders 权限。
- 首次权限对话框可能超过默认 30 秒 request timeout；做出选择后重启。
- Fetch 默认每 300 秒运行；reconciliation 是另一阶段，因此更新不是即时的。
- EXTERNAL Planner item 在 Lum Harbor 中只读；后续 fetch 缺失也不会删除它。

重建应用会改变 ad-hoc signature，可能使权限重置为 `notDetermined`；重建后再次弹窗是预期行为。

### macOS 原生通知未出现

- 确认恰好一个 `notification.deliver` Instance 已启用并重启。
- 在 macOS Notification settings 中允许 notification bridge。
- 查看日志中的 delivery error。拒绝授权会返回错误，不会写入虚假的外部成功标记。
- 应用内 Notification、toast 与 unread 状态独立于外部投递。

### Source Processing 失败

抓取事实与语义解释是两个阶段。到 History 查找 Source observation 与 AI Run 结果。`FAILED` 的语义 Source 不会自动重试；修正 Provider 后使用显式 **Interpret**。若已有 pending Proposal，系统可能有意抑制新解释，直到用户 Apply 或 Reject。

### Proposal 变成 STALE

说明 Proposal 创建后目标真值发生了变化。STALE 是安全结果，不是数据丢失。先查看当前对象，再请求新的 Proposal。

## 开发者/运维排障

### 过期的预发布数据库

首次公开发布前，项目会就地修改唯一的 `00001_foundation.sql`。旧数据库可能显示 Goose version 1，却缺少新的表或约束；系统不会自动修复。验收时使用全新隔离用户环境，或只重建明确可丢弃的开发数据库；不要把删除真实用户数据库当作常规修复。

`wails build` 的 binding generation 可能在当前用户环境中初始化应用。验收时应隔离环境，并在启动前确认预期数据库路径。

### Wails/frontend 构建不一致

- 先运行 `npm run build` 暴露 TypeScript 错误。
- 只有导出 Go App contract 变化后才运行 `wails generate module`。
- 审查生成 diff；若 CLI 改写其 vendored runtime，则恢复 `frontend/wailsjs/runtime`。
- 怀疑缓存产物时使用 `wails build -clean`。

### 本地 AI 很慢或负载过高

Assistant 与后台 Source Processing 共用有效 `ai.Service`。本地模型会让请求排队或变慢，这不一定是死锁。查看最终 AI Run History 与 Provider 日志。负载较高时不要并发运行多个完整 Go suite 和 Plugin acceptance，否则 handshake test 可能超时。

### 日志与安全诊断

可临时在 `config.json` 中把 `logging.level` 设为 `debug`，重启、复现一次，并查看 `logs/lum-harbor.log`，随后恢复正常级别。Plugin 禁止把 protocol JSON 之外的内容写 stdout，也禁止记录 credential 或完整私有 payload。

## 当前 V1 限制

- Workflow 没有 lifecycle/completion state；没有 recurring Todo 或 recurring Workflow action engine。
- Planner 没有通用 edit/archive/delete/reopen UI，也没有通用 calendar-event 创建。
- Plugin 没有 hot reload、**Restart Now**、marketplace、签名、自动更新或原地替换。
- 没有 Plugin credential store、自动进程重启或通用 Plugin→Host callback API。
- 没有 data/cache 清理功能；卸载会保留两者。
- 没有 Backup/Restore。
- 失败的语义 Source 没有自动 retry/backoff。
- MonitorPoint 不能编辑、改期、重新激活或自动标为 satisfied。
- Dashboard 与 runtime health 仍不完整；没有 System runtime page 或 **Run Now**。
- 通用外部项目 Source 不会自动映射成 WorkItem/Workflow/Tracked Item。
- 原生 Windows 桌面 runtime 尚未通过产品验证。

这些是当前边界，不代表路线图承诺。
