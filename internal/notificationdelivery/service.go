package notificationdelivery

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"lum-harbor-notifier/internal/database/codec"
	dbgen "lum-harbor-notifier/internal/database/generated"
	"lum-harbor-notifier/internal/history"
)

// CycleResult is the deterministic outcome of one RunDeliveryCycle call,
// mirroring attention.ReconcileResult/DispatchResult's shape.
type CycleResult struct {
	Attempted int
	Delivered int
	Failed    int
}

// Service is the delivery boundary. It reads real SENT Notification truth,
// invokes a Deliverer once per eligible candidate, and durably records only
// confirmed success. It never opens a database transaction spanning more
// than one statement: each candidate's delivery attempt and its success
// marker are independent, isolated units of work, so one candidate's
// failure can never roll back or block another's.
type Service struct {
	db        dbgen.DBTX
	deliverer Deliverer
}

func NewService(db dbgen.DBTX, deliverer Deliverer) *Service {
	return &Service{db: db, deliverer: deliverer}
}

// RunDeliveryCycle attempts external delivery for every SENT Notification
// that has no durable success marker yet. A per-candidate delivery or
// recording failure is isolated: it is logged into the returned error and
// the candidate remains eligible for the next cycle, but every other
// candidate in this run is still attempted and any of their successes are
// still durably recorded.
func (s *Service) RunDeliveryCycle(ctx context.Context, now time.Time) (CycleResult, error) {
	repo := newRepository(s.db)
	candidates, err := repo.listAwaitingExternalDelivery(ctx)
	if err != nil {
		return CycleResult{}, fmt.Errorf("list notifications awaiting external delivery: %w", err)
	}

	var result CycleResult
	var failures []error
	deliveredAt := codec.FormatTime(now.UTC())
	for _, candidate := range candidates {
		result.Attempted++
		request := DeliveryRequest{
			DeliveryID: candidate.NotificationID, Title: candidate.Title,
			Message: candidate.Message, Urgency: mapUrgency(candidate.Severity),
			PlannerItemID: candidate.PlannerItemID, WorkItemID: candidate.WorkItemID,
		}
		if _, err := s.deliverer.Deliver(ctx, request); err != nil {
			result.Failed++
			failures = append(failures, fmt.Errorf("deliver notification %s: %w", candidate.NotificationID, err))
			continue
		}
		var txExecutor dbgen.DBTX = s.db
		var tx *sql.Tx
		if sqlDB, ok := s.db.(*sql.DB); ok {
			var txErr error
			tx, txErr = sqlDB.BeginTx(ctx, nil)
			if txErr != nil {
				result.Failed++
				failures = append(failures, fmt.Errorf("begin delivery tx for %s: %w", candidate.NotificationID, txErr))
				continue
			}
			txExecutor = tx
		}

		txRepo := newRepository(txExecutor)
		if err := txRepo.recordSuccess(ctx, candidate.NotificationID, deliveredAt); err != nil {
			if tx != nil {
				_ = tx.Rollback()
			}
			result.Failed++
			failures = append(failures, fmt.Errorf("record delivery success for notification %s: %w", candidate.NotificationID, err))
			continue
		}

		histRepo := history.NewRepository(txExecutor)
		notifID := candidate.NotificationID
		cause, _ := histRepo.FindLatestBySubjectAndType(ctx, notifID, history.EventNotificationSent)
		var causeID *string
		correlationID := "notification:" + notifID
		if cause != nil {
			causeID = &cause.ID
			correlationID = cause.CorrelationID
		}
		afterMap := map[string]any{
			"version":         1,
			"notification_id": notifID,
			"delivered_at":    deliveredAt,
		}
		afterJSON, _ := history.Snapshot(afterMap)
		if _, err := histRepo.Append(ctx, history.Draft{
			Type:             history.EventNotificationDelivered,
			Scope:            history.ScopeNotification,
			Actor:            history.ActorSystem,
			Summary:          "Notification delivered to external macOS notification center",
			SubjectEntityID:  &notifID,
			CorrelationID:    correlationID,
			CausationEventID: causeID,
			AfterJSON:        afterJSON,
			OccurredAt:       now.UTC(),
		}); err != nil {
			if tx != nil {
				_ = tx.Rollback()
			}
			result.Failed++
			failures = append(failures, fmt.Errorf("record delivery history for notification %s: %w", candidate.NotificationID, err))
			continue
		}

		if tx != nil {
			if err := tx.Commit(); err != nil {
				result.Failed++
				failures = append(failures, fmt.Errorf("commit delivery tx for %s: %w", candidate.NotificationID, err))
				continue
			}
		}
		result.Delivered++
	}
	if len(failures) > 0 {
		return result, fmt.Errorf("notification delivery cycle: %d of %d candidate(s) failed: %w", result.Failed, result.Attempted, failures[0])
	}
	return result, nil
}
