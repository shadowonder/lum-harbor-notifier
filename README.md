# Lum Harbor

Lum Harbor is a local-first work cockpit for long-running WorkItems, daily planning, external evidence, AI-assisted proposals, reminders, notifications, and explainable History.

Start with the bilingual Project Handbook: [English](../docs/handbook/README.md) · [中文](../docs/handbook/README.zh-CN.md).

Contributor shortcuts: [Build & Run](../docs/handbook/en/build-and-run.md) · [Plugin Development](../docs/handbook/en/plugin-development.md).

## Live Development

To run in live development mode, run `wails dev` in the project directory. This will run a Vite development
server that will provide very fast hot reload of your frontend changes. If you want to develop in a browser
and have access to your Go methods, there is also a dev server that runs on http://localhost:34115. Connect
to this in your browser, and you can call your Go code from devtools.

## Building

To build a redistributable, production mode package, use `wails build`.

For real product verification, use the production bundle with a fresh isolated user data directory. Do not use an old development database: before the first public release, the single foundation migration is intentionally evolved in place.
