package source

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/identity"
)

var ErrStaleSourceFetch = errors.New("stale source fetch checkpoint")

type Service struct {
	db         *sql.DB
	repository *Repository
	now        func() time.Time
	newID      func() (string, error)
}

func NewService(db *sql.DB) *Service {
	return &Service{
		db: db, repository: NewRepository(db),
		now: func() time.Time { return time.Now().UTC() }, newID: identity.New,
	}
}

type preparedObservation struct {
	observation Observation
	canonical   []byte
	hash        string
}

func (s *Service) IngestFetchResult(ctx context.Context, batch FetchBatch) (IngestionResult, error) {
	prepared, err := prepareBatch(batch)
	if err != nil {
		return IngestionResult{}, err
	}
	if s == nil || s.db == nil || s.repository == nil {
		return IngestionResult{}, fmt.Errorf("source service database must not be nil")
	}
	if ctx == nil {
		return IngestionResult{}, fmt.Errorf("source ingestion context must not be nil")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return IngestionResult{}, fmt.Errorf("begin source ingestion transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	repository := NewRepository(tx)
	historyRepository := history.NewRepository(tx)
	now := s.now().UTC()
	existingState, err := repository.GetSyncState(ctx, batch.ExtensionID, batch.InstanceID, batch.CapabilityID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return IngestionResult{}, err
	}
	if !checkpointMatches(batch.Checkpoint, existingState) {
		return IngestionResult{}, staleSourceFetchError(batch)
	}
	if batch.Checkpoint.Exists && batch.Checkpoint.Version == math.MaxInt64 {
		return IngestionResult{}, fmt.Errorf("source checkpoint version exhausted for extension %q instance %q capability %q", batch.ExtensionID, batch.InstanceID, batch.CapabilityID)
	}
	result := IngestionResult{
		ExtensionID: batch.ExtensionID, InstanceID: batch.InstanceID, CapabilityID: batch.CapabilityID,
		CursorUpdated: batch.NextCursor != nil,
	}
	for _, item := range prepared {
		if err := s.ingestObservation(ctx, repository, historyRepository, batch, item, now, &result); err != nil {
			return IngestionResult{}, err
		}
	}

	var cursor *string
	createdAt := now
	if existingState != nil {
		cursor = cloneString(existingState.Cursor)
		createdAt = existingState.CreatedAt
	}
	if batch.NextCursor != nil {
		cursor = cloneString(batch.NextCursor)
	}
	nextState := SyncState{
		ExtensionID: batch.ExtensionID, InstanceID: batch.InstanceID, CapabilityID: batch.CapabilityID,
		Cursor: cursor, LastSuccessAt: now, CreatedAt: createdAt, UpdatedAt: now,
	}
	var advanced bool
	if batch.Checkpoint.Exists {
		advanced, err = repository.AdvanceSyncStateCAS(ctx, nextState, batch.Checkpoint.Version)
		result.CheckpointVersion = batch.Checkpoint.Version + 1
	} else {
		advanced, err = repository.CreateSyncStateCAS(ctx, nextState)
		result.CheckpointVersion = 1
	}
	if err != nil {
		return IngestionResult{}, err
	}
	if !advanced {
		return IngestionResult{}, staleSourceFetchError(batch)
	}
	result.Cursor = cloneString(cursor)

	if err := tx.Commit(); err != nil {
		return IngestionResult{}, fmt.Errorf("commit source ingestion transaction: %w", err)
	}
	return result, nil
}

// PrepareFetch captures the durable cursor and generation that a remote fetch
// must later present to IngestFetchResult. The generation is checked again
// inside the ingestion transaction; this read intentionally occurs before the
// remote call and does not itself lock the checkpoint.
func (s *Service) PrepareFetch(ctx context.Context, extensionID, instanceID, capabilityID string) (FetchCheckpoint, error) {
	checkpoint := FetchCheckpoint{ExtensionID: extensionID, InstanceID: instanceID, CapabilityID: capabilityID}
	if strings.TrimSpace(extensionID) == "" || strings.TrimSpace(instanceID) == "" || strings.TrimSpace(capabilityID) == "" {
		return FetchCheckpoint{}, fmt.Errorf("source checkpoint identity must not contain empty values")
	}
	state, err := s.repository.GetSyncState(ctx, extensionID, instanceID, capabilityID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return checkpoint, nil
		}
		return FetchCheckpoint{}, err
	}
	checkpoint.Exists = true
	checkpoint.Version = state.CheckpointVersion
	checkpoint.Cursor = cloneString(state.Cursor)
	return checkpoint, nil
}

func checkpointMatches(expected FetchCheckpoint, current *SyncState) bool {
	if !expected.Exists {
		return current == nil && expected.Version == 0
	}
	return current != nil && expected.Version >= 1 && current.CheckpointVersion == expected.Version
}

func staleSourceFetchError(batch FetchBatch) error {
	return fmt.Errorf("%w: extension %q instance %q capability %q", ErrStaleSourceFetch, batch.ExtensionID, batch.InstanceID, batch.CapabilityID)
}

func (s *Service) ingestObservation(ctx context.Context, repository *Repository, historyRepository *history.Repository, batch FetchBatch, item preparedObservation, now time.Time, result *IngestionResult) error {
	observation := item.observation
	record, err := repository.GetRecordByIdentity(ctx, batch.ExtensionID, batch.InstanceID, observation.SourceKind, observation.ExternalID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if record == nil {
		return s.createObservation(ctx, repository, historyRepository, batch, item, now, result)
	}

	lastSeenAt := record.LastSeenAt
	if observation.ObservedAt.After(lastSeenAt) {
		lastSeenAt = observation.ObservedAt
	}
	stale := isStaleSourceTime(record.SourceUpdatedAt, observation.SourceUpdatedAt)
	if record.SourceUpdatedAt == nil || observation.SourceUpdatedAt == nil {
		currentRevision, err := repository.GetCurrentRevision(ctx, record.ID)
		if err != nil {
			return err
		}
		stale = observation.ObservedAt.Before(currentRevision.ObservedAt)
	}
	if stale {
		if err := repository.UpdateObservation(ctx, record.ID, record.SourceUpdatedAt, lastSeenAt, now); err != nil {
			return err
		}
		result.UnchangedCount++
		result.StaleCount++
		return nil
	}
	if record.CurrentContentHash == item.hash {
		sourceUpdatedAt := laterSourceTime(record.SourceUpdatedAt, observation.SourceUpdatedAt)
		if err := repository.UpdateObservation(ctx, record.ID, sourceUpdatedAt, lastSeenAt, now); err != nil {
			return err
		}
		result.UnchangedCount++
		return nil
	}

	revisionID, err := s.newID()
	if err != nil {
		return fmt.Errorf("generate source revision ID: %w", err)
	}
	revision := SourceRevision{
		ID: revisionID, SourceRecordID: record.ID, ContentHash: item.hash,
		Payload: append([]byte(nil), item.canonical...), SourceUpdatedAt: cloneTime(observation.SourceUpdatedAt),
		ObservedAt: observation.ObservedAt, CreatedAt: now,
	}
	if err := repository.CreateRevision(ctx, revision); err != nil {
		return err
	}
	if err := repository.UpdateHead(ctx, record.ID, revisionID, item.hash, observation.SourceUpdatedAt, lastSeenAt, now); err != nil {
		return err
	}
	mode := determineProcessingMode(observation.SourceKind)
	if err := repository.CreateProcessingState(ctx, ProcessingState{
		SourceRevisionID: revisionID,
		State:            ProcessingStatusPending,
		Mode:             mode,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		return fmt.Errorf("create source processing state: %w", err)
	}
	if err := recordRevisionObserved(ctx, historyRepository, *record, revision); err != nil {
		return err
	}
	result.UpdatedCount++
	result.Changes = append(result.Changes, Change{Type: ChangeUpdated, RecordID: record.ID, RevisionID: revisionID})
	result.NewlyChangedRevisionIDs = append(result.NewlyChangedRevisionIDs, revisionID)
	return nil
}

func (s *Service) createObservation(ctx context.Context, repository *Repository, historyRepository *history.Repository, batch FetchBatch, item preparedObservation, now time.Time, result *IngestionResult) error {
	recordID, err := s.newID()
	if err != nil {
		return fmt.Errorf("generate source record ID: %w", err)
	}
	revisionID, err := s.newID()
	if err != nil {
		return fmt.Errorf("generate source revision ID: %w", err)
	}
	observation := item.observation
	record := SourceRecord{
		ID: recordID, ExtensionID: batch.ExtensionID, InstanceID: batch.InstanceID,
		SourceKind: observation.SourceKind, ExternalID: observation.ExternalID,
		FirstSeenAt: observation.ObservedAt, LastSeenAt: observation.ObservedAt,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.CreateRecord(ctx, record); err != nil {
		return err
	}
	revision := SourceRevision{
		ID: revisionID, SourceRecordID: recordID, ContentHash: item.hash,
		Payload: append([]byte(nil), item.canonical...), SourceUpdatedAt: cloneTime(observation.SourceUpdatedAt),
		ObservedAt: observation.ObservedAt, CreatedAt: now,
	}
	if err := repository.CreateRevision(ctx, revision); err != nil {
		return err
	}
	if err := repository.UpdateHead(ctx, recordID, revisionID, item.hash, observation.SourceUpdatedAt, observation.ObservedAt, now); err != nil {
		return err
	}
	mode := determineProcessingMode(observation.SourceKind)
	if err := repository.CreateProcessingState(ctx, ProcessingState{
		SourceRevisionID: revisionID,
		State:            ProcessingStatusPending,
		Mode:             mode,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		return fmt.Errorf("create source processing state: %w", err)
	}
	if err := recordRevisionObserved(ctx, historyRepository, record, revision); err != nil {
		return err
	}
	result.CreatedCount++
	result.Changes = append(result.Changes, Change{Type: ChangeCreated, RecordID: recordID, RevisionID: revisionID})
	result.NewlyChangedRevisionIDs = append(result.NewlyChangedRevisionIDs, revisionID)
	return nil
}

func prepareBatch(batch FetchBatch) ([]preparedObservation, error) {
	if strings.TrimSpace(batch.ExtensionID) == "" {
		return nil, fmt.Errorf("source extension ID must not be empty")
	}
	if strings.TrimSpace(batch.InstanceID) == "" {
		return nil, fmt.Errorf("source instance ID must not be empty")
	}
	if strings.TrimSpace(batch.CapabilityID) == "" {
		return nil, fmt.Errorf("source capability ID must not be empty")
	}
	if batch.Checkpoint.ExtensionID != batch.ExtensionID || batch.Checkpoint.InstanceID != batch.InstanceID || batch.Checkpoint.CapabilityID != batch.CapabilityID {
		return nil, fmt.Errorf("source fetch checkpoint identity does not match batch identity")
	}
	if batch.Checkpoint.Exists {
		if batch.Checkpoint.Version < 1 {
			return nil, fmt.Errorf("existing source fetch checkpoint version must be positive")
		}
	} else if batch.Checkpoint.Version != 0 || batch.Checkpoint.Cursor != nil {
		return nil, fmt.Errorf("absent source fetch checkpoint must have version zero and nil cursor")
	}
	seen := make(map[string]struct{}, len(batch.Records))
	prepared := make([]preparedObservation, 0, len(batch.Records))
	for index, record := range batch.Records {
		if strings.TrimSpace(record.ExternalID) == "" {
			return nil, fmt.Errorf("source record %d external ID must not be empty", index)
		}
		if strings.TrimSpace(record.SourceKind) == "" {
			return nil, fmt.Errorf("source record %d kind must not be empty", index)
		}
		if record.ObservedAt.IsZero() {
			return nil, fmt.Errorf("source record %d observed time must not be zero", index)
		}
		if record.SourceUpdatedAt != nil && record.SourceUpdatedAt.IsZero() {
			return nil, fmt.Errorf("source record %d updated time must not be zero", index)
		}
		key := record.SourceKind + "\x00" + record.ExternalID
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("source fetch batch contains duplicate identity %q/%q", record.SourceKind, record.ExternalID)
		}
		seen[key] = struct{}{}
		canonical, hash, err := CanonicalizePayload(record.Payload)
		if err != nil {
			return nil, fmt.Errorf("source record %d payload: %w", index, err)
		}
		cloned := record
		cloned.Payload = append([]byte(nil), record.Payload...)
		cloned.SourceUpdatedAt = cloneTime(record.SourceUpdatedAt)
		prepared = append(prepared, preparedObservation{observation: cloned, canonical: canonical, hash: hash})
	}
	return prepared, nil
}

func isStaleSourceTime(current, incoming *time.Time) bool {
	return current != nil && incoming != nil && incoming.Before(*current)
}

func laterSourceTime(current, incoming *time.Time) *time.Time {
	if incoming == nil {
		return cloneTime(current)
	}
	if current == nil || !incoming.Before(*current) {
		return cloneTime(incoming)
	}
	return cloneTime(current)
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := value.UTC()
	return &copy
}

func (s *Service) GetSourceRecord(ctx context.Context, id string) (*SourceRecord, error) {
	return s.repository.GetRecord(ctx, id)
}

func (s *Service) GetSourceRecordByIdentity(ctx context.Context, extensionID, instanceID, sourceKind, externalID string) (*SourceRecord, error) {
	return s.repository.GetRecordByIdentity(ctx, extensionID, instanceID, sourceKind, externalID)
}

func (s *Service) GetSourceRevision(ctx context.Context, id string) (*SourceRevision, error) {
	return s.repository.GetRevision(ctx, id)
}

func (s *Service) GetCurrentRevision(ctx context.Context, recordID string) (*SourceRevision, error) {
	return s.repository.GetCurrentRevision(ctx, recordID)
}

func (s *Service) ListRevisionsForRecord(ctx context.Context, recordID string) ([]SourceRevision, error) {
	return s.repository.ListRevisions(ctx, recordID)
}

func (s *Service) GetSyncState(ctx context.Context, extensionID, instanceID, capabilityID string) (*SyncState, error) {
	return s.repository.GetSyncState(ctx, extensionID, instanceID, capabilityID)
}

// ListCurrentHeadsBySourceKind returns the current evidence head of every
// SourceRecord of the requested source_kind, for a downstream deterministic
// caller. It is a process-scoped read (no transaction ownership) like the
// other Get*/List* reads on this Service.
func (s *Service) ListCurrentHeadsBySourceKind(ctx context.Context, sourceKind string) ([]CurrentSourceHead, error) {
	return s.repository.ListCurrentHeadsBySourceKind(ctx, sourceKind)
}

// CaptureLocalSourceInput carries text or note content injected by the core producer.
type CaptureLocalSourceInput struct {
	Title      string
	Body       string
	SourceKind string
	ExternalID string
}

// CaptureLocalSource is the compatibility entry point for core-produced text/notes.
func (s *Service) CaptureLocalSource(ctx context.Context, input CaptureLocalSourceInput) (*SourceRevision, error) {
	if s == nil || s.db == nil || s.repository == nil {
		return nil, fmt.Errorf("source service database must not be nil")
	}
	title := strings.TrimSpace(input.Title)
	body := strings.TrimSpace(input.Body)
	if title == "" && body == "" {
		return nil, fmt.Errorf("source title or body must not be empty")
	}
	sourceKind := strings.TrimSpace(input.SourceKind)
	if sourceKind == "" {
		sourceKind = "NOTE"
	}
	extID := strings.TrimSpace(input.ExternalID)
	if extID == "" {
		generated, err := s.newID()
		if err != nil {
			return nil, fmt.Errorf("generate source external ID: %w", err)
		}
		extID = generated
	}
	payloadMap := map[string]string{
		"title": title,
		"body":  body,
	}
	payloadBytes, err := json.Marshal(payloadMap)
	if err != nil {
		return nil, fmt.Errorf("serialize source payload: %w", err)
	}

	extensionID := "core.local"
	instanceID := "local-inbox"
	capabilityID := "local.capture"

	checkpoint, err := s.PrepareFetch(ctx, extensionID, instanceID, capabilityID)
	if err != nil {
		return nil, fmt.Errorf("prepare source checkpoint: %w", err)
	}

	now := s.now().UTC()
	batch := FetchBatch{
		ExtensionID:  extensionID,
		InstanceID:   instanceID,
		CapabilityID: capabilityID,
		Checkpoint:   checkpoint,
		Records: []Observation{
			{
				ExternalID:      extID,
				SourceKind:      sourceKind,
				ObservedAt:      now,
				SourceUpdatedAt: &now,
				Payload:         payloadBytes,
			},
		},
	}

	result, err := s.IngestFetchResult(ctx, batch)
	if err != nil {
		return nil, fmt.Errorf("ingest source: %w", err)
	}
	if len(result.NewlyChangedRevisionIDs) == 0 {
		rec, err := s.repository.GetRecordByIdentity(ctx, extensionID, instanceID, sourceKind, extID)
		if err != nil {
			return nil, fmt.Errorf("get source record: %w", err)
		}
		return s.GetSourceRevision(ctx, rec.CurrentRevisionID)
	}
	return s.GetSourceRevision(ctx, result.NewlyChangedRevisionIDs[0])
}

// ListLocalSources is the compatibility query for Source heads captured by core.local.
func (s *Service) ListLocalSources(ctx context.Context) ([]CurrentSourceHead, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("source service repository must not be nil")
	}
	heads, err := s.repository.ListCurrentHeadsBySourceKind(ctx, "NOTE")
	if err != nil {
		return nil, err
	}
	var local []CurrentSourceHead
	for _, h := range heads {
		if h.ExtensionID == "core.local" {
			local = append(local, h)
		}
	}
	return local, nil
}

func determineProcessingMode(kind string) ProcessingMode {
	switch strings.ToUpper(strings.TrimSpace(kind)) {
	case "REMINDER":
		return ProcessingModeDeterministicSync
	case "MANUAL_INTENT":
		return ProcessingModeDirectMutation
	case "NOTE", "USER_INTENT":
		fallthrough
	default:
		return ProcessingModeSemanticProposal
	}
}

type CaptureIntentInput struct {
	ExtensionID    string
	InstanceID     string
	CapabilityID   string
	SourceKind     string
	ExternalID     string
	Payload        []byte
	ProcessingMode ProcessingMode
}

func (s *Service) CaptureIntentSource(ctx context.Context, input CaptureIntentInput) (*SourceRevision, error) {
	if s == nil || s.db == nil || s.repository == nil {
		return nil, fmt.Errorf("source service database must not be nil")
	}
	if ctx == nil {
		return nil, fmt.Errorf("source capture context must not be nil")
	}
	extID := strings.TrimSpace(input.ExternalID)
	if extID == "" {
		generated, err := s.newID()
		if err != nil {
			return nil, fmt.Errorf("generate intent external ID: %w", err)
		}
		extID = generated
	}
	extensionID := strings.TrimSpace(input.ExtensionID)
	if extensionID == "" {
		extensionID = "core.manual"
	}
	instanceID := strings.TrimSpace(input.InstanceID)
	if instanceID == "" {
		instanceID = "intent"
	}
	sourceKind := strings.TrimSpace(input.SourceKind)
	if sourceKind == "" {
		sourceKind = "USER_INTENT"
	}
	canonical, hash, err := CanonicalizePayload(input.Payload)
	if err != nil {
		return nil, fmt.Errorf("canonicalize intent payload: %w", err)
	}

	now := s.now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin intent capture tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	repo := NewRepository(tx)
	histRepo := history.NewRepository(tx)

	recordID, err := s.newID()
	if err != nil {
		return nil, fmt.Errorf("generate intent record ID: %w", err)
	}
	revisionID, err := s.newID()
	if err != nil {
		return nil, fmt.Errorf("generate intent revision ID: %w", err)
	}

	record := SourceRecord{
		ID:          recordID,
		ExtensionID: extensionID,
		InstanceID:  instanceID,
		SourceKind:  sourceKind,
		ExternalID:  extID,
		FirstSeenAt: now,
		LastSeenAt:  now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := repo.CreateRecord(ctx, record); err != nil {
		return nil, fmt.Errorf("create intent source record: %w", err)
	}

	revision := SourceRevision{
		ID:              revisionID,
		SourceRecordID:  recordID,
		ContentHash:     hash,
		Payload:         canonical,
		SourceUpdatedAt: &now,
		ObservedAt:      now,
		CreatedAt:       now,
	}
	if err := repo.CreateRevision(ctx, revision); err != nil {
		return nil, fmt.Errorf("create intent source revision: %w", err)
	}
	if err := repo.UpdateHead(ctx, recordID, revisionID, hash, &now, now, now); err != nil {
		return nil, fmt.Errorf("update intent head: %w", err)
	}
	if err := recordRevisionObserved(ctx, histRepo, record, revision); err != nil {
		return nil, fmt.Errorf("record intent revision observed history: %w", err)
	}

	mode := input.ProcessingMode
	if mode == "" {
		mode = determineProcessingMode(sourceKind)
	}
	if err := repo.CreateProcessingState(ctx, ProcessingState{
		SourceRevisionID: revisionID,
		State:            ProcessingStatusPending,
		Mode:             mode,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		return nil, fmt.Errorf("create intent processing state: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit intent source capture: %w", err)
	}

	return &revision, nil
}

func (s *Service) CompleteProcessing(ctx context.Context, sourceRevisionID string) error {
	if s == nil || s.repository == nil {
		return fmt.Errorf("source service repository must not be nil")
	}
	now := s.now().UTC()
	return s.repository.CompleteProcessing(ctx, sourceRevisionID, now)
}

func (s *Service) CompleteProcessingWithProposal(ctx context.Context, sourceRevisionID, proposalID string) error {
	if s == nil || s.repository == nil {
		return fmt.Errorf("source service repository must not be nil")
	}
	now := s.now().UTC()
	return s.repository.CompleteProcessingWithProposal(ctx, sourceRevisionID, proposalID, now)
}

func (s *Service) CompleteProcessingWithRevision(ctx context.Context, sourceRevisionID, revisionID string) error {
	if s == nil || s.repository == nil {
		return fmt.Errorf("source service repository must not be nil")
	}
	now := s.now().UTC()
	return s.repository.CompleteProcessingWithRevision(ctx, sourceRevisionID, revisionID, now)
}

func (s *Service) FailProcessing(ctx context.Context, sourceRevisionID string, lastError string) error {
	if s == nil || s.repository == nil {
		return fmt.Errorf("source service repository must not be nil")
	}
	now := s.now().UTC()
	return s.repository.FailProcessing(ctx, sourceRevisionID, lastError, now)
}

// FailOrphanedProcessing is called once at startup, before any processing
// can begin: a row still PROCESSING was interrupted by a previous shutdown or
// crash and would otherwise never leave that state. It is recorded as FAILED
// (not re-queued), so an abandoned Assistant turn never becomes a surprise
// Proposal; an explicit Source interpretation can be requested again.
func (s *Service) FailOrphanedProcessing(ctx context.Context) (int64, error) {
	if s == nil || s.repository == nil {
		return 0, fmt.Errorf("source service repository must not be nil")
	}
	return s.repository.FailOrphanedProcessing(ctx, "processing was interrupted before it finished (application restarted)", s.now().UTC())
}

func (s *Service) GetProcessingState(ctx context.Context, sourceRevisionID string) (*ProcessingState, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("source service repository must not be nil")
	}
	return s.repository.GetProcessingState(ctx, sourceRevisionID)
}

func (s *Service) ListPendingSemanticSources(ctx context.Context, limit int) ([]PendingSemanticSource, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("source service repository must not be nil")
	}
	return s.repository.ListPendingSemanticSources(ctx, int64(limit))
}

func (s *Service) ClaimPendingProcessing(ctx context.Context, sourceRevisionID string) (bool, error) {
	if s == nil || s.repository == nil {
		return false, fmt.Errorf("source service repository must not be nil")
	}
	now := s.now().UTC()
	return s.repository.ClaimPendingProcessing(ctx, sourceRevisionID, now)
}

func (s *Service) ReleaseClaimToPending(ctx context.Context, sourceRevisionID string) (bool, error) {
	if s == nil || s.repository == nil {
		return false, fmt.Errorf("source service repository must not be nil")
	}
	now := s.now().UTC()
	return s.repository.ReleaseClaimToPending(ctx, sourceRevisionID, now)
}
