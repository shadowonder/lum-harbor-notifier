package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/ailineage"
	"lum-harbor-notifier/internal/airuntime"
	"lum-harbor-notifier/internal/assistant"
	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/credential"
	"lum-harbor-notifier/internal/dashboardview"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/plannerview"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/readcap"
	"lum-harbor-notifier/internal/reminder"
	"lum-harbor-notifier/internal/source"
	"lum-harbor-notifier/internal/sourceprocessing"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/trackingreevaluation"
	"lum-harbor-notifier/internal/workitem"
	"lum-harbor-notifier/internal/workitemoverview"

	"github.com/wailsapp/wails/v2/pkg/options"
)

// RuntimeLifecycle is the narrow application-owned shutdown boundary used by
// the Wails hook. Construction and startup remain in main's composition root.
type RuntimeLifecycle interface {
	Shutdown() error
}

type AIExecutor interface {
	Execute(context.Context, ai.Request) (ai.Response, error)
}

type SourceContextReader interface {
	Execute(context.Context, readcap.Request) (readcap.Result, error)
}

// OpenAIStoreResolver resolves the credential store to use for a given
// desired store selection (macOS: true = system credential manager, false =
// application-managed fallback; every other platform ignores the flag).
//
// This is a function type rather than a single resolved credential.OpenAIStore
// because two independent responsibilities need different stores at different
// times: startup composition (main.ComposeAI) always resolves once against
// the config loaded at process start, while Settings must resolve again on
// every read/Set/Delete against whatever store mode is currently saved,
// which can change without a restart. A plain function value keeps both
// call sites able to reuse credential.NewOpenAIStore directly (or, in tests,
// a closure returning an in-memory fake) without a larger store-registry
// abstraction.
type OpenAIStoreResolver func(useSystemCredentialManager bool) credential.OpenAIStore

// App is the Wails-facing application boundary.
//
// Its services are process-scoped dependencies assembled by the composition
// root in main. App does not own the database lifetime and does not construct
// repositories or domain services itself.
type App struct {
	ctx context.Context

	// plannerService owns frontend-triggered Planner domain mutations.
	plannerService *planner.Service

	// plannerViewService owns the page-level Planner read projection.
	plannerViewService *plannerview.Service

	// workitemService owns WorkItem create/read/detail/lifecycle use cases and transactions.
	workitemService *workitem.Service

	// workItemOverviewService owns the cross-domain WorkItem list projection.
	workItemOverviewService *workitemoverview.Service

	// proposalService owns Change Proposal persistence, producer, and apply/reject lifecycle.
	proposalService *proposal.Service

	// trackingService owns Tracking Core truth (workflows, TrackedItems, step
	// states, MonitorPoints). Its commands are human-explicit direct commands.
	trackingService *tracking.Service

	// attentionService owns Attention/Notification truth. The App exposes only
	// the human interactions (Mark Read, Snooze) and the Notification Center read;
	// Reconcile and Dispatch are background-only and never reach the frontend.
	attentionService *attention.Service

	// dashboardViewService owns the cross-domain Dashboard read projection.
	dashboardViewService *dashboardview.Service

	// historyService owns bounded, read-only semantic History projection.
	historyService *history.Service

	// reminderService owns Todo reminder intent and lifecycle.
	reminderService *reminder.Service

	// runtimeConfig is the immutable, frontend-safe startup projection.
	runtimeConfig RuntimeConfigDTO

	// generalConfigMu serializes desktop General setting saves.
	generalConfigMu sync.Mutex

	// appOptions holds the Wails options reference so desktop shell mutations
	// can dynamically update window close behavior.
	appOptions   *options.App
	appOptionsMu sync.Mutex

	// notificationConfigMu serializes presentation-preference saves. The
	// frontend applies the returned desired value immediately; backend runtime
	// notification semantics remain unchanged.
	notificationConfigMu sync.Mutex

	// notificationActivationMu protects the bounded, process-memory handoff
	// from a native macOS notification click into the frontend's existing
	// navigation paths. It is not durable notification state.
	notificationActivationMu       sync.Mutex
	pendingNotificationActivations []NotificationActivationDTO
	notificationActivationUIReady  bool

	// pluginInventory projects startup-owned Registry/Instances plus current
	// Host process state without exposing manifests, config, or runtime internals.
	pluginInventory *PluginInventoryReader

	// aiControlPlane projects the exact production central AI composition and
	// live Plugin process availability to Settings.
	aiControlPlane AIControlPlaneReader
	aiExecutor     AIExecutor
	aiRuntime      airuntime.Runtime

	// assistantRuntime shapes Assistant context and decodes the final typed
	// result. The selected high-level AI Runtime owns all reasoning rounds.
	assistantRuntime *assistant.RuntimeAdapter

	// proposeRegistry is the single application-owned source of Proposal
	// capability descriptors and handlers. The same instance is also passed
	// to assistantRuntime at composition time so final-result instructions render exactly the
	// capabilities this registry can execute.
	proposeRegistry *ProposeRegistry

	// Built-in OpenAI settings are next-start desired state. The immutable

	// current Service remains unchanged after these fields are saved.
	//
	// aiCredentialStoreResolver is resolved fresh against the currently saved
	// store-mode config on every Settings read/Set/Delete, so it always
	// targets the current Saved/Desired store rather than the fixed store the
	// process started with.
	aiCredentialStoreResolver OpenAIStoreResolver
	startupAIConfig           config.BuiltInOpenAIConfig
	aiConfigMu                sync.Mutex
	aiRestartRequired         bool

	// pluginInstaller places user-selected artifact directories into the
	// configured plugin root. It never mutates the current Registry/Host.
	pluginInstaller PluginInstaller

	// pluginConfigMu serializes every Plugin-instance desired-config mutation
	// (create, enable, disable, remove) against the same load-validate-save
	// sequence so two near-simultaneous submissions cannot silently lose one
	// declaration to a last-writer-wins overwrite. All such mutations share
	// this one mutex rather than one per mutation kind.
	pluginConfigMu sync.Mutex

	// removedExtensionIDs tracks extensions whose declarations were durably
	// detached by RemovePlugin during this application lifetime. The startup
	// Registry is immutable and still lists such an extension as installed, so
	// without this set a later CreatePluginInstance could resolve successfully
	// against stale Registry state after artifact removal succeeded, failed, or
	// partially completed. Always read/written while holding pluginConfigMu.
	removedExtensionIDs map[string]struct{}

	// sourceService owns Source layer records and revisions.
	sourceService *source.Service

	// sourceProcessor orchestrates source.clean and source.interpret runs.
	sourceProcessor *sourceprocessing.Processor

	// sourceContextReader performs the one exact external-identity read used
	// to build bounded Source interpretation context.
	sourceContextReader SourceContextReader

	// lineageService records durable AI run lineage.
	lineageService *ailineage.Service

	// trackingReevaluationService owns the purpose-specific durable completion
	// handoff. The trigger functions only wake existing Scheduler jobs; durable
	// truth remains in SQLite.
	trackingReevaluationService *trackingreevaluation.Service
	trackingReevaluationTrigger func() error
	todoReminderTrigger         func() error

	// runtimeLifecycle owns ordered Scheduler then Plugin Host shutdown.
	runtimeLifecycle RuntimeLifecycle
}

// SetAIRuntime gives product controls the exact same high-level Runtime used
// by Assistant and Source processing. The low-level executor remains only as
// a compatibility seam for embedded callers and focused provider tests.
func (a *App) SetAIRuntime(runtime airuntime.Runtime) *App {
	a.aiRuntime = runtime
	return a
}

// SetSourceDependencies configures source, interpretation processor, and AI lineage dependencies.
func (a *App) SetSourceDependencies(sourceService *source.Service, sourceProcessor *sourceprocessing.Processor, lineageService *ailineage.Service) *App {
	a.sourceService = sourceService
	a.sourceProcessor = sourceProcessor
	a.lineageService = lineageService
	return a
}

func (a *App) SetTrackingReevaluationDependencies(service *trackingreevaluation.Service, reevaluationTrigger, todoReminderTrigger func() error) *App {
	a.trackingReevaluationService = service
	a.trackingReevaluationTrigger = reevaluationTrigger
	a.todoReminderTrigger = todoReminderTrigger
	return a
}

// NewApp creates the Wails application with its fully constructed services.
//
// Keeping dependency construction outside App makes the process-scoped
// dependency graph explicit and leaves transaction-scoped repository creation
// with the services that own those transactions.
func NewApp(
	plannerService *planner.Service,
	plannerViewService *plannerview.Service,
	workitemService *workitem.Service,
	workItemOverviewService *workitemoverview.Service,
	proposalService *proposal.Service,
	trackingService *tracking.Service,
	attentionService *attention.Service,
	dashboardViewService *dashboardview.Service,
	historyService *history.Service,
	reminderService *reminder.Service,
	runtimeConfig RuntimeConfigDTO,
	pluginInventory *PluginInventoryReader,
	aiControlPlane AIControlPlaneReader,
	aiExecutor AIExecutor,
	assistantRuntime *assistant.RuntimeAdapter,
	proposeRegistry *ProposeRegistry,
	sourceContextReader SourceContextReader,
	aiCredentialStoreResolver OpenAIStoreResolver,
	startupAIConfig config.BuiltInOpenAIConfig,
	pluginInstaller PluginInstaller,
	runtimeLifecycle RuntimeLifecycle,
) *App {
	return &App{
		plannerService:            plannerService,
		plannerViewService:        plannerViewService,
		workitemService:           workitemService,
		workItemOverviewService:   workItemOverviewService,
		proposalService:           proposalService,
		trackingService:           trackingService,
		attentionService:          attentionService,
		dashboardViewService:      dashboardViewService,
		historyService:            historyService,
		reminderService:           reminderService,
		runtimeConfig:             runtimeConfig,
		pluginInventory:           pluginInventory,
		aiControlPlane:            aiControlPlane,
		aiExecutor:                aiExecutor,
		assistantRuntime:          assistantRuntime,
		proposeRegistry:           proposeRegistry,
		sourceContextReader:       sourceContextReader,
		aiCredentialStoreResolver: aiCredentialStoreResolver,
		startupAIConfig:           startupAIConfig,
		pluginInstaller:           pluginInstaller,
		runtimeLifecycle:          runtimeLifecycle,
	}
}

// SetAppOptions stores the Wails options reference so desktop shell mutations
// can dynamically update window close behavior.
func (a *App) SetAppOptions(opts *options.App) {
	a.appOptionsMu.Lock()
	a.appOptions = opts
	a.appOptionsMu.Unlock()
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) Startup(ctx context.Context) {
	a.notificationActivationMu.Lock()
	a.ctx = ctx
	a.notificationActivationMu.Unlock()
	initTray(a)
}

// Shutdown is called by Wails before its run loop returns. Runtime Shutdown is
// bounded and idempotent; the composition root subsequently Waits for any
// safety drain before allowing the database defer to run.
func (a *App) Shutdown(_ context.Context) {
	if a.runtimeLifecycle == nil {
		slog.Error("application runtime lifecycle is not initialized")
		return
	}
	if err := a.runtimeLifecycle.Shutdown(); err != nil {
		slog.Error("application runtime shutdown did not finish within the Wails callback", "error", err)
	}
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}

func (app *App) Add(a, b int) int {
	return a + b
}
