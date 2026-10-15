package notificationdelivery

import (
	"context"
	"database/sql"
	"errors"

	dbgen "lum-harbor-notifier/internal/database/generated"
)

// eligibleNotification is one SENT Notification with no recorded external
// delivery success yet, joined with just enough linked Attention truth
// (severity) to build a DeliveryRequest. It carries no other Notification
// or AttentionIssue field.
type eligibleNotification struct {
	NotificationID string
	Title          string
	Message        string
	Severity       string
	PlannerItemID  *string
	WorkItemID     *string
}

// repository persists/reads delivery-boundary state through its executor.
// It owns no transaction and no business rule; it never touches
// notifications or attention_issues rows.
type repository struct {
	queries *dbgen.Queries
}

func newRepository(db dbgen.DBTX) *repository { return &repository{queries: dbgen.New(db)} }

func (r *repository) listAwaitingExternalDelivery(ctx context.Context) ([]eligibleNotification, error) {
	rows, err := r.queries.ListSentNotificationsAwaitingExternalDelivery(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]eligibleNotification, len(rows))
	for i, row := range rows {
		out[i] = eligibleNotification{
			NotificationID: row.NotificationID, Title: row.Title, Message: row.Message, Severity: row.Severity,
			PlannerItemID: row.PlannerItemID, WorkItemID: row.WorkItemID,
		}
	}
	return out, nil
}

func (r *repository) recordSuccess(ctx context.Context, notificationID string, deliveredAt string) error {
	return r.queries.RecordNotificationExternalDelivery(ctx, dbgen.RecordNotificationExternalDeliveryParams{
		NotificationID: notificationID, DeliveredAt: deliveredAt,
	})
}

// alreadyDelivered reports whether a durable success marker already exists
// for notificationID. It exists for tests; production code never needs it
// because listAwaitingExternalDelivery already excludes delivered rows.
func (r *repository) alreadyDelivered(ctx context.Context, notificationID string) (bool, error) {
	if _, err := r.queries.GetNotificationExternalDelivery(ctx, notificationID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
