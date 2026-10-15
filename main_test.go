package main

import (
	"path/filepath"
	"testing"
	"time"

	"lum-harbor-notifier/internal/appdir"
	"lum-harbor-notifier/internal/config"
)

func TestProductionPluginRoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configRoot, err := appdir.ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := productionPluginRoot(filepath.Join("plugins", "nested"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(configRoot, "plugins", "nested"); resolved != want {
		t.Fatalf("relative plugin root: got %q want %q", resolved, want)
	}
	if _, err := productionPluginRoot(filepath.Join("..", "outside")); err == nil {
		t.Fatal("expected relative plugin root escape to fail")
	}
	abs := filepath.Join(t.TempDir(), "extensions")
	resolved, err = productionPluginRoot(abs)
	if err != nil || resolved != filepath.Clean(abs) {
		t.Fatalf("absolute plugin root: got %q err=%v", resolved, err)
	}
}

func TestSourceProcessingCycleTimeoutFitsConfiguredProviderCalls(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.BuiltInOpenAI.RequestTimeoutSeconds = 300
	// Every one of the 10 claimable Sources may need two 300 s provider
	// calls; the old fixed 60 s deadline cancelled local-model interpretation.
	if got, want := sourceProcessingCycleTimeout(cfg), 10*(600*time.Second+sourceProcessingCallMargin); got != want {
		t.Fatalf("cycle timeout = %s, want %s", got, want)
	}
}
