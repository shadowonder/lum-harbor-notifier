package app

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"lum-harbor-notifier/internal/attention"
)

// GetNotificationCenter returns every delivered (SENT) Notification with its
// linked Attention truth in one bounded collection. PENDING and CANCELLED
// Notifications are durable backend truth but are never returned. Dispositions
// are derived with the backend clock. The Work Item ID on every item is the V1
// "Open Item" target; the subject TrackedItem ID is retained for future focus.
func (app *App) GetNotificationCenter() (NotificationCenterDTO, error) {
	if app.attentionService == nil {
		return NotificationCenterDTO{}, fmt.Errorf("notification service is not initialized")
	}
	ctx := appContext(app.ctx)
	center, err := app.attentionService.NotificationCenter(ctx)
	if err != nil {
		slog.Error("notification center failed", "error", err)
		return NotificationCenterDTO{}, fmt.Errorf("load notification center failed")
	}
	titles := map[string]string{}
	if app.workitemService != nil {
		items, err := app.workitemService.List(ctx)
		if err != nil {
			slog.Error("notification center work item titles failed", "error", err)
			return NotificationCenterDTO{}, fmt.Errorf("load notification center failed")
		}
		for _, item := range items {
			titles[item.ID] = item.Title
		}
	}
	dto, err := notificationCenterToDTO(center, titles)
	if err != nil {
		slog.Error("notification center mapping failed", "error", err)
		return NotificationCenterDTO{}, fmt.Errorf("load notification center failed")
	}
	return dto, nil
}

// MarkNotificationRead marks a delivered Notification read (idempotent). It
// never resolves or snoozes the Attention issue and never touches domain truth.
func (app *App) MarkNotificationRead(request MarkNotificationReadRequestDTO) (NotificationReadResultDTO, error) {
	if app.attentionService == nil {
		return NotificationReadResultDTO{}, fmt.Errorf("notification service is not initialized")
	}
	notification, err := app.attentionService.MarkNotificationRead(appContext(app.ctx), request.NotificationID)
	if err != nil {
		return NotificationReadResultDTO{}, notificationTransportError("mark notification read", err)
	}
	result := NotificationReadResultDTO{NotificationID: notification.ID}
	if notification.ReadAt != nil {
		result.ReadAt = notification.ReadAt.UTC().Format(time.RFC3339Nano)
	}
	return result, nil
}

// AcknowledgeNotificationPresented records that an in-app alert or notification
// item was presented to the user on the screen. It sets presented_at and emits
// NOTIFICATION_PRESENTED History event idempotently.
func (app *App) AcknowledgeNotificationPresented(id string) error {
	if app.attentionService == nil {
		return fmt.Errorf("notification service is not initialized")
	}
	_, err := app.attentionService.MarkNotificationPresented(appContext(app.ctx), id)
	if err != nil {
		return notificationTransportError("acknowledge notification presented", err)
	}
	return nil
}

// SnoozeAttentionIssue snoozes an OPEN issue until a future instant. Only the
// issue and instant are accepted; the backend clock validates it.
func (app *App) SnoozeAttentionIssue(request SnoozeAttentionIssueRequestDTO) (SnoozeResultDTO, error) {
	if app.attentionService == nil {
		return SnoozeResultDTO{}, fmt.Errorf("notification service is not initialized")
	}
	until, err := time.Parse(time.RFC3339, strings.TrimSpace(request.SnoozedUntil))
	if err != nil {
		return SnoozeResultDTO{}, fmt.Errorf("snooze time must be an RFC 3339 timestamp")
	}
	issue, err := app.attentionService.SnoozeAttentionIssue(appContext(app.ctx), attention.SnoozeInput{ID: request.AttentionIssueID, Until: until})
	if err != nil {
		return SnoozeResultDTO{}, notificationTransportError("snooze attention issue", err)
	}
	result := SnoozeResultDTO{AttentionIssueID: issue.ID}
	if issue.SnoozedUntil != nil {
		result.SnoozedUntil = issue.SnoozedUntil.UTC().Format(time.RFC3339Nano)
	}
	return result, nil
}

func notificationTransportError(operation string, err error) error {
	switch {
	case errors.Is(err, attention.ErrNotificationMissing):
		return fmt.Errorf("the notification was not found")
	case errors.Is(err, attention.ErrIssueNotFound):
		return fmt.Errorf("the attention issue was not found")
	case errors.Is(err, attention.ErrNotDelivered):
		return fmt.Errorf("the notification has not been delivered")
	case errors.Is(err, attention.ErrIssueNotOpen):
		return fmt.Errorf("the attention issue is already resolved")
	case errors.Is(err, attention.ErrInvalidInput):
		return err
	}
	slog.Error("notification operation failed", "operation", operation, "error", err)
	return fmt.Errorf("%s failed", operation)
}
