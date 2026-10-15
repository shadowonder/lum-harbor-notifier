# Plugin Usage

[Handbook](../README.md) · [中文](../zh-CN/plugin-usage.md) · [Plugin Development](plugin-development.md)

## Plugin, Capability, Binding, and Instance

- A **Plugin** (extension) is one installed package and executable identity.
- A **Capability** says what it can do, such as `source.fetch` or `notification.deliver`.
- A **Binding** says where or when the Host uses that capability, such as `SCHEDULE` or `AI_PROVIDER`.
- A **Plugin Instance** is one configured use of one capability. Instances have stable IDs and separate configuration, while enabled instances of the same extension share one process.

Installed does not mean configured, enabled, or running. Lum Harbor starts one process only for each extension referenced by at least one enabled Instance at application startup.

## What `.lhplugin` means

A `.lhplugin` is a standard ZIP archive whose root contains `plugin.json` and the manifest-declared executable or application bundle. Lum Harbor extracts it into a staging directory, rejects unsafe paths, symlinks, duplicates, incompatible package metadata, oversized archives, and invalid manifests, then atomically publishes the installed directory.

Only install packages from sources you trust. V1 validates structure and compatibility but does not provide package signing, sandboxing, a marketplace, or remote trust management.

## Complete lifecycle

```text
build/package → Add Plugin → restart/discovery → Add Instance
→ choose Capability/configuration → restart → runtime starts Plugin
→ Stop Using / Use Again → restart → Remove Plugin
```

1. Obtain or build `dist/<plugin-id>.lhplugin`.
2. Open **Settings → Extensions → Add Plugin** and select the file.
3. Restart. Startup discovery now sees the installed package.
4. Open the extension and choose **Add Instance**.
5. Choose a Capability and enter the schema-described non-secret configuration.
6. Restart again. Lum Harbor resolves enabled Instances, starts each selected extension once, then registers the relevant Host workloads.
7. **Stop Using** disables an Instance for the next start; **Use Again** re-enables the same ID and configuration for the next start.
8. To remove a Plugin, stop every Instance, restart so no process is active, then choose **Remove Plugin**.

Every lifecycle mutation is restart-bound. Inventory may immediately show desired enabled/disabled state, but the live Registry, Plugin processes, and Scheduler remain unchanged until restart. V1 has no hot reload or **Restart Now** action.

Removal deletes the installed program artifact and detaches its Instance declarations. It deliberately preserves both Plugin data and cache directories; V1 has no cleanup button.

## Official Plugins

| Plugin | Capability | Use |
|---|---|---|
| `lh-plugin-ai-runtime` | `ai.provider` | Replaces the built-in provider for Assistant and Source Processing. Supports OpenAI-compatible endpoints and deterministic offline/mock behavior. |
| `lh-plugin-apple-reminders` | `source.fetch` with a 300-second `SCHEDULE` binding | Reads EventKit Reminders and deterministically reconciles them into EXTERNAL Planner items. Read-only; it never writes back to Apple Reminders. |
| `lh-plugin-macos-notifications` | `notification.deliver` | Delivers already-decided Core Notifications to macOS Notification Center with duplicate suppression. |
| `lh-plugin-synthetic-project` | `source.fetch`, `read.query`; 60-second schedule | Simulates evolving project facts and bounded read queries. Useful for demonstration and end-to-end verification. |

At most one enabled `notification.deliver` Instance is allowed because external delivery needs one unambiguous side-effect owner. The application also enforces a single effective AI provider.

## Capability and permission behavior

- `source.fetch` observes external facts. The Host owns scheduling, Source persistence, deduplication, and any later deterministic or AI processing.
- `read.query` returns a bounded summary and fact list for Assistant reads; its query is opaque text, never SQL.
- `ai.provider` serves the global AI boundary. A Plugin failure does not silently fall back to the built-in provider.
- `notification.deliver` performs only the external side effect after Core has already decided a Notification should be delivered.

Apple Reminders requires macOS Reminders access. If the prompt is denied or remains open longer than the Host request timeout, correct the permission in System Settings and restart; V1 does not automatically restart the process.

macOS Notifications requires notification authorization for the installed notification bridge bundle. A denied authorization returns failure and does not create a false successful-delivery marker. Lum Harbor's in-app Notification still exists independently.

## Plugin state and persistence

The Host launches a Plugin with its working directory set to the installed package and provides dedicated paths:

- persistent data: `<ConfigDir>/lum_harbor/plugin-data/<extensionID>`;
- cache: `<ConfigDir>/lum_harbor/plugin-cache/<extensionID>`.

The Synthetic Project Plugin stores `simulation.json` in its data directory so the simulated world survives restart. Plugins must not write runtime state into the installation directory and must never locate or access Lum Harbor's Core SQLite database.

## Common failures

| Symptom | Check |
|---|---|
| Installed package does not appear | Restart; confirm the archive has root `plugin.json`, matches the current OS/architecture, and was not already installed. |
| Instance appears enabled but nothing runs | Restart after the Instance change; enabled is desired state, not proof of a live process. |
| Application fails during startup | An enabled Instance may reference a missing/invalid extension, unsupported Capability, invalid config, or a Plugin that failed handshake. Check `lum-harbor.log`. |
| Source does not update | Check Plugin permission, schedule, startup/handshake errors, and Source-processing AI health. Fetch and interpretation are separate stages. |
| Native notification is missing | Check the enabled `notification.deliver` Instance, restart, macOS authorization, and delivery errors. |
| Remove is unavailable | Stop every Instance, restart to stop the shared extension process, then remove. |
| Reinstall does not reset behavior | Removal preserves data and cache; there is no in-product cleanup action in V1. |

See [Runtime and Troubleshooting](runtime-and-troubleshooting.md) for paths and deeper diagnostics.
