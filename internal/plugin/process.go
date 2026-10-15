package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const maxRPCFrameBytes = 4 * 1024 * 1024

type processStatus string

const (
	processStarting  processStatus = "STARTING"
	processRunning   processStatus = "RUNNING"
	processStopping  processStatus = "STOPPING"
	processStopped   processStatus = "STOPPED"
	processUnhealthy processStatus = "UNHEALTHY"
)

type callResult struct {
	result json.RawMessage
	err    error
}

type pluginProcess struct {
	extension Extension
	logger    *slog.Logger

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	writeMu sync.Mutex
	mu      sync.Mutex
	status  processStatus
	pending map[string]chan callResult
	retired retiredRequestTombstones
	lastErr error

	requestSequence atomic.Uint64
	done            chan struct{}
	doneOnce        sync.Once
	ioWG            sync.WaitGroup
}

func startPluginProcess(
	ctx context.Context,
	extension Extension,
	startupTimeout time.Duration,
	logger *slog.Logger,
) (*pluginProcess, error) {
	cmd := exec.CommandContext(context.Background(), extension.ExecutablePath)
	cmd.Dir = extension.Directory

	// Derive canonical plugin data and cache directories
	pluginRoot := filepath.Dir(extension.Directory)
	appRoot := filepath.Dir(pluginRoot)
	dataDir := filepath.Join(appRoot, "plugin-data", extension.Manifest.ID)
	cacheDir := filepath.Join(appRoot, "plugin-cache", extension.Manifest.ID)

	if err := preparePluginOwnedDirectory(dataDir); err != nil {
		return nil, fmt.Errorf("%w: extension %q data directory: %v", ErrProcessStart, extension.Manifest.ID, err)
	}
	if err := preparePluginOwnedDirectory(cacheDir); err != nil {
		return nil, fmt.Errorf("%w: extension %q cache directory: %v", ErrProcessStart, extension.Manifest.ID, err)
	}

	cmd.Env = append(os.Environ(),
		"LUM_HARBOR_PLUGIN_ID="+extension.Manifest.ID,
		"LUM_HARBOR_PLUGIN_VERSION="+extension.Manifest.Version,
		"LUM_HARBOR_PLUGIN_INSTALL_DIR="+extension.Directory,
		"LUM_HARBOR_PLUGIN_DATA_DIR="+dataDir,
		"LUM_HARBOR_PLUGIN_CACHE_DIR="+cacheDir,
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("%w: extension %q stdin: %v", ErrProcessStart, extension.Manifest.ID, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("%w: extension %q stdout: %v", ErrProcessStart, extension.Manifest.ID, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("%w: extension %q stderr: %v", ErrProcessStart, extension.Manifest.ID, err)
	}
	process := &pluginProcess{
		extension: extension,
		logger:    logger,
		cmd:       cmd,
		stdin:     stdin,
		stdout:    stdout,
		stderr:    stderr,
		status:    processStarting,
		pending:   make(map[string]chan callResult),
		done:      make(chan struct{}),
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%w: extension %q: %v", ErrProcessStart, extension.Manifest.ID, err)
	}
	process.ioWG.Add(2)
	go process.readResponses()
	go process.readStderr()
	go process.waitForExit()

	handshakeCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	request := HandshakeRequest{
		ProtocolVersion:  SupportedProtocolVersion,
		ExtensionID:      extension.Manifest.ID,
		ExtensionVersion: extension.Manifest.Version,
		Capabilities:     extension.Manifest.CapabilityIDs(),
	}
	params, _ := json.Marshal(request)
	result, err := process.invoke(handshakeCtx, MethodHandshake, params, true)
	if err != nil {
		process.fail(fmt.Errorf("%w: extension %q: %v", ErrHandshake, extension.Manifest.ID, err), true)
		<-process.done
		return nil, fmt.Errorf("%w: extension %q: %v", ErrHandshake, extension.Manifest.ID, err)
	}
	var handshake HandshakeResult
	if err := json.Unmarshal(result, &handshake); err != nil {
		process.fail(fmt.Errorf("%w: extension %q decode result: %v", ErrHandshake, extension.Manifest.ID, err), true)
		<-process.done
		return nil, fmt.Errorf("%w: extension %q decode result: %v", ErrHandshake, extension.Manifest.ID, err)
	}
	if err := validateHandshake(extension.Manifest, handshake); err != nil {
		process.fail(err, true)
		<-process.done
		return nil, err
	}
	process.mu.Lock()
	if process.status == processStarting {
		process.status = processRunning
	}
	process.mu.Unlock()
	return process, nil
}

func preparePluginOwnedDirectory(path string) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("%q must be a regular directory", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect %q: %w", path, err)
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create %q: %w", path, err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure %q: %w", path, err)
	}
	return nil
}

func validateHandshake(manifest Manifest, result HandshakeResult) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("%w: extension %q: %s", ErrHandshake, manifest.ID, fmt.Sprintf(format, args...))
	}
	if result.ProtocolVersion != manifest.ProtocolVersion {
		return fail("protocol version %d does not match %d", result.ProtocolVersion, manifest.ProtocolVersion)
	}
	if result.ExtensionID != manifest.ID {
		return fail("runtime id %q does not match manifest", result.ExtensionID)
	}
	if result.ExtensionVersion != manifest.Version {
		return fail("runtime version %q does not match manifest", result.ExtensionVersion)
	}
	want := manifest.CapabilityIDs()
	got := append([]CapabilityID(nil), result.Capabilities...)
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	if len(want) != len(got) {
		return fail("runtime capabilities do not match manifest")
	}
	for index := range want {
		if want[index] != got[index] {
			return fail("runtime capabilities do not match manifest")
		}
	}
	return nil
}

func (p *pluginProcess) invoke(ctx context.Context, method string, params json.RawMessage, allowStarting bool) (json.RawMessage, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: extension %q nil context", ErrRPCCancelled, p.extension.Manifest.ID)
	}
	id := fmt.Sprintf("%s-%d", p.extension.Manifest.ID, p.requestSequence.Add(1))
	responseCh := make(chan callResult, 1)
	p.mu.Lock()
	p.retired.cleanup(time.Now())
	allowed := p.status == processRunning || (allowStarting && p.status == processStarting)
	if !allowed {
		err := p.lastErr
		if err == nil {
			err = ErrHostShuttingDown
		}
		p.mu.Unlock()
		return nil, fmt.Errorf("extension %q request %q: %w", p.extension.Manifest.ID, id, err)
	}
	p.pending[id] = responseCh
	p.mu.Unlock()

	request := rpcRequest{ID: id, Method: method, Params: params}
	if err := p.writeRequest(request); err != nil {
		p.removePending(id, false)
		p.fail(fmt.Errorf("%w: extension %q request %q write: %v", ErrProtocol, p.extension.Manifest.ID, id, err), true)
		return nil, fmt.Errorf("%w: extension %q request %q write: %v", ErrProtocol, p.extension.Manifest.ID, id, err)
	}

	select {
	case outcome := <-responseCh:
		return outcome.result, outcome.err
	case <-ctx.Done():
		if p.removePending(id, true) {
			return nil, fmt.Errorf("%w: extension %q request %q: %w", ErrRPCCancelled, p.extension.Manifest.ID, id, ctx.Err())
		}
		select {
		case outcome := <-responseCh:
			return outcome.result, outcome.err
		default:
			return nil, fmt.Errorf("%w: extension %q request %q: %w", ErrRPCCancelled, p.extension.Manifest.ID, id, ctx.Err())
		}
	}
}

func (p *pluginProcess) writeRequest(request rpcRequest) error {
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	if _, err := p.stdin.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func (p *pluginProcess) removePending(id string, retire bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.pending[id]; !exists {
		return false
	}
	delete(p.pending, id)
	if retire {
		p.retired.retire(id, time.Now())
	}
	return true
}

func (p *pluginProcess) readResponses() {
	defer p.ioWG.Done()
	scanner := bufio.NewScanner(p.stdout)
	scanner.Buffer(make([]byte, 64*1024), maxRPCFrameBytes)
	for scanner.Scan() {
		response, err := decodeRPCResponse(scanner.Bytes())
		if err != nil {
			p.fail(fmt.Errorf("%w: extension %q stdout: %v", ErrProtocol, p.extension.Manifest.ID, err), true)
			return
		}
		if err := p.handleResponse(response, time.Now()); err != nil {
			p.fail(err, true)
			return
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		p.fail(fmt.Errorf("%w: extension %q stdout read: %v", ErrProtocol, p.extension.Manifest.ID, err), true)
	}
}

func (p *pluginProcess) handleResponse(response rpcResponse, now time.Time) error {
	p.mu.Lock()
	p.retired.cleanup(now)
	responseCh, exists := p.pending[response.ID]
	if exists {
		delete(p.pending, response.ID)
		p.mu.Unlock()
		if response.Error != nil {
			responseCh <- callResult{err: fmt.Errorf("%w: extension %q request %q: %w", ErrRemote, p.extension.Manifest.ID, response.ID, response.Error)}
		} else {
			responseCh <- callResult{result: append(json.RawMessage(nil), response.Result...)}
		}
		return nil
	}
	if p.retired.consume(response.ID, now) {
		p.mu.Unlock()
		if p.logger != nil {
			p.logger.Debug("discarded late plugin response", "extension_id", p.extension.Manifest.ID, "request_id", response.ID)
		}
		return nil
	}
	p.mu.Unlock()
	return fmt.Errorf("%w: extension %q unknown response id %q", ErrProtocol, p.extension.Manifest.ID, response.ID)
}

func (p *pluginProcess) readStderr() {
	defer p.ioWG.Done()
	scanner := bufio.NewScanner(p.stderr)
	for scanner.Scan() {
		p.logger.Info("plugin stderr", "extension_id", p.extension.Manifest.ID, "message", scanner.Text())
	}
}

func (p *pluginProcess) waitForExit() {
	err := p.cmd.Wait()
	p.ioWG.Wait()
	p.mu.Lock()
	status := p.status
	p.mu.Unlock()
	if status == processStopping && (err == nil || isExitAfterKill(err)) {
		p.finish(processStopped, nil)
		return
	}
	if err == nil {
		err = errors.New("process exited")
	}
	p.finish(processUnhealthy, fmt.Errorf("extension %q process exited unexpectedly: %w", p.extension.Manifest.ID, err))
}

func isExitAfterKill(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}

func (p *pluginProcess) fail(err error, kill bool) {
	p.mu.Lock()
	if p.status == processStopped || p.status == processUnhealthy {
		p.mu.Unlock()
		return
	}
	p.status = processUnhealthy
	p.lastErr = err
	pending := p.takePendingLocked(false)
	p.mu.Unlock()
	for _, responseCh := range pending {
		responseCh <- callResult{err: err}
	}
	if kill && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
}

func (p *pluginProcess) finish(status processStatus, err error) {
	p.mu.Lock()
	if p.status != processUnhealthy {
		p.status = status
		p.lastErr = err
	} else if p.lastErr == nil {
		p.lastErr = err
	}
	pending := p.takePendingLocked(false)
	p.retired.clear()
	terminalErr := p.lastErr
	if terminalErr == nil && status != processStopped {
		terminalErr = errors.New("plugin process stopped")
	}
	p.mu.Unlock()
	for _, responseCh := range pending {
		responseCh <- callResult{err: terminalErr}
	}
	p.doneOnce.Do(func() { close(p.done) })
}

func (p *pluginProcess) takePendingLocked(retire bool) []chan callResult {
	values := make([]chan callResult, 0, len(p.pending))
	now := time.Now()
	for id, responseCh := range p.pending {
		delete(p.pending, id)
		if retire {
			p.retired.retire(id, now)
		}
		values = append(values, responseCh)
	}
	return values
}

func (p *pluginProcess) shutdown(timeout time.Duration) error {
	p.mu.Lock()
	if p.status == processStopped {
		p.mu.Unlock()
		return nil
	}
	if p.status == processUnhealthy {
		done := p.done
		p.mu.Unlock()
		select {
		case <-done:
			return nil
		case <-time.After(timeout):
			if p.cmd.Process != nil {
				_ = p.cmd.Process.Kill()
			}
			<-done
			return nil
		}
	}
	p.status = processStopping
	pending := p.takePendingLocked(true)
	p.mu.Unlock()
	shutdownErr := fmt.Errorf("%w: extension %q", ErrHostShuttingDown, p.extension.Manifest.ID)
	for _, responseCh := range pending {
		responseCh <- callResult{err: shutdownErr}
	}

	shutdownID := fmt.Sprintf("%s-shutdown-%d", p.extension.Manifest.ID, p.requestSequence.Add(1))
	shutdownCh := make(chan callResult, 1)
	p.mu.Lock()
	p.pending[shutdownID] = shutdownCh
	p.mu.Unlock()
	_ = p.writeRequest(rpcRequest{ID: shutdownID, Method: MethodShutdown, Params: json.RawMessage(`{}`)})

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-p.done:
		return nil
	case <-timer.C:
		p.removePending(shutdownID, true)
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		<-p.done
		return nil
	}
}

func (p *pluginProcess) snapshot() processSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	lastError := ""
	if p.lastErr != nil {
		lastError = p.lastErr.Error()
	}
	return processSnapshot{status: p.status, lastError: lastError}
}
