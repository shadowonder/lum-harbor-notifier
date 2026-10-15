//go:build plugincompat

package plugincompat_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugininstall"
)

const (
	syntheticProjectExtensionID         = "lh-plugin-synthetic-project"
	syntheticProjectArtifactEnvironment = "LUM_HARBOR_SYNTHETIC_PROJECT_PLUGIN_COMPAT_ARTIFACT"
)

func TestActualSyntheticProjectPluginProtocolSourceFetchAndReadQuery(t *testing.T) {
	configured := strings.TrimSpace(os.Getenv(syntheticProjectArtifactEnvironment))
	if configured == "" {
		defaultPath := filepath.Join("..", "lh-plugin-synthetic-project", "dist", "lh-plugin-synthetic-project.lhplugin")
		if _, err := os.Stat(defaultPath); err == nil {
			configured = defaultPath
		} else {
			defaultDir := filepath.Join("..", "lh-plugin-synthetic-project", "dist", "lh-plugin-synthetic-project")
			if _, err := os.Stat(defaultDir); err == nil {
				configured = defaultDir
			} else {
				t.Skipf("synthetic project plugin artifact not found; set %s", syntheticProjectArtifactEnvironment)
			}
		}
	}

	artifactPath, err := filepath.Abs(configured)
	if err != nil {
		t.Fatalf("resolve artifact path: %v", err)
	}

	pluginRoot := t.TempDir()
	installer, err := plugininstall.New(pluginRoot)
	if err != nil {
		t.Fatal(err)
	}

	installResult, err := installer.Install(artifactPath)
	if err != nil {
		t.Fatalf("install synthetic project plugin: %v", err)
	}
	if installResult.ExtensionID != syntheticProjectExtensionID || !installResult.RestartRequired {
		t.Fatalf("unexpected install result: %+v", installResult)
	}

	registry, failures, err := plugin.Discover(pluginRoot)
	if err != nil || len(failures) != 0 {
		t.Fatalf("discover installed plugin: err=%v failures=%+v", err, failures)
	}

	extensions := registry.List()
	if len(extensions) != 1 || extensions[0].Manifest.ID != syntheticProjectExtensionID {
		t.Fatalf("unexpected discovered extensions: %+v", extensions)
	}
	if len(extensions[0].Manifest.OS) != 1 || extensions[0].Manifest.OS[0] != "darwin" {
		t.Fatalf("expected discovered OS [darwin], got %v", extensions[0].Manifest.OS)
	}
	if len(extensions[0].Manifest.Arch) != 1 || extensions[0].Manifest.Arch[0] != "arm64" {
		t.Fatalf("expected discovered Arch [arm64], got %v", extensions[0].Manifest.Arch)
	}

	var logs bytes.Buffer
	host, err := plugin.NewHost(registry, plugin.HostOptions{
		StartupTimeout:  3 * time.Second,
		RequestTimeout:  2 * time.Second,
		ShutdownTimeout: 2 * time.Second,
		Logger:          slog.New(slog.NewTextHandler(&logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := host.Start(ctx, syntheticProjectExtensionID); err != nil {
		t.Fatalf("start synthetic project plugin: %v", err)
	}
	defer func() {
		_ = host.Shutdown(ctx)
	}()

	// 1. Initial Source Fetch
	fetchInstance := plugin.Instance{
		ID:           "test-synthetic-fetch",
		ExtensionID:  syntheticProjectExtensionID,
		CapabilityID: plugin.CapabilitySourceFetch,
		Config:       json.RawMessage(`{"tick_advance":true}`),
	}
	fetcher := plugin.NewSourceFetcher(host)
	fetchResult1, err := fetcher.Fetch(ctx, fetchInstance, plugin.SourceFetchRequest{Cursor: nil})
	if err != nil {
		t.Fatalf("initial source.fetch failed: %v", err)
	}
	if len(fetchResult1.Records) != 3 {
		t.Fatalf("expected 3 initial records, got %d", len(fetchResult1.Records))
	}
	if fetchResult1.NextCursor == nil || *fetchResult1.NextCursor != "tick-0" {
		t.Fatalf("expected next cursor tick-0, got %v", fetchResult1.NextCursor)
	}

	// Verify records structure
	for _, rec := range fetchResult1.Records {
		if !strings.HasPrefix(rec.ExternalID, "workflow-") {
			t.Errorf("expected workflow- prefix in externalId, got %q", rec.ExternalID)
		}
		if rec.SourceKind != "synthetic.project.story" {
			t.Errorf("expected sourceKind synthetic.project.story, got %q", rec.SourceKind)
		}
		if !json.Valid(rec.Payload) {
			t.Errorf("invalid json payload for %s", rec.ExternalID)
		}
	}

	// 2. Subsequent Source Fetch with tick advance
	fetchResult2, err := fetcher.Fetch(ctx, fetchInstance, plugin.SourceFetchRequest{Cursor: fetchResult1.NextCursor})
	if err != nil {
		t.Fatalf("subsequent source.fetch failed: %v", err)
	}
	if len(fetchResult2.Records) != 5 {
		t.Fatalf("expected 5 records after advance, got %d", len(fetchResult2.Records))
	}
	if fetchResult2.NextCursor == nil || *fetchResult2.NextCursor != "tick-1" {
		t.Fatalf("expected next cursor tick-1, got %v", fetchResult2.NextCursor)
	}

	readInstance := plugin.Instance{
		ID:           "test-synthetic-read",
		ExtensionID:  syntheticProjectExtensionID,
		CapabilityID: plugin.CapabilityReadQuery,
		Config:       json.RawMessage(`{}`),
	}
	querier := plugin.NewReadQuerier(host)

	// 3. Read Query: get_story
	readResult1, err := querier.Query(ctx, readInstance, plugin.ReadQueryRequest{Query: "get_story:workflow-001/story-001"})
	if err != nil {
		t.Fatalf("read.query get_story failed: %v", err)
	}
	if !strings.Contains(readResult1.Summary, "workflow-001/story-001") {
		t.Fatalf("read.query summary missing story id: %+v", readResult1)
	}

	// 4. Read Query: get_workflow
	readResult2, err := querier.Query(ctx, readInstance, plugin.ReadQueryRequest{Query: "get_workflow:workflow-001"})
	if err != nil {
		t.Fatalf("read.query get_workflow failed: %v", err)
	}
	if !strings.Contains(readResult2.Summary, "workflow-001") {
		t.Fatalf("read.query get_workflow summary missing workflow id: %+v", readResult2)
	}

	// 5. Read Query: list_active
	readResult3, err := querier.Query(ctx, readInstance, plugin.ReadQueryRequest{Query: "list_active"})
	if err != nil {
		t.Fatalf("read.query list_active failed: %v", err)
	}
	if len(readResult3.Facts) == 0 {
		t.Fatalf("expected active facts, got none: %+v", readResult3)
	}

	// 6. Read Query: list_blocked
	readResult4, err := querier.Query(ctx, readInstance, plugin.ReadQueryRequest{Query: "list_blocked"})
	if err != nil {
		t.Fatalf("read.query list_blocked failed: %v", err)
	}
	if len(readResult4.Facts) == 0 {
		t.Fatalf("expected blocked facts at tick 1, got none: %+v", readResult4)
	}

	// 7. Read Query: search_stories
	readResult5, err := querier.Query(ctx, readInstance, plugin.ReadQueryRequest{Query: "search_stories:logging"})
	if err != nil {
		t.Fatalf("read.query search_stories failed: %v", err)
	}
	if len(readResult5.Facts) != 1 {
		t.Fatalf("expected 1 search result for logging, got: %+v", readResult5)
	}

	// 8. Read Query: overview
	readResult6, err := querier.Query(ctx, readInstance, plugin.ReadQueryRequest{Query: "overview"})
	if err != nil {
		t.Fatalf("read.query overview failed: %v", err)
	}
	if !strings.Contains(readResult6.Summary, "Synthetic Project Simulator") {
		t.Fatalf("unexpected overview summary: %+v", readResult6)
	}

	// 9. Shutdown and verify stderr diagnostics
	if err := host.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown synthetic project plugin: %v", err)
	}

	diag := logs.String()
	if !strings.Contains(diag, "lh-plugin-synthetic-project: started") || !strings.Contains(diag, "lh-plugin-synthetic-project: shutdown accepted") {
		t.Fatalf("expected diagnostics on stderr, got: %s", diag)
	}
}
