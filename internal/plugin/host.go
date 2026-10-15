package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"
)

type HostOptions struct {
	StartupTimeout  time.Duration
	RequestTimeout  time.Duration
	ShutdownTimeout time.Duration
	Logger          *slog.Logger
}

type Invocation struct {
	ExtensionID  string
	CapabilityID CapabilityID
	Instance     *Instance
	Input        json.RawMessage
}

type Invoker interface {
	Invoke(ctx context.Context, invocation Invocation) (json.RawMessage, error)
}

type Host struct {
	registry *Registry
	options  HostOptions

	mu           sync.RWMutex
	processes    map[string]*pluginProcess
	shuttingDown bool
}

func NewHost(registry *Registry, options HostOptions) (*Host, error) {
	if registry == nil {
		return nil, fmt.Errorf("plugin registry must not be nil")
	}
	if options.StartupTimeout <= 0 || options.RequestTimeout <= 0 || options.ShutdownTimeout <= 0 {
		return nil, fmt.Errorf("plugin host timeouts must be greater than zero")
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	return &Host{registry: registry, options: options, processes: make(map[string]*pluginProcess)}, nil
}

func (h *Host) Start(ctx context.Context, extensionID string) error {
	if ctx == nil {
		return fmt.Errorf("start extension %q: context must not be nil", extensionID)
	}
	h.mu.Lock()
	if h.shuttingDown {
		h.mu.Unlock()
		return ErrHostShuttingDown
	}
	if _, exists := h.processes[extensionID]; exists {
		h.mu.Unlock()
		return nil
	}
	extension, err := h.registry.Find(extensionID)
	if err != nil {
		h.mu.Unlock()
		return err
	}
	process, err := startPluginProcess(ctx, extension, h.options.StartupTimeout, h.options.Logger)
	if err != nil {
		h.mu.Unlock()
		return err
	}
	h.processes[extensionID] = process
	h.mu.Unlock()
	return nil
}

var hostSupportedCapabilities = map[CapabilityID]struct{}{
	CapabilitySourceFetch:         {},
	CapabilityAIPreRequest:        {},
	CapabilityAIProvider:          {},
	CapabilityAIRuntime:           {},
	CapabilityAIPostResponse:      {},
	CapabilityReadQuery:           {},
	CapabilityNotificationDeliver: {},
}

// SupportsCapability reports whether Plugin Runtime has a typed host contract
// for a manifest capability. Unknown capabilities remain discoverable but
// cannot be selected for production use.
func SupportsCapability(capabilityID CapabilityID) bool {
	_, supported := hostSupportedCapabilities[capabilityID]
	return supported
}

func (h *Host) Invoke(ctx context.Context, invocation Invocation) (json.RawMessage, error) {
	if ctx == nil {
		return nil, fmt.Errorf("invoke extension %q: context must not be nil", invocation.ExtensionID)
	}
	if !SupportsCapability(invocation.CapabilityID) {
		return nil, fmt.Errorf("%w: capability %q", ErrCapabilityUnsupported, invocation.CapabilityID)
	}
	if _, err := h.registry.ResolveCapability(invocation.ExtensionID, invocation.CapabilityID); err != nil {
		return nil, err
	}
	params := CapabilityInvokeParams{CapabilityID: invocation.CapabilityID, Input: invocation.Input}
	if invocation.Instance != nil {
		if invocation.Instance.ExtensionID != invocation.ExtensionID || invocation.Instance.CapabilityID != invocation.CapabilityID {
			return nil, fmt.Errorf("%w: instance %q does not match extension/capability", ErrInstanceNotFound, invocation.Instance.ID)
		}
		if err := h.registry.ValidateInstance(*invocation.Instance); err != nil {
			return nil, err
		}
		params.InstanceID = invocation.Instance.ID
		params.Config = append(json.RawMessage(nil), invocation.Instance.Config...)
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("encode extension %q capability %q invocation: %w", invocation.ExtensionID, invocation.CapabilityID, err)
	}
	h.mu.RLock()
	if h.shuttingDown {
		h.mu.RUnlock()
		return nil, ErrHostShuttingDown
	}
	process := h.processes[invocation.ExtensionID]
	h.mu.RUnlock()
	if process == nil {
		return nil, fmt.Errorf("%w: extension %q is not started", ErrProcessStart, invocation.ExtensionID)
	}
	invokeCtx := ctx
	cancel := func() {}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		invokeCtx, cancel = context.WithTimeout(ctx, h.options.RequestTimeout)
	}
	defer cancel()
	result, err := process.invoke(invokeCtx, MethodCapabilityInvoke, encoded, false)
	if err != nil {
		return nil, fmt.Errorf("extension %q capability %q: %w", invocation.ExtensionID, invocation.CapabilityID, err)
	}
	return result, nil
}

func (h *Host) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("shutdown plugin host: context must not be nil")
	}
	h.mu.Lock()
	if h.shuttingDown {
		h.mu.Unlock()
		return nil
	}
	h.shuttingDown = true
	processes := make([]*pluginProcess, 0, len(h.processes))
	for _, process := range h.processes {
		processes = append(processes, process)
	}
	h.mu.Unlock()

	deadline := h.options.ShutdownTimeout
	if ctx.Err() != nil {
		deadline = time.Millisecond
	}
	if value, ok := ctx.Deadline(); ok {
		remaining := time.Until(value)
		if remaining < deadline {
			deadline = remaining
		}
	}
	if deadline <= 0 {
		deadline = time.Millisecond
	}
	results := make(chan error, len(processes))
	for _, process := range processes {
		go func(process *pluginProcess) {
			results <- process.shutdown(deadline)
		}(process)
	}
	var firstErr error
	for range processes {
		if err := <-results; err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

type RuntimeSnapshot struct {
	Extensions []ExtensionRuntimeSnapshot
}

type ExtensionRuntimeSnapshot struct {
	ID              string
	Name            string
	Version         string
	ProtocolVersion int
	Status          string
	Running         bool
	Capabilities    []CapabilityID
	Bindings        []BindingSnapshot
	LastError       string
}

type BindingSnapshot struct {
	ID         string
	Type       BindingType
	Capability CapabilityID
	Actions    []string
	Order      int
}

type processSnapshot struct {
	status    processStatus
	lastError string
}

func (h *Host) Snapshot() RuntimeSnapshot {
	h.mu.RLock()
	processes := make(map[string]*pluginProcess, len(h.processes))
	for id, process := range h.processes {
		processes[id] = process
	}
	h.mu.RUnlock()
	result := RuntimeSnapshot{}
	for _, extension := range h.registry.List() {
		snapshot := ExtensionRuntimeSnapshot{
			ID: extension.Manifest.ID, Name: extension.Manifest.Name, Version: extension.Manifest.Version,
			ProtocolVersion: extension.Manifest.ProtocolVersion, Status: "DISCOVERED",
			Capabilities: extension.Manifest.CapabilityIDs(),
		}
		for _, binding := range extension.Manifest.Bindings {
			snapshot.Bindings = append(snapshot.Bindings, BindingSnapshot{
				ID: binding.ID, Type: binding.Type, Capability: binding.Capability,
				Actions: append([]string(nil), binding.Actions...), Order: binding.Order,
			})
		}
		if process := processes[extension.Manifest.ID]; process != nil {
			state := process.snapshot()
			snapshot.Status = string(state.status)
			snapshot.Running = state.status == processRunning
			snapshot.LastError = state.lastError
		}
		result.Extensions = append(result.Extensions, snapshot)
	}
	sort.Slice(result.Extensions, func(i, j int) bool { return result.Extensions[i].ID < result.Extensions[j].ID })
	return result
}
