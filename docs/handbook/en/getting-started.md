# Getting Started

[Handbook](../README.md) · [中文](../zh-CN/getting-started.md)

## What Lum Harbor is

Lum Harbor is a local-first desktop cockpit for long-running WorkItems, daily Todos, external evidence, AI-assisted Proposals, reminders, notifications, and explainable History. Authoritative product state lives in a local SQLite database. Plugins observe external systems or perform bounded side effects; AI can read and propose, but validated Go domain services own changes.

## Repository map

```text
lum-harbor-notifier/
├── app/                          Wails application gateway
├── frontend/                     React/TypeScript interface
├── internal/                     Go application and domain packages
└── docs/handbook/                This project handbook
```

Official Plugin sources and packages are maintained separately from this standalone application repository.

## If you want to use the desktop app

1. Obtain a trusted Lum Harbor application bundle and launch it.
2. Open **Settings → AI** if you want Assistant and semantic Source interpretation.
3. Open **Settings → Extensions** to install any trusted `.lhplugin` packages.
4. Restart after installation, create the required Plugin Instance, then restart again.
5. Begin with the [Product Guide](product-guide.md).

The base product works without optional Plugins. AI features require either the built-in OpenAI-compatible provider or an enabled `ai.provider` Plugin Instance.

## If you want to run from source

The checked-in application currently requires Go `1.26.0`, Node.js compatible with Vite 7 (`^20.19.0` or `>=22.12.0`), npm, and Wails CLI `v2.15.0`. Building the two native macOS Plugins additionally requires Apple Swift/Xcode command-line tools. Follow [Build and Run](build-and-run.md); do not rely on an old pre-release database for verification.

## Platform expectations

- The main Go application has real macOS development and product acceptance evidence.
- The Go backend and reference Plugin have Windows cross-build checks, but native Windows desktop runtime remains unverified.
- Apple Reminders and macOS Notifications Plugins are `darwin/arm64` implementations and require macOS permissions.
- Secure built-in OpenAI credentials are supported through macOS Keychain or Windows Credential Manager; the macOS UI also offers a weaker application-managed fallback file store.

## Next

- Product operation: [Product Guide](product-guide.md)
- Source builds: [Build and Run](build-and-run.md)
- Extension lifecycle: [Plugin Usage](plugin-usage.md)
- Extension authoring: [Plugin Development](plugin-development.md)
- Failures and file locations: [Runtime and Troubleshooting](runtime-and-troubleshooting.md)
