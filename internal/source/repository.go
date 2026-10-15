package source

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"lum-harbor-notifier/internal/database/codec"
	dbgen "lum-harbor-notifier/internal/database/generated"
)

var ErrNotFound = errors.New("source value not found")

type Repository struct {
	queries *dbgen.Queries
}

func NewRepository(db dbgen.DBTX) *Repository {
	return &Repository{queries: dbgen.New(db)}
}

func (r *Repository) GetRecordByIdentity(ctx context.Context, extensionID, instanceID, sourceKind, externalID string) (*SourceRecord, error) {
	row, err := r.queries.GetSourceRecordByIdentity(ctx, dbgen.GetSourceRecordByIdentityParams{
		ExtensionID: extensionID,
		InstanceID:  instanceID,
		SourceKind:  sourceKind,
		ExternalID:  externalID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get source record by identity: %w", err)
	}
	return sourceRecordFromDB(row)
}

func (r *Repository) GetRecord(ctx context.Context, id string) (*SourceRecord, error) {
	row, err := r.queries.GetSourceRecordByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: source record %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("get source record %s: %w", id, err)
	}
	return sourceRecordFromDB(row)
}

func (r *Repository) CreateRecord(ctx context.Context, record SourceRecord) error {
	err := r.queries.CreateSourceRecord(ctx, dbgen.CreateSourceRecordParams{
		ID:          record.ID,
		ExtensionID: record.ExtensionID,
		InstanceID:  record.InstanceID,
		SourceKind:  record.SourceKind,
		ExternalID:  record.ExternalID,
		FirstSeenAt: codec.FormatTime(record.FirstSeenAt),
		LastSeenAt:  codec.FormatTime(record.LastSeenAt),
		CreatedAt:   codec.FormatTime(record.CreatedAt),
		UpdatedAt:   codec.FormatTime(record.UpdatedAt),
	})
	if err != nil {
		return fmt.Errorf("create source record %s: %w", record.ID, err)
	}
	return nil
}

func (r *Repository) UpdateObservation(ctx context.Context, recordID string, sourceUpdatedAt *time.Time, lastSeenAt, updatedAt time.Time) error {
	err := r.queries.UpdateSourceRecordObservation(ctx, dbgen.UpdateSourceRecordObservationParams{
		SourceUpdatedAt: codec.FormatOptionalTime(sourceUpdatedAt),
		LastSeenAt:      codec.FormatTime(lastSeenAt),
		UpdatedAt:       codec.FormatTime(updatedAt),
		ID:              recordID,
	})
	if err != nil {
		return fmt.Errorf("update source record observation %s: %w", recordID, err)
	}
	return nil
}

func (r *Repository) UpdateHead(ctx context.Context, recordID, revisionID, contentHash string, sourceUpdatedAt *time.Time, lastSeenAt, updatedAt time.Time) error {
	err := r.queries.UpdateSourceRecordHead(ctx, dbgen.UpdateSourceRecordHeadParams{
		CurrentRevisionID:  &revisionID,
		CurrentContentHash: &contentHash,
		SourceUpdatedAt:    codec.FormatOptionalTime(sourceUpdatedAt),
		LastSeenAt:         codec.FormatTime(lastSeenAt),
		UpdatedAt:          codec.FormatTime(updatedAt),
		ID:                 recordID,
	})
	if err != nil {
		return fmt.Errorf("update source record head %s: %w", recordID, err)
	}
	return nil
}

func (r *Repository) CreateRevision(ctx context.Context, revision SourceRevision) error {
	_, err := r.queries.CreateSourceRevision(ctx, dbgen.CreateSourceRevisionParams{
		ID:              revision.ID,
		SourceRecordID:  revision.SourceRecordID,
		ContentHash:     revision.ContentHash,
		PayloadJson:     string(revision.Payload),
		SourceUpdatedAt: codec.FormatOptionalTime(revision.SourceUpdatedAt),
		ObservedAt:      codec.FormatTime(revision.ObservedAt),
		CreatedAt:       codec.FormatTime(revision.CreatedAt),
	})
	if err != nil {
		return fmt.Errorf("create source revision %s: %w", revision.ID, err)
	}
	return nil
}

func (r *Repository) GetRevision(ctx context.Context, id string) (*SourceRevision, error) {
	row, err := r.queries.GetSourceRevisionByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: source revision %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("get source revision %s: %w", id, err)
	}
	return sourceRevisionFromDB(row)
}

func (r *Repository) GetCurrentRevision(ctx context.Context, recordID string) (*SourceRevision, error) {
	row, err := r.queries.GetCurrentSourceRevision(ctx, recordID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: current revision for source record %s", ErrNotFound, recordID)
		}
		return nil, fmt.Errorf("get current revision for source record %s: %w", recordID, err)
	}
	return sourceRevisionFromDB(row)
}

func (r *Repository) ListRevisions(ctx context.Context, recordID string) ([]SourceRevision, error) {
	rows, err := r.queries.ListSourceRevisionsForRecord(ctx, recordID)
	if err != nil {
		return nil, fmt.Errorf("list source revisions for record %s: %w", recordID, err)
	}
	revisions := make([]SourceRevision, 0, len(rows))
	for _, row := range rows {
		revision, err := sourceRevisionFromDB(row)
		if err != nil {
			return nil, err
		}
		revisions = append(revisions, *revision)
	}
	return revisions, nil
}

func (r *Repository) GetSyncState(ctx context.Context, extensionID, instanceID, capabilityID string) (*SyncState, error) {
	row, err := r.queries.GetSourceSyncState(ctx, dbgen.GetSourceSyncStateParams{
		ExtensionID: extensionID, InstanceID: instanceID, CapabilityID: capabilityID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get source sync state: %w", err)
	}
	return syncStateFromDB(row)
}

func (r *Repository) CreateSyncStateCAS(ctx context.Context, state SyncState) (bool, error) {
	rows, err := r.queries.CreateSourceSyncStateCAS(ctx, dbgen.CreateSourceSyncStateCASParams{
		ExtensionID: state.ExtensionID, InstanceID: state.InstanceID, CapabilityID: state.CapabilityID,
		Cursor: cloneString(state.Cursor), LastSuccessAt: codec.FormatTime(state.LastSuccessAt),
		CreatedAt: codec.FormatTime(state.CreatedAt), UpdatedAt: codec.FormatTime(state.UpdatedAt),
	})
	if err != nil {
		return false, fmt.Errorf("create source sync state: %w", err)
	}
	return rows == 1, nil
}

func (r *Repository) AdvanceSyncStateCAS(ctx context.Context, state SyncState, expectedVersion int64) (bool, error) {
	rows, err := r.queries.AdvanceSourceSyncStateCAS(ctx, dbgen.AdvanceSourceSyncStateCASParams{
		ExtensionID: state.ExtensionID, InstanceID: state.InstanceID, CapabilityID: state.CapabilityID,
		Cursor: cloneString(state.Cursor), LastSuccessAt: codec.FormatTime(state.LastSuccessAt),
		UpdatedAt: codec.FormatTime(state.UpdatedAt), ExpectedCheckpointVersion: expectedVersion,
	})
	if err != nil {
		return false, fmt.Errorf("advance source sync state: %w", err)
	}
	return rows == 1, nil
}

// ListCurrentHeadsBySourceKind returns one current-head fact per SourceRecord
// of the requested source_kind in one bounded, set-based read (record and its
// current revision joined server-side; no per-record follow-up query).
func (r *Repository) ListCurrentHeadsBySourceKind(ctx context.Context, sourceKind string) ([]CurrentSourceHead, error) {
	rows, err := r.queries.ListCurrentSourceHeadsBySourceKind(ctx, sourceKind)
	if err != nil {
		return nil, fmt.Errorf("list current source heads for kind %q: %w", sourceKind, err)
	}
	heads := make([]CurrentSourceHead, 0, len(rows))
	for _, row := range rows {
		head, err := currentSourceHeadFromDB(row)
		if err != nil {
			return nil, err
		}
		heads = append(heads, *head)
	}
	return heads, nil
}

func currentSourceHeadFromDB(row dbgen.ListCurrentSourceHeadsBySourceKindRow) (*CurrentSourceHead, error) {
	lastSeenAt, err := codec.ParseTime(row.LastSeenAt)
	if err != nil {
		return nil, fmt.Errorf("parse source record %s last_seen_at: %w", row.RecordID, err)
	}
	observedAt, err := codec.ParseTime(row.ObservedAt)
	if err != nil {
		return nil, fmt.Errorf("parse source revision %s observed_at: %w", row.RevisionID, err)
	}
	sourceUpdatedAt, err := codec.ParseOptionalTime(row.SourceUpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse source revision %s source_updated_at: %w", row.RevisionID, err)
	}
	if !json.Valid([]byte(row.PayloadJson)) {
		return nil, fmt.Errorf("source revision %s has invalid payload JSON", row.RevisionID)
	}
	return &CurrentSourceHead{
		RecordID: row.RecordID, ExtensionID: row.ExtensionID, InstanceID: row.InstanceID,
		SourceKind: row.SourceKind, ExternalID: row.ExternalID,
		CurrentRevisionID: row.RevisionID, CurrentContentHash: row.ContentHash,
		Payload:         json.RawMessage(append([]byte(nil), row.PayloadJson...)),
		SourceUpdatedAt: sourceUpdatedAt, ObservedAt: observedAt, LastSeenAt: lastSeenAt,
	}, nil
}

func sourceRecordFromDB(row dbgen.SourceRecord) (*SourceRecord, error) {
	if row.CurrentRevisionID == nil || row.CurrentContentHash == nil {
		return nil, fmt.Errorf("source record %s has no current revision head", row.ID)
	}
	firstSeenAt, err := codec.ParseTime(row.FirstSeenAt)
	if err != nil {
		return nil, fmt.Errorf("parse source record %s first_seen_at: %w", row.ID, err)
	}
	lastSeenAt, err := codec.ParseTime(row.LastSeenAt)
	if err != nil {
		return nil, fmt.Errorf("parse source record %s last_seen_at: %w", row.ID, err)
	}
	createdAt, err := codec.ParseTime(row.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse source record %s created_at: %w", row.ID, err)
	}
	updatedAt, err := codec.ParseTime(row.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse source record %s updated_at: %w", row.ID, err)
	}
	sourceUpdatedAt, err := codec.ParseOptionalTime(row.SourceUpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse source record %s source_updated_at: %w", row.ID, err)
	}
	return &SourceRecord{
		ID: row.ID, ExtensionID: row.ExtensionID, InstanceID: row.InstanceID,
		SourceKind: row.SourceKind, ExternalID: row.ExternalID,
		CurrentRevisionID: *row.CurrentRevisionID, CurrentContentHash: *row.CurrentContentHash,
		SourceUpdatedAt: sourceUpdatedAt, FirstSeenAt: firstSeenAt, LastSeenAt: lastSeenAt,
		CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}

func sourceRevisionFromDB(row dbgen.SourceRevision) (*SourceRevision, error) {
	observedAt, err := codec.ParseTime(row.ObservedAt)
	if err != nil {
		return nil, fmt.Errorf("parse source revision %s observed_at: %w", row.ID, err)
	}
	createdAt, err := codec.ParseTime(row.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse source revision %s created_at: %w", row.ID, err)
	}
	sourceUpdatedAt, err := codec.ParseOptionalTime(row.SourceUpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse source revision %s source_updated_at: %w", row.ID, err)
	}
	if !json.Valid([]byte(row.PayloadJson)) {
		return nil, fmt.Errorf("source revision %s has invalid payload JSON", row.ID)
	}
	return &SourceRevision{
		RevisionNo: row.RevisionNo, ID: row.ID, SourceRecordID: row.SourceRecordID,
		ContentHash: row.ContentHash, Payload: json.RawMessage(append([]byte(nil), row.PayloadJson...)),
		SourceUpdatedAt: sourceUpdatedAt, ObservedAt: observedAt, CreatedAt: createdAt,
	}, nil
}

func syncStateFromDB(row dbgen.SourceSyncState) (*SyncState, error) {
	lastSuccessAt, err := codec.ParseTime(row.LastSuccessAt)
	if err != nil {
		return nil, fmt.Errorf("parse source sync state last_success_at: %w", err)
	}
	createdAt, err := codec.ParseTime(row.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse source sync state created_at: %w", err)
	}
	updatedAt, err := codec.ParseTime(row.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse source sync state updated_at: %w", err)
	}
	return &SyncState{
		ExtensionID: row.ExtensionID, InstanceID: row.InstanceID, CapabilityID: row.CapabilityID,
		Cursor: cloneString(row.Cursor), CheckpointVersion: row.CheckpointVersion,
		LastSuccessAt: lastSuccessAt, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func (r *Repository) CreateProcessingState(ctx context.Context, state ProcessingState) error {
	var startedAt, completedAt *string
	if state.StartedAt != nil {
		s := codec.FormatTime(*state.StartedAt)
		startedAt = &s
	}
	if state.CompletedAt != nil {
		c := codec.FormatTime(*state.CompletedAt)
		completedAt = &c
	}
	return r.queries.CreateSourceProcessingState(ctx, dbgen.CreateSourceProcessingStateParams{
		SourceRevisionID: state.SourceRevisionID,
		State:            string(state.State),
		Mode:             string(state.Mode),
		StartedAt:        startedAt,
		CompletedAt:      completedAt,
		LastError:        state.LastError,
		ProposalID:       state.ProposalID,
		RevisionID:       state.RevisionID,
		CreatedAt:        codec.FormatTime(state.CreatedAt),
		UpdatedAt:        codec.FormatTime(state.UpdatedAt),
	})
}

func (r *Repository) GetProcessingState(ctx context.Context, sourceRevisionID string) (*ProcessingState, error) {
	row, err := r.queries.GetSourceProcessingState(ctx, sourceRevisionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get source processing state %s: %w", sourceRevisionID, err)
	}
	startedAt, err := codec.ParseOptionalTime(row.StartedAt)
	if err != nil {
		return nil, err
	}
	completedAt, err := codec.ParseOptionalTime(row.CompletedAt)
	if err != nil {
		return nil, err
	}
	createdAt, err := codec.ParseTime(row.CreatedAt)
	if err != nil {
		return nil, err
	}
	updatedAt, err := codec.ParseTime(row.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &ProcessingState{
		SourceRevisionID: row.SourceRevisionID,
		State:            ProcessingStatus(row.State),
		Mode:             ProcessingMode(row.Mode),
		StartedAt:        startedAt,
		CompletedAt:      completedAt,
		LastError:        row.LastError,
		ProposalID:       row.ProposalID,
		RevisionID:       row.RevisionID,
		CreatedAt:        createdAt,
		UpdatedAt:        updatedAt,
	}, nil
}

func (r *Repository) ListPendingSemanticSources(ctx context.Context, limit int64) ([]PendingSemanticSource, error) {
	rows, err := r.queries.ListPendingSemanticSources(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list pending semantic sources: %w", err)
	}
	result := make([]PendingSemanticSource, 0, len(rows))
	for _, row := range rows {
		startedAt, _ := codec.ParseOptionalTime(row.StartedAt)
		completedAt, _ := codec.ParseOptionalTime(row.CompletedAt)
		createdAt, _ := codec.ParseTime(row.CreatedAt)
		updatedAt, _ := codec.ParseTime(row.UpdatedAt)
		result = append(result, PendingSemanticSource{
			SourceRevisionID: row.SourceRevisionID,
			State:            ProcessingStatus(row.State),
			Mode:             ProcessingMode(row.Mode),
			StartedAt:        startedAt,
			CompletedAt:      completedAt,
			LastError:        row.LastError,
			ProposalID:       row.ProposalID,
			RevisionID:       row.RevisionID,
			CreatedAt:        createdAt,
			UpdatedAt:        updatedAt,
			Payload:          json.RawMessage(row.PayloadJson),
			ContentHash:      row.ContentHash,
			ExtensionID:      row.ExtensionID,
			InstanceID:       row.InstanceID,
			SourceKind:       row.SourceKind,
			ExternalID:       row.ExternalID,
		})
	}
	return result, nil
}

func (r *Repository) ClaimPendingProcessing(ctx context.Context, sourceRevisionID string, startedAt time.Time) (bool, error) {
	atStr := codec.FormatTime(startedAt)
	rows, err := r.queries.ClaimPendingSourceProcessing(ctx, dbgen.ClaimPendingSourceProcessingParams{
		SourceRevisionID: sourceRevisionID,
		StartedAt:        &atStr,
		UpdatedAt:        atStr,
	})
	if err != nil {
		return false, fmt.Errorf("claim pending source processing %s: %w", sourceRevisionID, err)
	}
	return rows > 0, nil
}

func (r *Repository) ReleaseClaimToPending(ctx context.Context, sourceRevisionID string, releasedAt time.Time) (bool, error) {
	atStr := codec.FormatTime(releasedAt)
	rows, err := r.queries.ReleaseClaimToPendingSourceProcessing(ctx, dbgen.ReleaseClaimToPendingSourceProcessingParams{
		SourceRevisionID: sourceRevisionID,
		UpdatedAt:        atStr,
	})
	if err != nil {
		return false, fmt.Errorf("release claim to pending %s: %w", sourceRevisionID, err)
	}
	return rows > 0, nil
}

func (r *Repository) CompleteProcessing(ctx context.Context, sourceRevisionID string, completedAt time.Time) error {
	atStr := codec.FormatTime(completedAt)
	return r.queries.CompleteSourceProcessingWithProposal(ctx, dbgen.CompleteSourceProcessingWithProposalParams{
		SourceRevisionID: sourceRevisionID,
		ProposalID:       nil,
		CompletedAt:      &atStr,
		UpdatedAt:        atStr,
	})
}

func (r *Repository) CompleteProcessingWithProposal(ctx context.Context, sourceRevisionID, proposalID string, completedAt time.Time) error {
	atStr := codec.FormatTime(completedAt)
	var propIDPtr *string
	if proposalID != "" {
		propIDPtr = &proposalID
	}
	return r.queries.CompleteSourceProcessingWithProposal(ctx, dbgen.CompleteSourceProcessingWithProposalParams{
		SourceRevisionID: sourceRevisionID,
		ProposalID:       propIDPtr,
		CompletedAt:      &atStr,
		UpdatedAt:        atStr,
	})
}

func (r *Repository) CompleteProcessingWithRevision(ctx context.Context, sourceRevisionID, revisionID string, completedAt time.Time) error {
	atStr := codec.FormatTime(completedAt)
	return r.queries.CompleteSourceProcessingWithRevision(ctx, dbgen.CompleteSourceProcessingWithRevisionParams{
		SourceRevisionID: sourceRevisionID,
		RevisionID:       &revisionID,
		CompletedAt:      &atStr,
		UpdatedAt:        atStr,
	})
}

// FailOrphanedProcessing marks every PROCESSING row FAILED and returns how
// many it changed.
func (r *Repository) FailOrphanedProcessing(ctx context.Context, lastError string, completedAt time.Time) (int64, error) {
	atStr := codec.FormatTime(completedAt)
	return r.queries.FailOrphanedSourceProcessing(ctx, dbgen.FailOrphanedSourceProcessingParams{
		LastError:   &lastError,
		CompletedAt: &atStr,
		UpdatedAt:   atStr,
	})
}

func (r *Repository) FailProcessing(ctx context.Context, sourceRevisionID string, lastError string, completedAt time.Time) error {
	atStr := codec.FormatTime(completedAt)
	return r.queries.FailSourceProcessing(ctx, dbgen.FailSourceProcessingParams{
		SourceRevisionID: sourceRevisionID,
		LastError:        &lastError,
		CompletedAt:      &atStr,
		UpdatedAt:        atStr,
	})
}
