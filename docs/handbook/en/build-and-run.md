# Build and Run

[Handbook](../README.md) · [中文](../zh-CN/build-and-run.md) · [Troubleshooting](runtime-and-troubleshooting.md)

Run all commands from the repository root unless a command starts with `cd`.

## Prerequisites

| Tool | Verified project requirement |
|---|---|
| Go | `go.mod` declares Go `1.26.0` |
| Node.js | Vite 7 in `package-lock.json` requires `^20.19.0` or `>=22.12.0` |
| npm | Required by `wails.json` and the frontend scripts |
| Wails CLI | Application module uses Wails `v2.15.0`; use a matching v2 CLI |
| Swift/Xcode tools | Required only for the Apple Reminders and macOS Notifications Plugins |
| `zip`, `codesign` | Used by official macOS Plugin package targets; `codesign` is used by Notifications |

This checkout was verified with Go `1.26.4`, Node `23.6.1`, npm `10.9.2`, Wails CLI `v2.15.0`, and Apple Swift `6.2.3`. These are verification versions, not extra constraints beyond the repository declarations above.

## Open the repository and install dependencies

```sh
git clone https://github.com/shadowonder/lum-harbor-notifier.git
cd lum-harbor-notifier
go mod download
cd frontend
npm install
```

`npm install` is the command configured in `wails.json`; the checked-in lockfile also permits `npm ci` when an exact clean install is preferred.

## Development modes

Run the complete desktop application with frontend hot reload:

```sh
wails dev
```

Wails starts the Go application and the configured Vite watcher (`npm run dev`). It also exposes the Wails development server described by the CLI, normally at `http://localhost:34115`.

Run only the Vite frontend:

```sh
cd frontend
npm run dev
```

Standalone Vite is useful for frontend work, but native Wails calls require the Wails development runtime; use `wails dev` for end-to-end behavior.

## Production build

```sh
wails build
```

On macOS, this project produces:

```text
build/bin/lum-harbor-notifier.app
```

Use a clean Wails build when generated or cached output may be stale:

```sh
wails build -clean
```

`build/` output is disposable and Git-ignored except for the maintained `build/darwin/Info.plist`, which contains the application permission descriptions. A macOS rebuild changes the ad-hoc signature and can cause Reminders permission to be requested again.

## Frontend commands

```sh
cd frontend
npm test
npx tsc --noEmit
npm run lint
npm run build
```

- `npm test` runs the Node-based TypeScript test suite.
- `npx tsc --noEmit` performs an explicit typecheck without writing JavaScript.
- `npm run lint` runs ESLint.
- `npm run build` runs `tsc && vite build` and writes the production frontend bundle.

## Backend commands

```sh
go test ./...
go vet ./...
go build ./...
go test -race ./...
```

The race run is relevant to scheduler, Plugin process, and application-runtime concurrency but is slower. Run full Go commands sequentially: heavily parallel full-suite runs can cause unrelated Plugin handshake timeouts on a loaded machine.

The repository formatting and combined static checks are:

```sh
make fmt
make lint
make check
```

`make fmt` modifies Go/frontend files; use it only when formatting changes are intended. `make check` is read-only and runs Go lint, frontend lint, and Prettier checking.

## Generated bindings

Database query bindings:

```sh
sqlc generate
```

Wails Go/TypeScript bindings:

```sh
wails generate module
```

Generation is required after changing sqlc queries/schema or the exported Wails `App` contract. The repository policy is to restore `frontend/wailsjs/runtime` after Wails generation if the CLI rewrites its vendored runtime; review the generated diff and never hand-edit generated bindings.

## Plugin builds

Official Plugin sources are maintained separately from this application repository. Build and package a Plugin from its own source checkout using its README and Makefile. Packages are written as canonical `dist/<plugin-id>.lhplugin` files plus versioned archives. See [Plugin Development](plugin-development.md) for package contents and compatibility testing against this Host.

## Windows checks

The repository supports cross-compiling and vetting the CGO-free Go backend:

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go vet ./...
```

These commands do not establish native Windows desktop acceptance; a Windows host is still required for that. Windows Plugin compatibility checks additionally require the separately maintained Plugin source or an explicit compatible artifact.

## Clean builds

- `wails build -clean` clears Wails build intermediates before packaging.
- Each official Plugin supports `make clean`; follow it with `make package` for a clean package.
- Do not delete or rewrite a user's application data directory as part of a build. For acceptance, use a fresh isolated user environment or deliberately recreate only a disposable pre-release database.

## Current checkout verification note

The commands above are the repository's canonical commands. In the pre-commit engineering pass, `wails build -clean` succeeded cleanly in the normal local build environment (with an isolated writable user environment), producing the self-signed desktop application bundle (`build/bin/lum-harbor-notifier.app`). The earlier `_OBJC_CLASS_$_UTType` failure reported during the handbook finalization audit was not reproduced and is classified as an environment/build-context discrepancy; zero production code or linker configuration changes were required.
