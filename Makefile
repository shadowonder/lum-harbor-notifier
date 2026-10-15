.PHONY: fmt lint check plugin-compat plugin-compat-local plugin-compat-local-windows reminders-plugin-compat-local notifications-plugin-compat-local ai-orchestrator-plugin-compat-local

PLUGIN_COMPAT_ARTIFACT ?=
PLUGIN_COMPAT_LOCAL_ARTIFACT := ../lh-plugin-base/dist/lh-plugin-base
PLUGIN_COMPAT_WINDOWS_ARTIFACT ?=
PLUGIN_COMPAT_LOCAL_WINDOWS_ARTIFACT := ../lh-plugin-base/dist/lh-plugin-base-windows-amd64
REMINDERS_PLUGIN_COMPAT_LOCAL_ARTIFACT := ../lh-plugin-apple-reminders/dist/lh-plugin-apple-reminders.lhplugin
NOTIFICATIONS_PLUGIN_COMPAT_LOCAL_ARTIFACT := ../lh-plugin-macos-notifications/dist/lh-plugin-macos-notifications.lhplugin

fmt:
	golangci-lint fmt ./...
	cd frontend && npx prettier . --write

lint:
	golangci-lint run ./...
	cd frontend && npm run lint

check:
	golangci-lint run ./...
	cd frontend && npm run lint
	cd frontend && npx prettier . --check

plugin-compat:
	@if [ -z "$(PLUGIN_COMPAT_ARTIFACT)" ]; then echo "PLUGIN_COMPAT_ARTIFACT is required" >&2; exit 2; fi
	@if [ ! -d "$(PLUGIN_COMPAT_ARTIFACT)" ] && [ ! -f "$(PLUGIN_COMPAT_ARTIFACT)" ]; then echo "plugin artifact does not exist: $(PLUGIN_COMPAT_ARTIFACT)" >&2; exit 2; fi
	LUM_HARBOR_PLUGIN_COMPAT_ARTIFACT="$(abspath $(PLUGIN_COMPAT_ARTIFACT))" go test -count=1 -tags=plugincompat ./integration/plugincompat

plugin-compat-local:
	$(MAKE) -C ../lh-plugin-base clean
	$(MAKE) -C ../lh-plugin-base
	$(MAKE) plugin-compat PLUGIN_COMPAT_ARTIFACT="$(PLUGIN_COMPAT_LOCAL_ARTIFACT)"

# plugin-compat-local-windows only proves install/discover/manifest
# structure for a real Windows-built artifact (see
# TestActualHostValidatesStandaloneWindowsArtifactWithoutExecuting) — it
# cannot execute the .exe or run the RPC protocol on a non-Windows host.
# Full native Windows Plugin acceptance still requires a real Windows host.
plugin-compat-local-windows:
	$(MAKE) -C ../lh-plugin-base build-windows
	@if [ ! -d "$(PLUGIN_COMPAT_LOCAL_WINDOWS_ARTIFACT)" ]; then echo "Windows plugin artifact directory does not exist: $(PLUGIN_COMPAT_LOCAL_WINDOWS_ARTIFACT)" >&2; exit 2; fi
	LUM_HARBOR_PLUGIN_COMPAT_WINDOWS_ARTIFACT="$(abspath $(PLUGIN_COMPAT_LOCAL_WINDOWS_ARTIFACT))" go test -count=1 -tags=plugincompat -run TestActualHostValidatesStandaloneWindowsArtifactWithoutExecuting ./integration/plugincompat

# reminders-plugin-compat-local is macOS-only and TCC-gated: it requires
# Reminders access already granted to the built artifact on this machine
# (see the M13 Slice 3A report). It builds the real Swift Plugin artifact
# and runs the real handshake/source.fetch/Source-Layer-ingestion test
# against it — never a mock, never a fixture.
reminders-plugin-compat-local:
	$(MAKE) -C ../lh-plugin-apple-reminders clean
	$(MAKE) -C ../lh-plugin-apple-reminders package
	@if [ ! -f "$(REMINDERS_PLUGIN_COMPAT_LOCAL_ARTIFACT)" ] && [ ! -d "$(REMINDERS_PLUGIN_COMPAT_LOCAL_ARTIFACT)" ]; then echo "Reminders plugin artifact does not exist: $(REMINDERS_PLUGIN_COMPAT_LOCAL_ARTIFACT)" >&2; exit 2; fi
	LUM_HARBOR_REMINDERS_PLUGIN_COMPAT_ARTIFACT="$(abspath $(REMINDERS_PLUGIN_COMPAT_LOCAL_ARTIFACT))" go test -count=1 -tags=plugincompat -run TestActualRemindersPluginProtocolAndSourceFetch -v ./integration/plugincompat

# notifications-plugin-compat-local is macOS-only: it builds and packages
# the real macOS notification bridge, starts it via the delivery adapter,
# and verifies real delivery against the native notification center.
notifications-plugin-compat-local:
	$(MAKE) -C ../lh-plugin-macos-notifications clean
	$(MAKE) -C ../lh-plugin-macos-notifications package
	go test -count=1 -v -run TestRealNotificationMacOSBridgePluginProtocolAndDelivery ./internal/plugin/deliveryadapter

SYNTHETIC_PLUGIN_COMPAT_LOCAL_ARTIFACT := ../lh-plugin-synthetic-project/dist/lh-plugin-synthetic-project.lhplugin

synthetic-plugin-compat-local:
	$(MAKE) -C ../lh-plugin-synthetic-project clean
	$(MAKE) -C ../lh-plugin-synthetic-project package
	LUM_HARBOR_SYNTHETIC_PROJECT_PLUGIN_COMPAT_ARTIFACT="$(abspath $(SYNTHETIC_PLUGIN_COMPAT_LOCAL_ARTIFACT))" go test -count=1 -tags=plugincompat -run TestActualSyntheticProjectPluginProtocolSourceFetchAndReadQuery -v ./integration/plugincompat

AI_PLUGIN_COMPAT_LOCAL_ARTIFACT := ../lh-plugin-ai-runtime/dist/lh-plugin-ai-runtime.lhplugin
AI_ORCHESTRATOR_PLUGIN_COMPAT_LOCAL_ARTIFACT := ../lh-plugin-ai-orchestrator/dist/lh-plugin-ai-orchestrator.lhplugin

ai-plugin-compat-local:
	$(MAKE) -C ../lh-plugin-ai-runtime clean
	$(MAKE) -C ../lh-plugin-ai-runtime package
	LUM_HARBOR_AI_RUNTIME_PLUGIN_COMPAT_ARTIFACT="$(abspath $(AI_PLUGIN_COMPAT_LOCAL_ARTIFACT))" go test -count=1 -tags=plugincompat -run TestActualAIRuntimePluginProtocolAndAIProvider -v ./integration/plugincompat

ai-orchestrator-plugin-compat-local:
	$(MAKE) -C ../lh-plugin-ai-orchestrator clean
	$(MAKE) -C ../lh-plugin-ai-orchestrator package
	LUM_HARBOR_AI_ORCHESTRATOR_PLUGIN_COMPAT_ARTIFACT="$(abspath $(AI_ORCHESTRATOR_PLUGIN_COMPAT_LOCAL_ARTIFACT))" go test -count=1 -tags=plugincompat -run TestActualAIOrchestratorPluginProtocolAndPipeline -v ./integration/plugincompat
