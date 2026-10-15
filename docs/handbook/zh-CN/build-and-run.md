# 构建与运行

[项目手册](../README.zh-CN.md) · [English](../en/build-and-run.md) · [故障排查](runtime-and-troubleshooting.md)

除非命令中先执行了 `cd`，下列命令都从仓库根目录运行。

## 环境要求

| 工具 | 仓库中可验证的要求 |
|---|---|
| Go | `go.mod` 声明 Go `1.26.0` |
| Node.js | `package-lock.json` 中的 Vite 7 要求 `^20.19.0` 或 `>=22.12.0` |
| npm | `wails.json` 与 frontend scripts 使用 npm |
| Wails CLI | 应用依赖 Wails `v2.15.0`，应使用匹配的 v2 CLI |
| Swift/Xcode 工具 | 仅构建 Apple Reminders 与 macOS Notifications Plugin 时需要 |
| `zip`、`codesign` | 官方 macOS Plugin 打包使用；Notifications 会执行 `codesign` |

本次验证环境为 Go `1.26.4`、Node `23.6.1`、npm `10.9.2`、Wails CLI `v2.15.0` 和 Apple Swift `6.2.3`。这些是验证版本，并不额外收紧仓库声明的约束。

## 获取仓库并安装依赖

```sh
git clone https://github.com/shadowonder/lum-harbor-notifier.git
cd lum-harbor-notifier
go mod download
cd frontend
npm install
```

`npm install` 是 `wails.json` 配置的安装命令；如需严格按 lockfile 做全新安装，也可使用 `npm ci`。

## 开发运行方式

运行完整桌面应用并启用 frontend 热更新：

```sh
wails dev
```

Wails 会启动 Go 应用和配置好的 Vite watcher（`npm run dev`），并提供 CLI 描述的开发服务，通常为 `http://localhost:34115`。

只启动 Vite frontend：

```sh
cd frontend
npm run dev
```

独立 Vite 适合纯前端工作，但原生 Wails 调用依赖 Wails 开发运行时；端到端行为应使用 `wails dev`。

## 正式构建

```sh
wails build
```

macOS 产物路径为：

```text
build/bin/lum-harbor-notifier.app
```

怀疑生成文件或缓存过期时执行干净构建：

```sh
wails build -clean
```

`build/` 产物可丢弃且默认被 Git 忽略，唯一例外是受维护的 `build/darwin/Info.plist`，其中包含应用权限说明。macOS 重建会改变 ad-hoc 签名，因此系统可能再次请求 Reminders 权限。

## Frontend 命令

```sh
cd frontend
npm test
npx tsc --noEmit
npm run lint
npm run build
```

- `npm test`：运行基于 Node 的 TypeScript 测试。
- `npx tsc --noEmit`：只做类型检查，不写出 JavaScript。
- `npm run lint`：运行 ESLint。
- `npm run build`：执行 `tsc && vite build` 并生成正式 frontend bundle。

## Backend 命令

```sh
go test ./...
go vet ./...
go build ./...
go test -race ./...
```

Race 检查适用于 Scheduler、Plugin process 和 application runtime 的并发路径，但耗时更长。完整 Go 命令应顺序运行；高负载下并行跑多个全量套件，可能导致无关的 Plugin handshake 超时。

仓库还提供格式化与组合检查：

```sh
make fmt
make lint
make check
```

`make fmt` 会修改 Go/frontend 文件，仅在确实需要格式化时运行。`make check` 不改文件，会检查 Go lint、frontend lint 和 Prettier。

## 生成绑定

数据库查询绑定：

```sh
sqlc generate
```

Wails Go/TypeScript 绑定：

```sh
wails generate module
```

只有在 sqlc 查询/Schema 或导出的 Wails `App` contract 发生变化后才需要生成。仓库规则要求：如果 Wails CLI 改写了其 vendored runtime，应恢复 `frontend/wailsjs/runtime`；请审查生成差异，禁止手工编辑生成文件。

## Plugin 构建

官方 Plugin 源码与此应用仓库分开维护。请在相应 Plugin 的独立源码目录中，按照其 README 和 Makefile 构建与打包。输出包括标准 `dist/<plugin-id>.lhplugin` 与带版本的归档。包内容以及如何针对本 Host 进行兼容性测试见 [Plugin 开发指南](plugin-development.md)。

## Windows 检查

CGO-free Go 后端支持交叉编译与 vet：

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go vet ./...
```

这些命令不能替代原生 Windows 桌面验收；仍需真实 Windows 主机。Windows Plugin 兼容性检查还需要单独维护的 Plugin 源码或显式提供的兼容产物。

## 干净构建语义

- `wails build -clean` 会在打包前清理 Wails 中间产物。
- 每个官方 Plugin 支持 `make clean`；随后运行 `make package` 可获得干净包。
- 构建过程不得删除或改写用户应用数据。验收时使用隔离的新用户环境，或只重建明确可丢弃的预发布数据库。

## 当前 checkout 验证说明

上述命令仍是仓库的标准命令。在 commit 前工程回传验证中，正常本地构建环境（配有隔离的可写用户环境）下运行 `wails build -clean` 完整成功构建出已自签名的桌面应用产物（`build/bin/lum-harbor-notifier.app`）。手册审计中出现的 `_OBJC_CLASS_$_UTType` 错误未在正常构建环境中复现，归类为环境/构建上下文差异；无需对生产代码或链接器配置做任何修改。
