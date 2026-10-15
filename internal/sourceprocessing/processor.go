package sourceprocessing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/ailineage"
	"lum-harbor-notifier/internal/airuntime"
	"lum-harbor-notifier/internal/identity"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/source"
)

const lineageFinalizeTimeout = 5 * time.Second

// Processor coordinates exact-revision reads, canonical AI execution, and
// durable run lineage. All per-attempt state is local to ProcessRevision.
type Processor struct {
	aiService *ai.Service
	aiRuntime airuntime.Runtime
	source    *source.Repository
	lineage   *ailineage.Service
	newID     func() (string, error)
	now       func() time.Time
}

// SetRuntime installs the high-level runtime used for both Source
// normalization and semantic interpretation.
func (p *Processor) SetRuntime(runtime airuntime.Runtime) *Processor {
	p.aiRuntime = runtime
	return p
}

func NewProcessor(db *sql.DB, aiService *ai.Service) *Processor {
	processor := &Processor{
		aiService: aiService, newID: identity.New,
		now: func() time.Time { return time.Now().UTC() },
	}
	if db != nil {
		processor.source = source.NewRepository(db)
		processor.lineage = ailineage.NewService(db)
	}
	return processor
}

func (p *Processor) ProcessRevision(ctx context.Context, request ProcessRequest) (ProcessingResult, error) {
	if ctx == nil {
		return ProcessingResult{}, fmt.Errorf("source processing context must not be nil")
	}
	if p == nil || p.aiService == nil || p.source == nil || p.lineage == nil {
		return ProcessingResult{}, fmt.Errorf("source processor dependencies must not be nil")
	}
	if strings.TrimSpace(request.SourceRevisionID) == "" {
		return ProcessingResult{}, fmt.Errorf("source revision ID must not be empty")
	}
	contextSnapshot, err := canonicalContext(request.InterpretationContext)
	if err != nil {
		return ProcessingResult{}, err
	}
	revision, err := p.source.GetRevision(ctx, request.SourceRevisionID)
	if err != nil {
		return ProcessingResult{}, fmt.Errorf("load exact source revision %s: %w", request.SourceRevisionID, err)
	}
	record, err := p.source.GetRecord(ctx, revision.SourceRecordID)
	if err != nil {
		return ProcessingResult{}, fmt.Errorf("load source identity for revision %s: %w", request.SourceRevisionID, err)
	}

	processingID, err := p.nextID("processing")
	if err != nil {
		return ProcessingResult{}, err
	}
	cleanRunID, err := p.nextID("clean run")
	if err != nil {
		return ProcessingResult{}, err
	}
	cleanRequestID, err := p.nextID("clean request")
	if err != nil {
		return ProcessingResult{}, err
	}
	cleanRequest, err := buildCleanRequest(cleanRequestID, *record, *revision)
	if err != nil {
		return ProcessingResult{}, err
	}
	cleanSnapshot, err := canonicalRequestJSON(cleanRequest)
	if err != nil {
		return ProcessingResult{}, err
	}
	if err := p.lineage.Start(ctx, ailineage.StartRun{
		ID: cleanRunID, ProcessingID: processingID, RequestID: cleanRequestID,
		Action: ai.ActionSourceClean, CanonicalRequest: cleanSnapshot,
		SourceRevisionID: revision.ID, StartedAt: p.now().UTC(),
	}); err != nil {
		return ProcessingResult{}, fmt.Errorf("persist source.clean RUNNING lineage: %w", err)
	}

	var cleanResponse ai.Response
	var executeErr error
	if p.aiRuntime != nil {
		runtimeResult, runtimeErr := p.aiRuntime.Execute(ctx, airuntime.Request{
			RequestID: cleanRequestID, Purpose: ai.ActionSourceClean,
			Instructions: cleanRequest.Messages[0].Content,
			Input:        cleanRequest.Messages[len(cleanRequest.Messages)-1].Content,
			OutputSchema: CleanOutputSchemaV1(), Metadata: cloneMap(cleanRequest.Metadata),
			Limits: airuntime.Limits{MaxRounds: 1, MaxCalls: 0, MaxParallelCalls: 1, MaxContextBytes: 64 * 1024, MaxOutputBytes: 1024 * 1024},
		})
		executeErr = runtimeErr
		if runtimeErr == nil {
			cleanResponse = ai.Response{RequestID: cleanRequestID, Action: ai.ActionSourceClean, Output: runtimeResult.Output, Provenance: runtimeResult.Provenance, Usage: runtimeResult.Usage, Metadata: runtimeResult.Metadata}
		}
	} else {
		cleanResponse, executeErr = p.aiService.Execute(ctx, cleanRequest)
	}
	if executeErr != nil {
		if finalizeErr := p.failRun(ctx, cleanRunID, executionFailureDiagnostic(ai.ActionSourceClean, executeErr), nil); finalizeErr != nil {
			return ProcessingResult{}, errors.Join(fmt.Errorf("execute source.clean: %w", executeErr), finalizeErr)
		}
		return ProcessingResult{}, fmt.Errorf("execute source.clean: %w", executeErr)
	}
	cleanResult, decodeErr := decodeCleanOutput(cleanResponse.Output)
	if decodeErr != nil {
		if finalizeErr := p.failRun(ctx, cleanRunID, errors.New("source.clean typed output validation failed"), &cleanResponse.Provenance); finalizeErr != nil {
			return ProcessingResult{}, errors.Join(decodeErr, finalizeErr)
		}
		return ProcessingResult{}, decodeErr
	}
	if err := p.succeedRun(ctx, cleanRunID, cleanResponse); err != nil {
		return ProcessingResult{}, err
	}

	interpretRunID, err := p.nextID("interpret run")
	if err != nil {
		return ProcessingResult{}, err
	}
	interpretRequestID, err := p.nextID("interpret request")
	if err != nil {
		return ProcessingResult{}, err
	}

	var readEvidences []map[string]any

	interpretRequest, err := buildInterpretRequest(interpretRequestID, *record, *revision, cleanResult, contextSnapshot)
	if err != nil {
		return ProcessingResult{}, err
	}
	interpretSnapshot, err := canonicalRequestJSON(interpretRequest)
	if err != nil {
		return ProcessingResult{}, err
	}
	if err := p.lineage.Start(ctx, ailineage.StartRun{
		ID: interpretRunID, ProcessingID: processingID, RequestID: interpretRequestID,
		Action: ai.ActionSourceInterpret, CanonicalRequest: interpretSnapshot,
		SourceRevisionID: revision.ID, UpstreamRunID: &cleanRunID, StartedAt: p.now().UTC(),
	}); err != nil {
		return ProcessingResult{}, fmt.Errorf("persist source.interpret RUNNING lineage: %w", err)
	}

	var interpretResponse ai.Response
	if p.aiRuntime != nil {
		runtimeResult, runtimeErr := p.aiRuntime.Execute(ctx, airuntime.Request{
			RequestID:    interpretRequestID,
			Purpose:      ai.ActionSourceInterpret,
			Instructions: interpretSystemPrompt,
			Context:      nil,
			Input:        interpretRequest.Messages[len(interpretRequest.Messages)-1].Content,
			OutputSchema: InterpretOutputSchemaV1(),
			Metadata:     cloneMap(interpretRequest.Metadata),
			Limits:       airuntime.Limits{MaxRounds: 4, MaxCalls: 8, MaxParallelCalls: 4, MaxContextBytes: 256 * 1024, MaxOutputBytes: 1024 * 1024},
		})
		executeErr = runtimeErr
		if runtimeErr == nil {
			interpretResponse = ai.Response{
				RequestID: interpretRequestID, Action: ai.ActionSourceInterpret,
				Output: runtimeResult.Output, Provenance: runtimeResult.Provenance,
				Usage: runtimeResult.Usage, Metadata: runtimeResult.Metadata,
			}
			for _, evidence := range runtimeResult.Evidence {
				var output any
				if len(evidence.Output) > 0 {
					_ = json.Unmarshal(evidence.Output, &output)
				}
				readEvidences = append(readEvidences, map[string]any{"capabilityId": evidence.Call.ID, "input": evidence.Call.Arguments, "output": output, "error": evidence.Error})
			}
		}
	} else {
		// Compatibility for unit fixtures that construct Processor directly.
		// Production composition always sets aiRuntime.
		interpretResponse, executeErr = p.aiService.Execute(ctx, interpretRequest)
	}
	if executeErr != nil {
		if finalizeErr := p.failRun(ctx, interpretRunID, executionFailureDiagnostic(ai.ActionSourceInterpret, executeErr), nil); finalizeErr != nil {
			return ProcessingResult{}, errors.Join(fmt.Errorf("execute source.interpret: %w", executeErr), finalizeErr)
		}
		return ProcessingResult{}, fmt.Errorf("execute source.interpret: %w", executeErr)
	}
	interpretResult, decodeErr := decodeInterpretOutput(interpretResponse.Output)
	if decodeErr != nil {
		if finalizeErr := p.failRun(ctx, interpretRunID, errors.New("source.interpret typed output validation failed"), &interpretResponse.Provenance); finalizeErr != nil {
			return ProcessingResult{}, errors.Join(decodeErr, finalizeErr)
		}
		return ProcessingResult{}, decodeErr
	}
	if err := p.succeedRun(ctx, interpretRunID, interpretResponse); err != nil {
		return ProcessingResult{}, err
	}

	return ProcessingResult{
		ProcessingID: processingID, SourceRevisionID: revision.ID,
		CleanRunID: cleanRunID, InterpretRunID: interpretRunID,
		CleanResult: cleanResult, InterpretResult: interpretResult,
		ReadEvidence: readEvidences,
	}, nil
}

func (p *Processor) nextID(kind string) (string, error) {
	id, err := p.newID()
	if err != nil {
		return "", fmt.Errorf("generate %s ID: %w", kind, err)
	}
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("generate %s ID: empty identity", kind)
	}
	return id, nil
}

func (p *Processor) succeedRun(parent context.Context, runID string, response ai.Response) error {
	ctx, cancel := lineageContext(parent)
	defer cancel()
	if err := p.lineage.Succeed(ctx, runID, response.Output, response.Provenance, p.now().UTC()); err != nil {
		return fmt.Errorf("persist AI run %s success: %w", runID, err)
	}
	return nil
}

func (p *Processor) failRun(parent context.Context, runID string, cause error, provenance *ai.ResponseProvenance) error {
	ctx, cancel := lineageContext(parent)
	defer cancel()
	if err := p.lineage.Fail(ctx, runID, cause, provenance, p.now().UTC()); err != nil {
		return fmt.Errorf("persist AI run %s failure: %w", runID, err)
	}
	return nil
}

func lineageContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), lineageFinalizeTimeout)
}

func executionFailureDiagnostic(action ai.Action, cause error) error {
	var rpcErr *plugin.RPCError
	if errors.As(cause, &rpcErr) {
		return fmt.Errorf("%s %s: %s", action, rpcErr.Code, rpcErr.Message)
	}
	category := "execution failed"
	switch {
	case errors.Is(cause, context.Canceled), errors.Is(cause, context.DeadlineExceeded):
		category = "execution canceled or timed out"
	case errors.Is(cause, ai.ErrProvider):
		category = "provider execution failed"
	case errors.Is(cause, ai.ErrMiddleware):
		category = "middleware execution failed"
	case errors.Is(cause, ai.ErrFinalValidation), errors.Is(cause, ai.ErrInvalidProviderResponse):
		category = "runtime response validation failed"
	case errors.Is(cause, ai.ErrInvalidRequest):
		category = "runtime request validation failed"
	}
	return fmt.Errorf("%s %s", action, category)
}
