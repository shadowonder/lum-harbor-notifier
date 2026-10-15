package applicationruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/credential"
	"lum-harbor-notifier/internal/openai"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/aiadapter"
)

// ErrBuiltInAINotConfigured is returned by the production built-in provider
// seam until a real built-in provider and its credential boundary exist.
var ErrBuiltInAINotConfigured = errors.New("built-in AI provider is not configured")

const (
	AIRuntimeReady       = "READY"
	AIRuntimeUnavailable = "UNAVAILABLE"
	AIRuntimeConflict    = "CONFLICT"
)

// AIRuntimeSnapshotter is the narrow live process-state dependency used by
// the AI control-plane projection.
type AIRuntimeSnapshotter interface {
	Snapshot() plugin.RuntimeSnapshot
}

// AIStageProjection is one stage that is actually registered in the central
// AI runtime. Available/Enabled are runtime facts, not manifest claims.
type AIStageProjection struct {
	ID                string
	Name              string
	Stage             string
	Order             int
	Actions           []string
	AllActions        bool
	Enabled           bool
	Available         bool
	ExtensionID       string
	ExtensionName     string
	InstanceID        string
	UnavailableReason string
}

// AIProjection is the application-owned answer to "what AI runtime does this
// process actually have right now?".
type AIProjection struct {
	Status                     string
	Issue                      string
	Mode                       ai.RuntimeMode
	EffectiveProviderID        string
	EffectiveProviderName      string
	ProviderSource             ai.ProviderSource
	BuiltInConfigEditable      bool
	ManagingExtensionID        string
	ManagingExtensionName      string
	ManagingProviderInstanceID string
	Stages                     []AIStageProjection
}

type composedAIStage struct {
	projection AIStageProjection
	binding    plugin.BindingDeclaration
}

// AIComposition owns the one production ai.Service and the control-plane
// facts derived from the exact same startup selection. Service is nil only
// when configuration is ambiguous and execution must be blocked.
type AIComposition struct {
	Service *ai.Service

	runtime        AIRuntimeSnapshotter
	base           AIProjection
	stages         []AIStageProjection
	providerPlugin bool
}

// ComposeAI builds the production central AI runtime from enabled, validated
// Plugin Instances and their manifest bindings. It never silently chooses one
// of multiple global provider candidates.
func ComposeAI(ctx context.Context, registry *plugin.Registry, instances []plugin.Instance, invoker plugin.Invoker, runtime AIRuntimeSnapshotter, builtInConfig config.BuiltInOpenAIConfig, credentials credential.OpenAIStore) (*AIComposition, error) {
	if ctx == nil {
		return nil, fmt.Errorf("compose AI runtime: context must not be nil")
	}
	if registry == nil {
		return nil, fmt.Errorf("compose AI runtime: plugin registry must not be nil")
	}
	if invoker == nil {
		return nil, fmt.Errorf("compose AI runtime: plugin invoker must not be nil")
	}
	if runtime == nil {
		return nil, fmt.Errorf("compose AI runtime: runtime snapshotter must not be nil")
	}
	if credentials == nil {
		return nil, fmt.Errorf("compose AI runtime: credential store must not be nil")
	}

	builtIn := ai.ProviderRegistration{
		ID: "builtin-openai", Name: "Built-in OpenAI", Provider: unavailableBuiltInProvider{},
	}
	builtInReady := false
	builtInIssue := "Built-in OpenAI configuration is missing."
	if builtInConfig.Configured() {
		secret, exists, err := credentials.Resolve(ctx)
		switch {
		case err != nil:
			builtInIssue = "Built-in OpenAI credential could not be resolved from secure storage."
		case !exists || strings.TrimSpace(secret) == "":
			builtInIssue = "Built-in OpenAI credential is missing."
		default:
			provider, providerErr := openai.NewProvider(openai.Config{
				Endpoint: builtInConfig.Endpoint, Model: builtInConfig.Model, Credential: secret,
				Timeout: time.Duration(builtInConfig.RequestTimeoutSeconds) * time.Second,
			})
			if providerErr != nil {
				builtInIssue = "Built-in OpenAI configuration is invalid."
			} else {
				builtIn.Provider = provider
				builtInReady = true
				builtInIssue = ""
			}
		}
	}
	composition := &AIComposition{
		runtime: runtime,
		base: AIProjection{
			Status: AIRuntimeUnavailable, Issue: builtInIssue,
			Mode: ai.RuntimeModeBuiltIn, EffectiveProviderID: builtIn.ID,
			EffectiveProviderName: builtIn.Name, ProviderSource: ai.ProviderSourceBuiltIn,
			BuiltInConfigEditable: true,
		},
	}
	if builtInReady {
		composition.base.Status = AIRuntimeReady
	}

	type providerCandidate struct {
		instance  plugin.Instance
		extension plugin.Extension
		binding   plugin.BindingDeclaration
	}
	var providers []providerCandidate
	var runtimes []providerCandidate
	var preStages, postStages []composedAIStage
	for _, instance := range instances {
		if !plugin.SupportsCapability(instance.CapabilityID) {
			continue
		}
		extension, err := registry.Find(instance.ExtensionID)
		if err != nil {
			return nil, fmt.Errorf("compose AI runtime instance %q: %w", instance.ID, err)
		}
		for _, binding := range extension.Manifest.Bindings {
			if binding.Capability != instance.CapabilityID {
				continue
			}
			switch binding.Type {
			case plugin.BindingAIRuntime:
				runtimes = append(runtimes, providerCandidate{instance: instance, extension: extension, binding: binding})
			case plugin.BindingAIProvider:
				providers = append(providers, providerCandidate{instance: instance, extension: extension, binding: binding})
			case plugin.BindingAIPre:
				preStages = append(preStages, newComposedAIStage("PRE", extension, instance, binding))
			case plugin.BindingAIPost:
				postStages = append(postStages, newComposedAIStage("POST", extension, instance, binding))
			}
		}
	}
	if len(runtimes) > 1 {
		names := make([]string, 0, len(runtimes))
		for _, candidate := range runtimes {
			names = append(names, fmt.Sprintf("%s (%s)", candidate.extension.Manifest.Name, candidate.instance.ID))
		}
		sort.Strings(names)
		composition.base = AIProjection{Status: AIRuntimeConflict, Issue: fmt.Sprintf("Multiple enabled AI Runtime instances claim the global runtime slot: %s.", strings.Join(names, ", ")), Mode: ai.RuntimeModePluginRuntime, ProviderSource: ai.ProviderSourcePlugin, BuiltInConfigEditable: false}
		return composition, nil
	}
	if len(runtimes) == 1 && len(providers) > 0 {
		composition.base = AIProjection{Status: AIRuntimeConflict, Issue: "A Plugin AI Runtime and a legacy Plugin model provider cannot both own AI execution.", Mode: ai.RuntimeModePluginRuntime, ProviderSource: ai.ProviderSourcePlugin, BuiltInConfigEditable: false}
		return composition, nil
	}

	if len(providers) > 1 {
		names := make([]string, 0, len(providers))
		for _, candidate := range providers {
			names = append(names, fmt.Sprintf("%s (%s)", candidate.extension.Manifest.Name, candidate.instance.ID))
		}
		sort.Strings(names)
		composition.base = AIProjection{
			Status: AIRuntimeConflict,
			Issue:  fmt.Sprintf("Multiple enabled AI provider instances claim the global provider slot: %s.", strings.Join(names, ", ")),
			Mode:   ai.RuntimeModePluginProvider, ProviderSource: ai.ProviderSourcePlugin,
			BuiltInConfigEditable: false,
		}
		return composition, nil
	}

	sortComposedStages(preStages)
	sortComposedStages(postStages)
	config := ai.Config{BuiltInProvider: builtIn}
	for _, stage := range preStages {
		extension, _ := registry.Find(stage.projection.ExtensionID)
		instance := findInstance(instances, stage.projection.InstanceID)
		bindings := aiadapter.PreBindings(extension, invoker, instance)
		for _, binding := range bindings {
			if binding.ID == stage.binding.ID {
				binding.ID = stage.projection.ID
				config.PreMiddleware = append(config.PreMiddleware, binding)
				composition.stages = append(composition.stages, stage.projection)
				break
			}
		}
	}

	if len(providers) == 1 {
		candidate := providers[0]
		registration, err := aiadapter.ProviderRegistration(candidate.extension, invoker, &candidate.instance)
		if err != nil {
			return nil, fmt.Errorf("compose AI provider instance %q: %w", candidate.instance.ID, err)
		}
		registration.ID = runtimeIdentity(candidate.extension.Manifest.ID, candidate.instance.ID, candidate.binding.ID)
		config.ReplacementProvider = &registration
		composition.providerPlugin = true
		status := AIRuntimeReady
		issue := ""
		if issueMsg := validateProviderInstanceConfig(candidate.extension, candidate.instance); issueMsg != "" {
			status = AIRuntimeUnavailable
			issue = issueMsg
		}
		composition.base = AIProjection{
			Status: status, Issue: issue, Mode: ai.RuntimeModePluginProvider,
			EffectiveProviderID: registration.ID, EffectiveProviderName: registration.Name,
			ProviderSource: ai.ProviderSourcePlugin, BuiltInConfigEditable: false,
			ManagingExtensionID:        candidate.extension.Manifest.ID,
			ManagingExtensionName:      candidate.extension.Manifest.Name,
			ManagingProviderInstanceID: candidate.instance.ID,
		}
		stageAvailable := status == AIRuntimeReady
		composition.stages = append(composition.stages, AIStageProjection{
			ID: registration.ID, Name: candidate.extension.Manifest.Name, Stage: "PROVIDER",
			Order: candidate.binding.Order, Actions: append([]string(nil), candidate.binding.Actions...),
			AllActions: isAllActions(candidate.binding.Actions), Enabled: true, Available: stageAvailable,
			UnavailableReason: issue,
			ExtensionID:       candidate.extension.Manifest.ID, ExtensionName: candidate.extension.Manifest.Name,
			InstanceID: candidate.instance.ID,
		})
	} else if len(runtimes) == 0 {
		composition.stages = append(composition.stages, AIStageProjection{
			ID: builtIn.ID, Name: builtIn.Name, Stage: "PROVIDER", AllActions: true,
			Enabled: builtInReady, Available: builtInReady, UnavailableReason: composition.base.Issue,
		})
	}

	for _, stage := range postStages {
		extension, _ := registry.Find(stage.projection.ExtensionID)
		instance := findInstance(instances, stage.projection.InstanceID)
		bindings := aiadapter.PostBindings(extension, invoker, instance)
		for _, binding := range bindings {
			if binding.ID == stage.binding.ID {
				binding.ID = stage.projection.ID
				config.PostMiddleware = append(config.PostMiddleware, binding)
				composition.stages = append(composition.stages, stage.projection)
				break
			}
		}
	}

	service, err := ai.NewService(config)
	if err != nil {
		return nil, fmt.Errorf("compose central AI service: %w", err)
	}
	composition.Service = service
	if len(runtimes) == 1 {
		candidate := runtimes[0]
		composition.providerPlugin = true
		composition.base = AIProjection{
			Status: AIRuntimeReady, Mode: ai.RuntimeModePluginRuntime,
			EffectiveProviderID:   runtimeIdentity(candidate.extension.Manifest.ID, candidate.instance.ID, candidate.binding.ID),
			EffectiveProviderName: candidate.extension.Manifest.Name, ProviderSource: ai.ProviderSourcePlugin,
			BuiltInConfigEditable: false, ManagingExtensionID: candidate.extension.Manifest.ID,
			ManagingExtensionName: candidate.extension.Manifest.Name, ManagingProviderInstanceID: candidate.instance.ID,
		}
		composition.stages = append(composition.stages, AIStageProjection{
			ID: composition.base.EffectiveProviderID, Name: candidate.extension.Manifest.Name, Stage: "RUNTIME",
			Order: candidate.binding.Order, Actions: append([]string(nil), candidate.binding.Actions...), AllActions: isAllActions(candidate.binding.Actions),
			Enabled: true, Available: true, ExtensionID: candidate.extension.Manifest.ID, ExtensionName: candidate.extension.Manifest.Name, InstanceID: candidate.instance.ID,
		})
	}
	return composition, nil
}

// Execute delegates to the exact immutable Service composed for this process.
// A provider conflict deliberately has no Service and cannot be verified.
func (c *AIComposition) Execute(ctx context.Context, request ai.Request) (ai.Response, error) {
	if c.Service == nil {
		return ai.Response{}, fmt.Errorf("AI runtime is unavailable: %s", c.base.Issue)
	}
	return c.Service.Execute(ctx, request)
}

func (c *AIComposition) Projection() AIProjection {
	projection := c.base
	projection.Stages = cloneAIStages(c.stages)
	if projection.Status == AIRuntimeConflict {
		return projection
	}
	running := make(map[string]bool)
	for _, extension := range c.runtime.Snapshot().Extensions {
		running[extension.ID] = extension.Running
	}
	for index := range projection.Stages {
		stage := &projection.Stages[index]
		if stage.ExtensionID == "" {
			continue
		}
		stage.Available = running[stage.ExtensionID]
		stage.Enabled = stage.Available
		if !stage.Available {
			stage.UnavailableReason = "Extension process is unavailable."
		}
	}
	if c.providerPlugin && !running[projection.ManagingExtensionID] {
		projection.Status = AIRuntimeUnavailable
		projection.Issue = fmt.Sprintf("Selected AI provider extension %q is unavailable.", projection.ManagingExtensionName)
	}
	return projection
}

type unavailableBuiltInProvider struct{}

func (unavailableBuiltInProvider) Execute(context.Context, ai.Request) (ai.Response, error) {
	return ai.Response{}, ErrBuiltInAINotConfigured
}

func newComposedAIStage(stage string, extension plugin.Extension, instance plugin.Instance, binding plugin.BindingDeclaration) composedAIStage {
	return composedAIStage{
		binding: binding,
		projection: AIStageProjection{
			ID: runtimeIdentity(extension.Manifest.ID, instance.ID, binding.ID), Name: binding.ID,
			Stage: stage, Order: binding.Order, Actions: append([]string(nil), binding.Actions...),
			AllActions: isAllActions(binding.Actions), Enabled: true, Available: true,
			ExtensionID: extension.Manifest.ID, ExtensionName: extension.Manifest.Name, InstanceID: instance.ID,
		},
	}
}

func runtimeIdentity(extensionID, instanceID, bindingID string) string {
	return extensionID + ":" + instanceID + ":" + bindingID
}

func isAllActions(actions []string) bool {
	return len(actions) == 1 && actions[0] == "*"
}

func sortComposedStages(stages []composedAIStage) {
	sort.SliceStable(stages, func(i, j int) bool {
		if stages[i].binding.Order != stages[j].binding.Order {
			return stages[i].binding.Order < stages[j].binding.Order
		}
		return stages[i].projection.ID < stages[j].projection.ID
	})
}

func findInstance(instances []plugin.Instance, id string) *plugin.Instance {
	for index := range instances {
		if instances[index].ID == id {
			instance := instances[index]
			return &instance
		}
	}
	return nil
}

func cloneAIStages(stages []AIStageProjection) []AIStageProjection {
	cloned := make([]AIStageProjection, len(stages))
	copy(cloned, stages)
	for index := range cloned {
		cloned[index].Actions = append([]string(nil), stages[index].Actions...)
	}
	return cloned
}

func validateProviderInstanceConfig(extension plugin.Extension, instance plugin.Instance) string {
	if extension.Manifest.ID == "lh-plugin-ai-runtime" {
		if len(instance.Config) == 0 {
			return fmt.Sprintf("%s configuration error: base_url and model are required when mock_mode is false.", extension.Manifest.Name)
		}
		var cfg struct {
			BaseURL  string `json:"base_url"`
			Endpoint string `json:"endpoint"`
			Model    string `json:"model"`
			MockMode bool   `json:"mock_mode"`
		}
		if err := json.Unmarshal(instance.Config, &cfg); err != nil {
			return fmt.Sprintf("%s configuration error: malformed JSON.", extension.Manifest.Name)
		}
		if cfg.MockMode {
			return ""
		}
		endpoint := strings.TrimSpace(cfg.BaseURL)
		if endpoint == "" {
			endpoint = strings.TrimSpace(cfg.Endpoint)
		}
		missingBaseURL := endpoint == ""
		missingModel := strings.TrimSpace(cfg.Model) == ""
		if missingBaseURL && missingModel {
			return fmt.Sprintf("%s configuration error: base_url and model are required when mock_mode is false.", extension.Manifest.Name)
		}
		if missingBaseURL {
			return fmt.Sprintf("%s configuration error: base_url is required when mock_mode is false.", extension.Manifest.Name)
		}
		if missingModel {
			return fmt.Sprintf("%s configuration error: model is required when mock_mode is false.", extension.Manifest.Name)
		}
	}
	return ""
}
