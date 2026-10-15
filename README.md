# Lum Harbor

Lum Harbor is a local-first work cockpit for long-running WorkItems, daily planning, external evidence, AI-assisted proposals, reminders, notifications, and explainable History.

## Documentation

- [Documentation index](docs/README.md)
- Project Handbook: [English](docs/handbook/README.md) · [中文](docs/handbook/README.zh-CN.md)
- Product Guide: [English](docs/handbook/en/product-guide.md) · [中文](docs/handbook/zh-CN/product-guide.md)
- Contributor guides: [Build & Run](docs/handbook/en/build-and-run.md) · [Plugin Development](docs/handbook/en/plugin-development.md)

This repository keeps only public, README-level product and contributor documentation. Internal development-state, contract, audit, and implementation-report documents are maintained separately and are not duplicated here.

## Live Development

To run in live development mode, run `wails dev` in the project directory. This will run a Vite development
server that will provide very fast hot reload of your frontend changes. If you want to develop in a browser
and have access to your Go methods, there is also a dev server that runs on http://localhost:34115. Connect
to this in your browser, and you can call your Go code from devtools.

## Building

To build a redistributable, production mode package, use `wails build`.

## Manual releases

The **Build and release** GitHub Actions workflow creates macOS arm64 and Windows x64 release files, checksums, a new Git tag, and a GitHub Release. Run it from the repository's **Actions** tab with **Run workflow**.

You may enter a semantic version such as `1.0.0` or `v1.0.0`. If the version is left blank, the workflow assigns a unique build version. The workflow rejects an existing tag and creates the tag and release only after verification and both platform builds succeed.

The current workflow produces a Wails self-signed macOS bundle and unsigned Windows binaries. Apple notarization and trusted Windows code signing require distribution credentials and are not performed by this workflow.

For real product verification, use the production bundle with a fresh isolated user data directory. Do not use an old development database: before the first public release, the single foundation migration is intentionally evolved in place.
