package readadapter_test

import (
	"testing"

	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/plugin/plugintest"
	"lum-harbor-notifier/internal/readcap"
	"lum-harbor-notifier/internal/testsupport"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

// TestPluginFixtureProcess lets this package's own compiled test binary act
// as a real Plugin subprocess when plugintest.WriteExtension's fixture.sh
// re-execs it with LUM_HARBOR_PLUGIN_FIXTURE_MODE set — the same one-line
// runner every other package that uses plugintest fixtures already defines
// (internal/plugin, internal/plugin/aiadapter, internal/background/pluginjob,
// internal/source/pluginadapter, internal/sourceprocessing, app,
// internal/applicationruntime).
func TestPluginFixtureProcess(t *testing.T) {
	if plugintest.RunIfRequested() {
		return
	}
}

// readcapFixture wires a real readcap.Service (real migrated SQLite CORE
// dependencies) to the readadapter.Source under test, proving CORE and
// PLUGIN capabilities are discovered/executed through the exact same
// Service — never a separate Plugin-only code path.
type readcapFixture struct {
	readcap *readcap.Service
}

func newReadcapFixture(t *testing.T, pluginSource readcap.PluginReadSource) *readcapFixture {
	t.Helper()
	database := testsupport.OpenTestDatabase(t)
	workItems := workitem.NewService(database.DB, workitem.NewRepository(database.DB))
	trackingSvc := tracking.NewService(database.DB, workItems)
	plannerSvc := planner.NewService(database.DB, planner.NewRepository(database.DB))

	service, err := readcap.NewService(workItems, trackingSvc, plannerSvc, database.DB, pluginSource, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &readcapFixture{readcap: service}
}
