# Runtime and Troubleshooting

[Handbook](../README.md) · [中文](../zh-CN/runtime-and-troubleshooting.md)

## Runtime locations

Lum Harbor resolves the operating system's user configuration directory and appends `lum_harbor`:

```text
<UserConfigDir>/lum_harbor/
├── config.json
├── lum_harbor.db
├── lum_harbor.db-wal / lum_harbor.db-shm   while SQLite WAL is active
├── logs/lum-harbor.log
├── plugins/<extensionID>/
├── plugin-data/<extensionID>/
├── plugin-cache/<extensionID>/
└── credentials/builtin-openai-credential.secret  macOS fallback store only
```

On macOS, `<UserConfigDir>` normally resolves to `~/Library/Application Support`; on Windows it is the current user's configuration/AppData location. Treat the symbolic layout above as authoritative because Go resolves the platform path.

- `config.json` is desired next-start configuration. A missing file means defaults; malformed, unknown, trailing, or invalid values fail startup.
- `lum_harbor.db` is authoritative product truth and operational history.
- Logs rotate according to `config.json`; the default is `logs/lum-harbor.log`, 1 MB, five backups, seven days.
- `plugins/` is managed installed code. `plugin-data/` and `plugin-cache/` are Plugin-owned and survive uninstall.
- Built-in OpenAI credentials live outside `config.json`: macOS Keychain by default, Windows Credential Manager on Windows, or the explicitly selected macOS fallback file (`0600`) in a private directory. Plugin secret persistence is not implemented.

Do not paste database, config, credential, or log contents into a public issue without removing personal data, paths, prompts, provider responses, and secrets.

## Restart-required behavior

Lum Harbor loads Runtime Config, discovers Plugins, resolves enabled Instances, and composes the runtime once at startup. Installing/removing a Plugin, adding/configuring/disabling/re-enabling an Instance, changing the effective provider, or editing next-start provider settings requires a full quit and reopen. There is no hot reload.

## Normal-user troubleshooting

### AI provider is unavailable or Assistant fails

1. Open **Settings → AI** and read the effective provider and readiness state.
2. For built-in OpenAI, verify endpoint, model, timeout, and credential state, then restart.
3. For `lh-plugin-ai-runtime`, confirm the Instance is enabled, restart, and inspect Plugin startup/RPC errors in the log.
4. Use **Verify AI Runtime**. Provider errors fail closed: no Proposal Apply and no authoritative mutation occur.
5. Local Ollama may take minutes. Confirm its OpenAI-compatible endpoint (commonly `http://127.0.0.1:11434/v1`) and exact model name.

Relative dates are model-sensitive. Reject incorrect Proposals and retry with an absolute date, time, and IANA timezone.

### Plugin does not start or call fails

- Restart after every lifecycle/config change.
- Check manifest, executable, OS/architecture, Instance Capability, and schema validation errors.
- Keep Plugin stdout strictly protocol-only; send logs to stderr.
- A malformed response, unknown/duplicate response ID, or result-plus-error kills the process and fails pending requests.
- Timeouts do not remotely cancel Plugin work. A late response is discarded only while its request tombstone remains retained.
- V1 does not automatically restart an exited or unhealthy process.

### Apple Reminders does not sync

- Confirm an enabled `source.fetch` Instance and restart.
- Grant Reminders access to the responsible Lum Harbor application identity in macOS System Settings.
- The first permission dialog can outlast the 30-second default Plugin request timeout; make the choice, then restart.
- Fetch defaults to every 300 seconds; reconciliation is a separate scheduled stage, so updates are not instantaneous.
- EXTERNAL Planner items are read-only in Lum Harbor, and absence from a later fetch does not delete them.

Rebuilding the app changes its ad-hoc signature and can reset the permission to `notDetermined`. A fresh prompt after rebuild is expected.

### Native macOS notification does not appear

- Confirm exactly one enabled `notification.deliver` Instance and restart.
- Allow the notification bridge in macOS Notification settings.
- Check delivery errors in the log. Denied authorization returns an error and no durable external-success marker.
- The in-app Notification, toast, and unread state are separate from external delivery.

### Source processing failed

Fetching evidence and semantically interpreting it are separate. Check History for the Source observation and AI Run outcome. A `FAILED` semantic Source is not automatically retried; correct the provider and use the explicit **Interpret** action. An existing pending Proposal can intentionally suppress a newer interpretation until Apply or Reject.

### Proposal became STALE

The target changed after Proposal creation. STALE is a safety result, not data loss. Review current truth and ask for a new Proposal.

## Developer/operator troubleshooting

### Stale pre-release database

Before the first public release, the project intentionally edits the single `00001_foundation.sql` in place. An old database may report Goose version 1 while missing newer tables or constraints. There is no automatic repair. Use a fresh isolated user environment for acceptance or deliberately recreate only a disposable development database; never delete a real user's database as a routine fix.

`wails build` binding generation can initialize the application against the current user environment. For acceptance, use an isolated environment and confirm the intended database path before launch.

### Wails/frontend rebuild mismatch

- Run `npm run build` first to expose TypeScript failures.
- Regenerate Wails bindings only after an exported Go App contract changes: `wails generate module`.
- Review generated diffs and restore `frontend/wailsjs/runtime` if the CLI rewrites its vendored runtime.
- Use `wails build -clean` when cached packaging output is suspect.

### Slow or overloaded local AI

Assistant and background Source Processing share the effective `ai.Service`. A local model can serialize or slow requests; this is latency, not necessarily deadlock. Check final AI Run History and provider logs. Avoid running multiple full Go suites concurrently with Plugin acceptance on a loaded machine because handshake tests can time out.

### Logs and safe diagnosis

Temporarily set `logging.level` to `debug` in `config.json`, restart, reproduce once, and inspect `logs/lum-harbor.log`. Restore normal logging afterward. Never log protocol JSON to stdout inside a Plugin, and never log credentials or complete private payloads.

## Current V1 limits

- No Workflow lifecycle/completion state; no recurring Todo or Workflow-action engine.
- No general Planner edit/archive/delete/reopen UI and no general calendar-event creation.
- No Plugin hot reload, **Restart Now**, marketplace, signing, automatic update, or replace-in-place.
- No Plugin credential store, automatic Plugin restart, or general Plugin-to-Host callback API.
- Plugin data/cache cleanup is not shipped; uninstall preserves both.
- No Backup/Restore feature.
- Failed semantic Sources have no automatic retry/backoff.
- MonitorPoints cannot be edited, rescheduled, reactivated, or automatically marked satisfied.
- Dashboard and runtime health surfaces remain partial; no System runtime page or **Run Now** control.
- Generic external project Sources do not automatically map into WorkItems/Workflow/Tracked Items.
- Native Windows desktop runtime is not product-verified.

These are current boundaries, not roadmap promises.
