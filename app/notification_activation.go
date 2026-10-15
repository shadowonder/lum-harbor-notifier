package app

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const NotificationActivationEvent = "lumharbor:notification-activate"

const maxPendingNotificationActivations = 16

// NotificationActivationDTO is a presentation/navigation handoff from the
// native macOS notification plugin. It carries existing identity only and
// never creates or mutates domain records by itself.
type NotificationActivationDTO struct {
	NotificationID string  `json:"notificationId"`
	PlannerItemID  *string `json:"plannerItemId,omitempty"`
	WorkItemID     *string `json:"workItemId,omitempty"`
}

func ParseNotificationActivationURL(raw string) (NotificationActivationDTO, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return NotificationActivationDTO{}, fmt.Errorf("parse notification activation URL: %w", err)
	}
	if parsed.Scheme != "lumharbor" || parsed.Host != "notification" || parsed.Path != "/activate" {
		return NotificationActivationDTO{}, fmt.Errorf("unsupported notification activation URL")
	}
	query := parsed.Query()
	notificationID := strings.TrimSpace(query.Get("notificationId"))
	if notificationID == "" {
		return NotificationActivationDTO{}, fmt.Errorf("notification activation URL requires notificationId")
	}
	return NotificationActivationDTO{
		NotificationID: notificationID,
		PlannerItemID:  optionalQueryValue(query, "plannerItemId"),
		WorkItemID:     optionalQueryValue(query, "workItemId"),
	}, nil
}

func optionalQueryValue(values url.Values, key string) *string {
	value := strings.TrimSpace(values.Get(key))
	if value == "" {
		return nil
	}
	return &value
}

// NotificationURLHandler returns the callback installed in Wails' macOS
// options without exposing URL injection as a bound frontend method.
func NotificationURLHandler(a *App) func(string) {
	return a.handleNotificationURL
}

// handleNotificationURL is installed only as Wails' macOS URL-open callback.
// Invalid or unrelated URLs fail closed. Valid activations are kept in a
// small in-memory startup buffer and also emitted to an already-running UI.
func (a *App) handleNotificationURL(raw string) {
	activation, err := ParseNotificationActivationURL(raw)
	if err != nil {
		return
	}
	a.notificationActivationMu.Lock()
	if !a.notificationActivationUIReady {
		a.pendingNotificationActivations = append(a.pendingNotificationActivations, activation)
		if len(a.pendingNotificationActivations) > maxPendingNotificationActivations {
			a.pendingNotificationActivations = a.pendingNotificationActivations[len(a.pendingNotificationActivations)-maxPendingNotificationActivations:]
		}
		a.notificationActivationMu.Unlock()
		return
	}
	ctx := a.ctx
	a.notificationActivationMu.Unlock()
	if ctx != nil {
		runtime.EventsEmit(ctx, NotificationActivationEvent, activation)
	}
}

// ConsumeNotificationActivations drains clicks that arrived before the
// frontend subscribed. Once drained, later clicks are emitted directly.
func (a *App) ConsumeNotificationActivations() []NotificationActivationDTO {
	a.notificationActivationMu.Lock()
	defer a.notificationActivationMu.Unlock()
	if a.ctx != nil {
		a.notificationActivationUIReady = true
	}
	items := append([]NotificationActivationDTO(nil), a.pendingNotificationActivations...)
	a.pendingNotificationActivations = nil
	return items
}
