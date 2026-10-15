package tracking

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"lum-harbor-notifier/internal/history"
)

type monitorCreatedSnapshot struct {
	Version                   int     `json:"version"`
	Kind                      string  `json:"kind"`
	State                     string  `json:"state"`
	DueAt                     string  `json:"due_at"`
	Timezone                  string  `json:"timezone"`
	SubjectKind               string  `json:"subject_kind"`
	SubjectID                 string  `json:"subject_id"`
	ExpectedKind              string  `json:"expected_kind"`
	ExpectedWorkItemLifecycle *string `json:"expected_work_item_lifecycle,omitempty"`
	ExpectedStepID            *string `json:"expected_step_id,omitempty"`
}

type monitorStateSnapshot struct {
	Version     int     `json:"version"`
	State       string  `json:"state"`
	SatisfiedAt *string `json:"satisfied_at,omitempty"`
}

func recordMonitorCreated(ctx context.Context, tx *sql.Tx, point *MonitorPoint) error {
	var expectedLifecycle *string
	if point.Expected.WorkItemState != nil {
		value := string(*point.Expected.WorkItemState)
		expectedLifecycle = &value
	}
	after, err := history.Snapshot(monitorCreatedSnapshot{
		Version: 1, Kind: string(point.Kind), State: string(point.State), DueAt: point.DueAt.Format(time.RFC3339Nano),
		Timezone: point.Timezone, SubjectKind: string(point.Subject.Kind), SubjectID: point.Subject.ID,
		ExpectedKind: string(point.Expected.Kind), ExpectedWorkItemLifecycle: expectedLifecycle, ExpectedStepID: point.Expected.StepID,
	})
	if err != nil {
		return err
	}
	workItemID, revisionID, subjectID := point.WorkItemID, point.LastRevisionID, point.ID
	_, err = history.NewRepository(tx).Append(ctx, history.Draft{
		Type: history.EventMonitorCreated, Scope: history.ScopeTracking, Actor: history.ActorUser,
		Summary: "Monitor created", SubjectEntityID: &subjectID, WorkItemID: &workItemID, RevisionID: &revisionID,
		CorrelationID: history.MonitorCorrelation(point.ID), AfterJSON: after, OccurredAt: point.CreatedAt,
	})
	return err
}

func recordMonitorStateChanged(ctx context.Context, tx *sql.Tx, point *MonitorPoint, oldState MonitorState, newState MonitorState, satisfiedAt *time.Time, revisionID string, occurredAt time.Time) error {
	before, err := history.Snapshot(monitorStateSnapshot{Version: 1, State: string(oldState)})
	if err != nil {
		return err
	}
	var satisfied *string
	if satisfiedAt != nil {
		value := satisfiedAt.Format(time.RFC3339Nano)
		satisfied = &value
	}
	after, err := history.Snapshot(monitorStateSnapshot{Version: 1, State: string(newState), SatisfiedAt: satisfied})
	if err != nil {
		return err
	}
	summary := fmt.Sprintf("Monitor marked %s", newState)
	workItemID, subjectID := point.WorkItemID, point.ID
	_, err = history.NewRepository(tx).Append(ctx, history.Draft{
		Type: history.EventMonitorStateChanged, Scope: history.ScopeTracking, Actor: history.ActorUser,
		Summary: summary, SubjectEntityID: &subjectID, WorkItemID: &workItemID, RevisionID: &revisionID,
		CorrelationID: history.MonitorCorrelation(point.ID), BeforeJSON: before, AfterJSON: after, OccurredAt: occurredAt,
	})
	return err
}
