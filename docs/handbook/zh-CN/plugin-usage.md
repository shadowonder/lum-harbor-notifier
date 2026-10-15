# Plugin 使用指南

[项目手册](../README.zh-CN.md) · [English](../en/plugin-usage.md) · [Plugin 开发指南](plugin-development.md)

## Plugin、Capability、Binding 与 Instance

- **Plugin（Extension）**：一个已安装的软件包与可执行程序身份。
- **Capability**：Plugin 能做什么，例如 `source.fetch` 或 `notification.deliver`。
- **Binding**：Host 在何处或何时使用该 Capability，例如 `SCHEDULE` 或 `AI_PROVIDER`。
- **Plugin Instance**：对某个 Capability 的一次具体配置。Instance 有稳定 ID 和独立配置；同一 Extension 的多个已启用 Instance 共享一个进程。

“已安装”不等于“已配置”“已启用”或“正在运行”。应用启动时，只有至少被一个已启用 Instance 引用的 Extension 才会启动，并且每个 Extension 只启动一个进程。

## `.lhplugin` 是什么

`.lhplugin` 是标准 ZIP 归档。根目录必须包含 `plugin.json`，以及 manifest 的 `executable` 所指向的二进制或应用 bundle。Lum Harbor 先解压到 staging 目录，拒绝危险路径、symlink、重复条目、不兼容的平台元数据、超限归档和无效 manifest，再原子发布安装目录。

只能安装来源可信的包。V1 会验证结构和兼容性，但没有包签名、沙箱、marketplace 或远程信任管理。

## 完整生命周期

```text
构建/打包 → Add Plugin → 重启/发现 → Add Instance
→ 选择 Capability/配置 → 重启 → Runtime 启动 Plugin
→ Stop Using / Use Again → 重启 → Remove Plugin
```

1. 获取或构建 `dist/<plugin-id>.lhplugin`。
2. 打开 **Settings → Extensions → Add Plugin** 并选择该文件。
3. 重启；启动阶段的 discovery 才会发现新包。
4. 打开 Extension，选择 **Add Instance**。
5. 选择 Capability，并填写 schema 描述的非 secret 配置。
6. 再次重启；Lum Harbor 解析已启用 Instance，为每个被选中的 Extension 启动一次进程，再注册对应 Host workload。
7. **Stop Using** 只会让 Instance 在下次启动时禁用；**Use Again** 会在下次启动时恢复同一个 ID 与配置。
8. 移除前必须停用该 Plugin 的所有 Instance，重启以确保共享进程未运行，再选择 **Remove Plugin**。

所有生命周期变更都以重启为边界。Inventory 可以立即显示期望的启用/禁用状态，但当前 Registry、Plugin process 与 Scheduler 在重启前不会变化。V1 没有热加载或 **Restart Now**。

Remove 会删除已安装程序并解除 Instance 声明，但会保留 Plugin data 与 cache；V1 没有清理按钮。

## 官方 Plugin

| Plugin | Capability | 用途 |
|---|---|---|
| `lh-plugin-ai-runtime` | `ai.provider` | 替代内置 Provider，统一服务 Assistant 与 Source Processing；支持 OpenAI-compatible endpoint 和确定性 offline/mock 模式。 |
| `lh-plugin-apple-reminders` | `source.fetch`，300 秒 `SCHEDULE` | 读取 EventKit Reminders，并确定性同步成 EXTERNAL Planner item；只读，不回写 Apple Reminders。 |
| `lh-plugin-macos-notifications` | `notification.deliver` | 把 Core 已决定发送的 Notification 投递到 macOS Notification Center，并做重复抑制。 |
| `lh-plugin-synthetic-project` | `source.fetch`、`read.query`，60 秒 schedule | 模拟持续变化的项目事实和受限查询，用于演示与端到端验证。 |

外部通知需要唯一的副作用 owner，因此最多只能启用一个 `notification.deliver` Instance。应用也只允许一个有效 AI Provider。

## Capability 与权限语义

- `source.fetch` 只观察外部事实；调度、Source 持久化、去重和后续处理由 Host/Core 管理。
- `read.query` 为 Assistant 提供受限 summary/facts；query 是不透明文本，不是 SQL。
- `ai.provider` 接管全局 AI 边界；Plugin 失败时不会静默回退到内置 Provider。
- `notification.deliver` 只在 Core 已做出业务决定后执行外部副作用。

Apple Reminders 需要 macOS Reminders 权限。若权限被拒绝，或首次提示停留时间超过 Host request timeout，请在 System Settings 中修正后重启；V1 不会自动重启 Plugin process。

macOS Notifications 需要为已安装的 notification bridge bundle 授权。拒绝授权会返回失败，不会伪造成功投递标记；应用内 Notification 仍然独立存在。

## 数据与缓存持久化

Host 以已安装目录为 Plugin 工作目录，并提供专属路径：

- 持久数据：`<ConfigDir>/lum_harbor/plugin-data/<extensionID>`；
- 缓存：`<ConfigDir>/lum_harbor/plugin-cache/<extensionID>`。

Synthetic Project 会把 `simulation.json` 写入 data 目录，使模拟世界跨重启保留。Plugin 不应在安装目录写运行数据，也绝不能定位或访问 Lum Harbor Core SQLite。

## 常见失败

| 现象 | 检查项 |
|---|---|
| 安装后看不到 Plugin | 重启；确认归档根目录有 `plugin.json`、平台/架构匹配，且没有同 ID 旧版本。 |
| Instance 显示 Enabled 但没有运行 | Instance 变更后必须重启；Enabled 是期望配置，不是活进程证明。 |
| 应用启动失败 | 已启用 Instance 可能引用缺失/无效 Extension、不支持的 Capability、无效配置，或 Plugin handshake 失败；查看 `lum-harbor.log`。 |
| Source 不更新 | 检查权限、schedule、启动/handshake 错误，以及 Source Processing 的 AI 健康；Fetch 与解释是不同阶段。 |
| 原生通知缺失 | 检查 `notification.deliver` Instance、重启、macOS 授权和 delivery error。 |
| Remove 不可用 | 停用全部 Instance，重启使共享进程停止，再移除。 |
| 重装后行为未重置 | Remove 会保留 data/cache；V1 没有产品内清理动作。 |

路径与深入诊断见[运行时与故障排查](runtime-and-troubleshooting.md)。
