# 快速开始

[项目手册](../README.zh-CN.md) · [English](../en/getting-started.md)

## Lum Harbor 是什么

Lum Harbor 是一款 local-first 桌面工作驾驶舱，用来统一管理长期 WorkItem、日常 Todo、外部事实、AI 辅助 Proposal、提醒、通知和可解释的 History。权威产品状态保存在本机 SQLite 中。Plugin 可以观察外部系统或执行受限的外部副作用；AI 可以读取信息并提出建议，但真正的状态变更必须由经过校验的 Go 领域服务完成。

## 仓库结构

```text
lum-harbor-notifier/
├── app/                           Wails 应用网关
├── frontend/                      React/TypeScript 界面
├── internal/                      Go 应用与领域包
└── docs/handbook/                 本项目手册
```

官方 Plugin 源码和安装包与此独立应用仓库分开维护。

## 如果你只是使用桌面应用

1. 从可信的发布者或维护者处取得 Lum Harbor 应用包并启动。
2. 如需 Assistant 与语义 Source 解释，打开 **Settings → AI**。
3. 在 **Settings → Extensions** 中安装可信的 `.lhplugin` 包。
4. 安装后重启，创建所需的 Plugin Instance，再重启一次。
5. 按[产品使用指南](product-guide.md)开始使用。

不安装可选 Plugin 也可以使用基础产品。AI 功能需要内置 OpenAI-compatible Provider，或一个已启用的 `ai.provider` Plugin Instance。

## 如果你要从源码运行

当前仓库要求 Go `1.26.0`、满足 Vite 7 要求的 Node.js（`^20.19.0` 或 `>=22.12.0`）、npm，以及 Wails CLI `v2.15.0`。构建两个 macOS 原生 Plugin 还需要 Apple Swift/Xcode Command Line Tools。请继续阅读[构建与运行](build-and-run.md)，验证时不要复用旧的预发布数据库。

## 最低平台预期

- 主应用已有真实 macOS 开发与产品验收记录。
- Go 后端和参考 Plugin 可执行 Windows 交叉构建检查，但原生 Windows 桌面运行尚未验证。
- Apple Reminders 与 macOS Notifications Plugin 是 `darwin/arm64` 实现，并依赖 macOS 权限。
- 内置 OpenAI 凭据可由 macOS Keychain 或 Windows Credential Manager 保存；macOS UI 也提供安全性较弱的应用自管文件存储作为显式备选。

## 接下来读什么

- 产品操作：[产品使用指南](product-guide.md)
- 源码构建：[构建与运行](build-and-run.md)
- 扩展生命周期：[Plugin 使用指南](plugin-usage.md)
- 扩展开发：[Plugin 开发指南](plugin-development.md)
- 文件位置与排障：[运行时与故障排查](runtime-and-troubleshooting.md)
