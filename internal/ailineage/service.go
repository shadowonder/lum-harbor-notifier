package ailineage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/database/codec"
)

const maxErrorBytes = 4096

// Service owns short transactions for durable AI run lifecycle transitions.
// No transaction opened here spans an AI execution.
type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service { return &Service{db: db} }

func (s *Service) Start(ctx context.Context, run StartRun) error {
	if ctx == nil {
		return fmt.Errorf("AI lineage context must not be nil")
	}
	if err := validateStart(run); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return fmt.Errorf("AI lineage database must not be nil")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin AI run start transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	repository := NewRepository(tx)
	if err := repository.CreateRun(ctx, run); err != nil {
		return err
	}
	if strings.TrimSpace(run.SourceRevisionID) != "" {
		if err := repository.CreateSourceLink(ctx, run.ID, run.SourceRevisionID); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit AI run start transaction: %w", err)
	}
	return nil
}

func (s *Service) Succeed(ctx context.Context, runID string, output json.RawMessage, provenance ai.ResponseProvenance, completedAt time.Time) error {
	if ctx == nil {
		return fmt.Errorf("AI lineage context must not be nil")
	}
	if strings.TrimSpace(runID) == "" || len(output) == 0 || !json.Valid(output) || completedAt.IsZero() {
		return fmt.Errorf("successful AI run finalization is invalid")
	}
	return s.finalize(ctx, runID, func(repository *Repository) (bool, error) {
		return repository.FinalizeSucceeded(ctx, runID, output, provenance, codec.FormatTime(completedAt))
	})
}

func (s *Service) Fail(ctx context.Context, runID string, cause error, provenance *ai.ResponseProvenance, completedAt time.Time) error {
	if ctx == nil {
		return fmt.Errorf("AI lineage context must not be nil")
	}
	if strings.TrimSpace(runID) == "" || cause == nil || completedAt.IsZero() {
		return fmt.Errorf("failed AI run finalization is invalid")
	}
	errorText := truncateUTF8(cause.Error(), maxErrorBytes)
	if strings.TrimSpace(errorText) == "" {
		errorText = "AI execution failed"
	}
	return s.finalize(ctx, runID, func(repository *Repository) (bool, error) {
		return repository.FinalizeFailed(ctx, runID, errorText, codec.FormatTime(completedAt), provenance)
	})
}

func (s *Service) finalize(ctx context.Context, runID string, transition func(*Repository) (bool, error)) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("AI lineage database must not be nil")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin AI run finalization transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	repository := NewRepository(tx)
	updated, err := transition(repository)
	if err != nil {
		return err
	}
	if !updated {
		return fmt.Errorf("AI run is missing or no longer RUNNING")
	}
	run, err := repository.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	sourceRevisionID, err := repository.GetPrimarySourceRevisionID(ctx, runID)
	if err != nil {
		return err
	}
	if err := recordTerminalHistory(ctx, tx, run, sourceRevisionID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit AI run finalization transaction: %w", err)
	}
	return nil
}

func validateStart(run StartRun) error {
	if strings.TrimSpace(run.ID) == "" || strings.TrimSpace(run.ProcessingID) == "" || strings.TrimSpace(run.RequestID) == "" || strings.TrimSpace(string(run.Action)) == "" {
		return fmt.Errorf("AI run identity fields must not be empty")
	}
	if len(run.CanonicalRequest) == 0 || !json.Valid(run.CanonicalRequest) {
		return fmt.Errorf("AI run canonical request must be valid JSON")
	}
	if run.StartedAt.IsZero() {
		return fmt.Errorf("AI run started time must not be zero")
	}
	return nil
}

func truncateUTF8(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	value = value[:maximum]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
