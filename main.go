package main

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"lum-harbor-notifier/app"
	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/ailineage"
	"lum-harbor-notifier/internal/airuntime"
	"lum-harbor-notifier/internal/appdir"
	"lum-harbor-notifier/internal/applicationruntime"
	"lum-harbor-notifier/internal/assistant"
	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/background"
	"lum-harbor-notifier/internal/calendar"
	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/credential"
	"lum-harbor-notifier/internal/dashboardview"
	"lum-harbor-notifier/internal/database"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/logging"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/plannerview"
	"lum-harbor-notifier/internal/platform"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/aiadapter"
	"lum-harbor-notifier/internal/plugin/readadapter"
	"lum-harbor-notifier/internal/plugininstall"
	"lum-harbor-notifier/internal/plugininstance"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/readcap"
	"lum-harbor-notifier/internal/reminder"
	"lum-harbor-notifier/internal/source"
	"lum-harbor-notifier/internal/source/pluginadapter"
	"lum-harbor-notifier/internal/sourceprocessing"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/trackingreevaluation"
	"lum-harbor-notifier/internal/workitem"
	"lum-harbor-notifier/internal/workitemoverview"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

// M11 Attention/Notification cycle: fixed-delay cadence (first run is triggered
// at startup, then one minute after each run finishes) and per-run timeout.
const (
	attentionCycleInterval = time.Minute
	attentionCycleTimeout  = 30 * time.Second
)

// M13 Slice 4A Reminder reconciliation cycle: a V1 implementation choice
// (Feature Definition asked only for "reasonably promptly," not a frozen
// cadence), matching the attention cycle's own fixed-delay-plus-startup-
// trigger shape. Not a Scheduler/Source/Foundation contract value.
const (
	reminderReconciliationInterval = 60 * time.Second
	reminderReconciliationTimeout  = 30 * time.Second
)

// M17 Slice 2 Notification delivery cycle: fixed-delay cadence (first run is
// triggered promptly at startup when registered, then follows a fixed delay)
// and per-run timeout, mirroring attention cycle and reminder reconciliation.
const (
	notificationDeliveryInterval = 60 * time.Second
	notificationDeliveryTimeout  = 30 * time.Second
)

// M18 Todo reminder cycle: checks active reminders and triggers attention/notifications
// at configured threshold offsets.
const (
	todoReminderCycleInterval = 60 * time.Second
	todoReminderCycleTimeout  = 15 * time.Second
)

// Background semantic source processing cycle: scans PENDING semantic sources out-of-band and executes Clean + Interpret.
const (
	sourceProcessingCycleInterval     = 60 * time.Second
	sourceProcessingCallMargin        = 15 * time.Second
	trackingReevaluationCycleInterval = 60 * time.Second
)

// sourceProcessingCycleTimeout sizes the cycle deadline from the configured
// AI request timeouts: every claimed Source runs source.clean and
// source.interpret, each bounded by the provider request timeout, so a fixed
// short deadline would cancel real interpretations mid-flight.
func sourceProcessingCycleTimeout(cfg config.Config) time.Duration {
	perCall := time.Duration(max(cfg.AI.BuiltInOpenAI.RequestTimeoutSeconds, cfg.Plugins.RequestTimeoutSeconds)) * time.Second
	for _, inst := range cfg.Plugins.Instances {
		if !inst.Disabled && inst.CapabilityID == string(plugin.CapabilityAIProvider) && len(inst.Config) > 0 {
			var tc struct {
				TimeoutSeconds float64 `json:"timeout_seconds"`
			}
			if err := json.Unmarshal(inst.Config, &tc); err == nil && tc.TimeoutSeconds > 0 {
				pluginTimeout := time.Duration(tc.TimeoutSeconds * float64(time.Second))
				if pluginTimeout > perCall {
					perCall = pluginTimeout
				}
			}
		}
	}
	return time.Duration(app.SourceProcessingBatchLimit) * (2*perCall + sourceProcessingCallMargin)
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() (resultErr error) {
	// Runtime configuration is loaded and validated before any dependency
	// construction. Startup errors use the standard logger because configured
	// logging does not exist yet.
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load runtime configuration: %w", err)
	}
	logger, err := logging.New(cfg.Logging)
	if err != nil {
		return fmt.Errorf("initialize logging: %w", err)
	}
	slog.SetDefault(logger)

	// Open database connection to sqlite
	ctx := context.Background()
	db, err := database.Open(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close database: %w", err))
		}
	}()

	// Compose the long-lived application dependency graph in one place.
	// Repositories created here are bound to *sql.DB and may be safely reused
	// by services for the lifetime of the process. Mutation methods continue to
	// create separate repositories bound to their own *sql.Tx.
	plannerRepository := planner.NewRepository(db.DB)
	calendarRepository := calendar.NewRepository(db.DB)
	plannerService := planner.NewService(db.DB, plannerRepository)
	plannerViewService := plannerview.NewService(plannerService, calendarRepository)
	workitemRepository := workitem.NewRepository(db.DB)
	workitemService := workitem.NewService(db.DB, workitemRepository)
	proposalService := proposal.NewService(db.DB, workitemService)
	trackingService := tracking.NewService(db.DB, workitemService)
	reminderService := reminder.NewService(db.DB)
	trackingReevaluationService := trackingreevaluation.NewService(db.DB)
	proposalService.SetTrackingOperator(trackingService)
	proposalService.SetPlannerOperator(plannerService)
	proposalService.SetReminderOperator(reminderService)
	attentionService := attention.NewService(db.DB, trackingService)
	plannerService.AddCompletionHook(func(ctx context.Context, tx *sql.Tx, plannerItemID, revisionID string, now time.Time) error {
		if err := reminderService.SatisfyReminderTx(ctx, tx, plannerItemID, revisionID, now); err != nil {
			return err
		}
		if err := attentionService.ResolveByPlannerItemTx(ctx, tx, plannerItemID, revisionID, now); err != nil {
			return err
		}
		return trackingReevaluationService.RequestForCompletionTx(ctx, tx, plannerItemID, revisionID, now)
	})
	workItemOverviewService := workitemoverview.NewService(workitemService, trackingService, attentionService, plannerService)
	dashboardViewService := dashboardview.NewService(plannerViewService, workitemService, trackingService)
	historyService := history.NewService(history.NewRepository(db.DB))

	pluginRoot, err := productionPluginRoot(cfg.Plugins.Directory)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(pluginRoot, 0o700); err != nil {
		return fmt.Errorf("create plugin root %q: %w", pluginRoot, err)
	}
	pluginInstaller, err := plugininstall.New(pluginRoot)
	if err != nil {
		return fmt.Errorf("construct plugin installer: %w", err)
	}
	registry, discoveryFailures, err := plugin.Discover(pluginRoot)
	if err != nil {
		return fmt.Errorf("discover production plugins: %w", err)
	}
	for _, failure := range discoveryFailures {
		slog.Warn("plugin discovery skipped invalid extension", "directory", failure.Directory, "error", failure.Err)
	}
	configuredInstances, err := plugininstance.Resolve(cfg.Plugins.Instances, registry)
	if err != nil {
		return fmt.Errorf("resolve configured plugin instances: %w", err)
	}
	slog.Info("resolved configured plugin instances", "count", len(configuredInstances))
	shutdownTimeout := time.Duration(cfg.Background.ShutdownTimeoutSeconds) * time.Second
	host, err := plugin.NewHost(registry, plugin.HostOptions{
		StartupTimeout:  time.Duration(cfg.Plugins.StartupTimeoutSeconds) * time.Second,
		RequestTimeout:  time.Duration(cfg.Plugins.RequestTimeoutSeconds) * time.Second,
		ShutdownTimeout: shutdownTimeout,
		Logger:          logger,
	})
	if err != nil {
		return fmt.Errorf("construct plugin host: %w", err)
	}
	scheduler := background.NewScheduler()
	if recovered, err := trackingReevaluationService.RecoverInterrupted(ctx); err != nil {
		slog.Warn("recover interrupted tracking reevaluations", "error", err)
	} else if recovered > 0 {
		slog.Warn("requeued interrupted tracking reevaluations", "count", recovered)
	}
	sourceService := source.NewService(db.DB)
	// Nothing is processing before the scheduler starts, so any PROCESSING
	// row was interrupted by the previous run. Best-effort: recovery must
	// never block startup.
	if orphaned, err := sourceService.FailOrphanedProcessing(ctx); err != nil {
		slog.Warn("recover interrupted source processing", "error", err)
	} else if orphaned > 0 {
		slog.Warn("recorded interrupted source processing as failed", "count", orphaned)
	}
	sourceAdapter, err := pluginadapter.New(sourceService)
	if err != nil {
		return fmt.Errorf("construct durable source plugin adapter: %w", err)
	}
	sourceDefinitions, err := applicationruntime.RegisterSourceWorkloads(
		scheduler,
		registry,
		configuredInstances,
		plugin.NewSourceFetcher(host),
		sourceAdapter,
		sourceAdapter,
		time.Duration(cfg.Plugins.RequestTimeoutSeconds)*time.Second,
	)
	if err != nil {
		return fmt.Errorf("register production source workloads: %w", err)
	}
	slog.Info("registered production source workloads", "count", len(sourceDefinitions))
	// M13 Slice 4A: exactly one Reminder reconciliation Job, registered only
	// when a resolved Instance actually selects the real Reminders Plugin's
	// source.fetch capability — never one Job per Instance. Pipeline B
	// (Source Layer -> plannersync.Reconciler -> Planner) stays fully
	// separate from Pipeline A (Plugin -> source.fetch -> Source Layer)
	// registered just above: this reuses the same already-constructed
	// sourceService/plannerService, never the SourceFetchJob/Sink path. As of
	// M14 Slice 2, external identity resolution lives entirely inside
	// planner.Service, so no process-scoped foundation.Repository is
	// constructed here anymore.
	reminderReconciliationDefinition, err := applicationruntime.RegisterReminderReconciliation(
		scheduler, configuredInstances, sourceService, plannerService,
		reminderReconciliationInterval, reminderReconciliationTimeout,
	)
	if err != nil {
		return fmt.Errorf("register reminder reconciliation: %w", err)
	}
	if reminderReconciliationDefinition != nil {
		slog.Info("registered reminder reconciliation cycle")
	}
	// M11: one static deterministic Attention/Notification cycle (Reconcile then
	// IN_APP Dispatch), registered before Start next to the source workloads.
	attentionDefinition, err := applicationruntime.RegisterAttentionCycle(scheduler, attentionService, attentionCycleInterval, attentionCycleTimeout)
	if err != nil {
		return fmt.Errorf("register attention cycle: %w", err)
	}
	// M17 Slice 2: external notification delivery cycle, conditionally registered
	// only when a resolved Instance selects the notification.deliver capability.
	notificationDeliveryDefinition, err := applicationruntime.RegisterProductionNotificationDelivery(
		scheduler, configuredInstances, host, db.DB,
		notificationDeliveryInterval, notificationDeliveryTimeout,
	)
	if err != nil {
		return fmt.Errorf("register notification delivery cycle: %w", err)
	}
	if notificationDeliveryDefinition != nil {
		slog.Info("registered notification delivery cycle")
	}
	// M18: one static deterministic Todo reminder cycle, evaluating reminder thresholds.
	todoReminderCycle := applicationruntime.NewTodoReminderCycle(db.DB, reminderService, attentionService)
	todoReminderDefinition, err := applicationruntime.RegisterTodoReminderCycle(scheduler, todoReminderCycle, todoReminderCycleInterval, todoReminderCycleTimeout)
	if err != nil {
		return fmt.Errorf("register todo reminder cycle: %w", err)
	}
	slog.Info("registered todo reminder cycle")

	// Semantic source processing cycle: scans PENDING semantic sources out-of-band and executes Clean + Interpret.
	deferredSourceCycle := &applicationruntime.DeferredSourceProcessingCycle{}
	sourceProcessingDefinition, err := applicationruntime.RegisterSourceProcessingCycle(
		scheduler, deferredSourceCycle, sourceProcessingCycleInterval, sourceProcessingCycleTimeout(cfg),
	)
	if err != nil {
		return fmt.Errorf("register source processing cycle: %w", err)
	}
	slog.Info("registered source processing cycle")
	deferredTrackingReevaluationCycle := &applicationruntime.DeferredTrackingReevaluationCycle{}
	trackingReevaluationDefinition, err := applicationruntime.RegisterTrackingReevaluationCycle(
		scheduler, deferredTrackingReevaluationCycle, trackingReevaluationCycleInterval, sourceProcessingCycleTimeout(cfg),
	)
	if err != nil {
		return fmt.Errorf("register tracking reevaluation cycle: %w", err)
	}
	slog.Info("registered tracking reevaluation cycle")

	applicationRuntime, err := applicationruntime.New(scheduler, host, configuredInstances, shutdownTimeout)
	if err != nil {
		return fmt.Errorf("construct application runtime: %w", err)
	}
	pluginInventory, err := app.NewPluginInventoryReader(registry, cfg.Plugins.Instances, host)
	if err != nil {
		return fmt.Errorf("construct plugin inventory: %w", err)
	}
	// The startup-bound credential store is resolved once, concretely, against
	// the store mode saved in cfg at process start. Settings resolves again on
	// every read/Set/Delete against whatever store mode is currently saved
	// (see app.OpenAIStoreResolver), so the bare credential.NewOpenAIStore
	// function value is passed uncalled into NewApp for that later use.
	openAICredentials := credential.NewOpenAIStore(cfg.AI.BuiltInOpenAI.UseSystemCredentialManager)
	aiComposition, err := applicationruntime.ComposeAI(ctx, registry, configuredInstances, host, host, cfg.AI.BuiltInOpenAI, openAICredentials)
	if err != nil {
		return fmt.Errorf("compose production AI runtime: %w", err)
	}
	if err := applicationRuntime.Start(context.Background()); err != nil {
		return err
	}
	// Resolve native notification permission as soon as the selected delivery
	// plugin is running, before any initial delivery cycle can be triggered.
	// This probe uses notification.deliver itself and never creates a durable
	// Notification or a platform notification.
	readiness, readinessErr := applicationruntime.CheckProductionNotificationDeliveryReadiness(
		context.Background(), configuredInstances, host,
	)
	if readinessErr != nil {
		slog.Warn("native notification permission check failed", "error", readinessErr)
	} else if readiness != nil {
		slog.Info("native notification permission checked", "authorizationStatus", readiness.AuthorizationStatus)
	}
	// Run one cycle at startup so durable PENDING state drains immediately after
	// a restart; subsequent cycles follow the fixed delay.
	if err := scheduler.Trigger(attentionDefinition.ID); err != nil {
		slog.Warn("could not trigger initial attention cycle", "error", err)
	}
	// M13 Slice 4A: an initial reconciliation runs promptly at startup too, so
	// durable REMINDER Source truth from a prior session does not have to wait
	// out a full fixed-delay interval before reappearing in Planner.
	if reminderReconciliationDefinition != nil {
		if err := scheduler.Trigger(reminderReconciliationDefinition.ID); err != nil {
			slog.Warn("could not trigger initial reminder reconciliation", "error", err)
		}
	}
	// M17 Slice 2: an initial delivery cycle runs promptly at startup too, so
	// any SENT Notifications awaiting external delivery from a prior session drain
	// immediately after restart.
	if notificationDeliveryDefinition != nil {
		if err := scheduler.Trigger(notificationDeliveryDefinition.ID); err != nil {
			slog.Warn("could not trigger initial notification delivery cycle", "error", err)
		}
	}
	// M18: an initial todo reminder cycle runs promptly at startup too, so
	// any threshold reached while the app was closed triggers immediately.
	if err := scheduler.Trigger(todoReminderDefinition.ID); err != nil {
		slog.Warn("could not trigger initial todo reminder cycle", "error", err)
	}

	readcapPluginSource, err := readadapter.New(host, registry, configuredInstances)
	if err != nil {
		return fmt.Errorf("construct read capability plugin source: %w", err)
	}
	readcapService, err := readcap.NewService(workitemService, trackingService, plannerService, db.DB, readcapPluginSource, attentionService, historyService)
	if err != nil {
		return fmt.Errorf("construct read capability service: %w", err)
	}
	// proposeRegistry is the single application-owned source of Proposal
	// capability descriptors and handlers - the mutation-side counterpart of
	// readcapService above. It has no service dependencies of its own
	// (handlers receive *App at call time), so it can be constructed once
	// and shared by both the AI-facing catalog and App dispatch execution.
	proposeRegistry := app.NewProposeRegistry()
	capabilityHost, err := airuntime.NewCapabilityHost(readcapService, proposeRegistry)
	if err != nil {
		return fmt.Errorf("construct AI capability host: %w", err)
	}
	nativeRuntime, err := airuntime.NewNative(aiComposition, capabilityHost)
	if err != nil {
		return fmt.Errorf("construct native AI runtime: %w", err)
	}
	var aiRuntime airuntime.Runtime = nativeRuntime
	aiProjection := aiComposition.Projection()
	if aiProjection.Status == applicationruntime.AIRuntimeReady && aiProjection.Mode == ai.RuntimeModePluginRuntime {
		for index := range configuredInstances {
			instance := &configuredInstances[index]
			if instance.CapabilityID != plugin.CapabilityAIRuntime || instance.ID != aiProjection.ManagingProviderInstanceID {
				continue
			}
			pluginRuntime, runtimeErr := aiadapter.NewRuntime(host, instance.ExtensionID, instance, capabilityHost)
			if runtimeErr != nil {
				return fmt.Errorf("construct Plugin AI runtime: %w", runtimeErr)
			}
			aiRuntime = pluginRuntime
		}
	}
	assistantRuntime, err := assistant.NewRuntimeAdapter(aiRuntime, proposeRegistry)
	if err != nil {
		return fmt.Errorf("construct Assistant runtime adapter: %w", err)
	}

	runtimeConfig := app.NewRuntimeConfigDTO(
		cfg.General,
		cfg.Dashboard.TodoDisplayLimit,
		platform.Current(),
		cfg.Notifications.InApp,
	)
	application := app.NewApp(plannerService, plannerViewService, workitemService, workItemOverviewService, proposalService, trackingService, attentionService, dashboardViewService, historyService, reminderService, runtimeConfig, pluginInventory, aiComposition, aiComposition, assistantRuntime, proposeRegistry, readcapService, credential.NewOpenAIStore, cfg.AI.BuiltInOpenAI, pluginInstaller, applicationRuntime)
	application.SetAIRuntime(aiRuntime)
	lineageService := ailineage.NewService(db.DB)
	sourceProcessor := sourceprocessing.NewProcessor(db.DB, aiComposition.Service).SetRuntime(aiRuntime)
	application.SetSourceDependencies(sourceService, sourceProcessor, lineageService)
	deferredSourceCycle.Set(application)
	application.SetTrackingReevaluationDependencies(
		trackingReevaluationService,
		func() error { return scheduler.Trigger(trackingReevaluationDefinition.ID) },
		func() error { return scheduler.Trigger(todoReminderDefinition.ID) },
	)
	deferredTrackingReevaluationCycle.Set(application)
	// Run initial source processing cycle promptly at startup so durable PENDING sources drain immediately after restart
	if err := scheduler.Trigger(sourceProcessingDefinition.ID); err != nil {
		slog.Warn("could not trigger initial source processing cycle", "error", err)
	}
	if err := scheduler.Trigger(trackingReevaluationDefinition.ID); err != nil {
		slog.Warn("could not trigger initial tracking reevaluation cycle", "error", err)
	}

	hideWindowOnClose := cfg.General.CloseWindowBehavior == config.CloseWindowBehaviorBackground
	appOptions := &options.App{
		Title:             "Lum Harbor",
		Width:             1100,
		Height:            700,
		MinWidth:          880,
		MinHeight:         600,
		HideWindowOnClose: hideWindowOnClose,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 247, G: 248, B: 246, A: 255},
		OnStartup:        application.Startup,
		OnShutdown:       application.Shutdown,
		Mac: &mac.Options{
			OnUrlOpen: app.NotificationURLHandler(application),
		},
		Bind: []interface{}{
			application,
		},
	}
	application.SetAppOptions(appOptions)

	// Create application with options
	err = wails.Run(appOptions)

	// OnShutdown normally initiates this sequence. Calling Shutdown again is
	// intentional and idempotent, and also covers a Wails startup/run failure
	// that returns without invoking the callback.
	if shutdownErr := applicationRuntime.Shutdown(); shutdownErr != nil {
		slog.Error("application runtime exceeded bounded shutdown callback", "error", shutdownErr)
	}
	shutdownErr := applicationRuntime.Wait()
	if err != nil {
		err = fmt.Errorf("run Wails application: %w", err)
	}
	return errors.Join(err, shutdownErr)
}

func productionPluginRoot(configured string) (string, error) {
	if filepath.IsAbs(configured) {
		return filepath.Clean(configured), nil
	}
	root, err := appdir.ConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve plugin root: %w", err)
	}
	candidate := filepath.Join(root, filepath.Clean(configured))
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == ".." || filepath.IsAbs(relative) || (len(relative) > 3 && relative[:3] == ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("relative plugin root %q escapes application config directory", configured)
	}
	return candidate, nil
}
