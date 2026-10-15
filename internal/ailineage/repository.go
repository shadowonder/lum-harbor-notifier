package ailineage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/database/codec"
	dbgen "lum-harbor-notifier/internal/database/generated"
)

type Repository struct {
	queries *dbgen.Queries
}

func NewRepository(db dbgen.DBTX) *Repository {
	return &Repository{queries: dbgen.New(db)}
}

func (r *Repository) CreateRun(ctx context.Context, run StartRun) error {
	if err := r.queries.CreateAIRun(ctx, dbgen.CreateAIRunParams{
		ID: run.ID, ProcessingID: run.ProcessingID, RequestID: run.RequestID,
		Action: string(run.Action), CanonicalRequestJson: string(run.CanonicalRequest),
		UpstreamRunID: cloneString(run.UpstreamRunID), StartedAt: codec.FormatTime(run.StartedAt),
	}); err != nil {
		return fmt.Errorf("create AI run %s: %w", run.ID, err)
	}
	return nil
}

func (r *Repository) CreateSourceLink(ctx context.Context, runID, sourceRevisionID string) error {
	if err := r.queries.CreateAIRunSource(ctx, dbgen.CreateAIRunSourceParams{
		AiRunID: runID, SourceRevisionID: sourceRevisionID, Role: SourceRolePrimary,
	}); err != nil {
		return fmt.Errorf("link AI run %s to source revision %s: %w", runID, sourceRevisionID, err)
	}
	return nil
}

func (r *Repository) GetPrimarySourceRevisionID(ctx context.Context, runID string) (string, error) {
	revisionID, err := r.queries.GetPrimarySourceRevisionForAIRun(ctx, runID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("get primary source revision for AI run %s: %w", runID, err)
	}
	return revisionID, nil
}

func (r *Repository) FinalizeSucceeded(ctx context.Context, runID string, output json.RawMessage, provenance ai.ResponseProvenance, completedAt string) (bool, error) {
	fields := provenanceFields(&provenance)
	outputJSON := string(output)
	rows, err := r.queries.FinalizeAIRunSucceeded(ctx, dbgen.FinalizeAIRunSucceededParams{
		OutputJson: &outputJSON, ProviderInvoked: fields.providerInvoked,
		ActualProviderID: fields.actualProviderID, ActualProviderName: fields.actualProviderName,
		ActualProviderSource: fields.actualProviderSource, ResponseOriginType: fields.originType,
		ResponseOriginID: fields.originID, ResponseOriginName: fields.originName,
		CompletedAt: &completedAt, ID: runID,
	})
	if err != nil {
		return false, fmt.Errorf("finalize AI run %s succeeded: %w", runID, err)
	}
	return rows == 1, nil
}

func (r *Repository) FinalizeFailed(ctx context.Context, runID, errorText, completedAt string, provenance *ai.ResponseProvenance) (bool, error) {
	fields := provenanceFields(provenance)
	rows, err := r.queries.FinalizeAIRunFailed(ctx, dbgen.FinalizeAIRunFailedParams{
		ErrorText: &errorText, ProviderInvoked: fields.providerInvoked,
		ActualProviderID: fields.actualProviderID, ActualProviderName: fields.actualProviderName,
		ActualProviderSource: fields.actualProviderSource, ResponseOriginType: fields.originType,
		ResponseOriginID: fields.originID, ResponseOriginName: fields.originName,
		CompletedAt: &completedAt, ID: runID,
	})
	if err != nil {
		return false, fmt.Errorf("finalize AI run %s failed: %w", runID, err)
	}
	return rows == 1, nil
}

func (r *Repository) GetRun(ctx context.Context, id string) (Run, error) {
	row, err := r.queries.GetAIRun(ctx, id)
	if err != nil {
		return Run{}, fmt.Errorf("get AI run %s: %w", id, err)
	}
	return runFromDB(row)
}

func (r *Repository) ListRunsForProcessing(ctx context.Context, processingID string) ([]Run, error) {
	rows, err := r.queries.ListAIRunsForProcessing(ctx, processingID)
	if err != nil {
		return nil, fmt.Errorf("list AI runs for processing %s: %w", processingID, err)
	}
	return runsFromDB(rows)
}

func (r *Repository) ListRunsForSourceRevision(ctx context.Context, revisionID string) ([]Run, error) {
	rows, err := r.queries.ListAIRunsForSourceRevision(ctx, revisionID)
	if err != nil {
		return nil, fmt.Errorf("list AI runs for source revision %s: %w", revisionID, err)
	}
	return runsFromDB(rows)
}

type storedProvenance struct {
	providerInvoked      *int64
	actualProviderID     *string
	actualProviderName   *string
	actualProviderSource *string
	originType           *string
	originID             *string
	originName           *string
}

func provenanceFields(provenance *ai.ResponseProvenance) storedProvenance {
	if provenance == nil {
		return storedProvenance{}
	}
	invoked := int64(0)
	if provenance.ProviderInvoked {
		invoked = 1
	}
	originType := string(provenance.Origin.Kind)
	fields := storedProvenance{
		providerInvoked: &invoked, originType: &originType,
		originID: stringPointer(provenance.Origin.ID), originName: stringPointer(provenance.Origin.Name),
	}
	if provenance.ActualProvider != nil {
		fields.actualProviderID = stringPointer(provenance.ActualProvider.ID)
		fields.actualProviderName = stringPointer(provenance.ActualProvider.Name)
		source := string(provenance.ActualProvider.Source)
		fields.actualProviderSource = &source
	}
	return fields
}

func runFromDB(row dbgen.AiRun) (Run, error) {
	startedAt, err := codec.ParseTime(row.StartedAt)
	if err != nil {
		return Run{}, fmt.Errorf("parse AI run %s started_at: %w", row.ID, err)
	}
	completedAt, err := codec.ParseOptionalTime(row.CompletedAt)
	if err != nil {
		return Run{}, fmt.Errorf("parse AI run %s completed_at: %w", row.ID, err)
	}
	run := Run{
		ID: row.ID, ProcessingID: row.ProcessingID, RequestID: row.RequestID,
		Action: ai.Action(row.Action), Status: Status(row.Status),
		CanonicalRequest: json.RawMessage(row.CanonicalRequestJson), ErrorText: cloneString(row.ErrorText),
		UpstreamRunID: cloneString(row.UpstreamRunID), StartedAt: startedAt, CompletedAt: completedAt,
	}
	if row.OutputJson != nil {
		run.Output = json.RawMessage(*row.OutputJson)
	}
	if row.ProviderInvoked != nil {
		provenance := ai.ResponseProvenance{
			ProviderInvoked: *row.ProviderInvoked == 1,
			Origin:          ai.ResponseOrigin{Kind: ai.ResponseOriginKind(value(row.ResponseOriginType)), ID: value(row.ResponseOriginID), Name: value(row.ResponseOriginName)},
		}
		if row.ActualProviderID != nil {
			provenance.ActualProvider = &ai.ProviderInfo{ID: *row.ActualProviderID, Name: value(row.ActualProviderName), Source: ai.ProviderSource(value(row.ActualProviderSource))}
		}
		run.Provenance = &provenance
	}
	return run, nil
}

func runsFromDB(rows []dbgen.AiRun) ([]Run, error) {
	runs := make([]Run, 0, len(rows))
	for _, row := range rows {
		run, err := runFromDB(row)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, nil
}

func value(input *string) string {
	if input == nil {
		return ""
	}
	return *input
}

func cloneString(input *string) *string {
	if input == nil {
		return nil
	}
	copy := *input
	return &copy
}

func stringPointer(value string) *string { return &value }
