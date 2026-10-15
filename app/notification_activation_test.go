package app

import (
	"fmt"
	"testing"
)

func TestParseNotificationActivationURLPreservesExistingIdentity(t *testing.T) {
	activation, err := ParseNotificationActivationURL("lumharbor://notification/activate?notificationId=n-1&plannerItemId=todo%2F1")
	if err != nil {
		t.Fatal(err)
	}
	if activation.NotificationID != "n-1" {
		t.Fatalf("unexpected activation identity: %+v", activation)
	}
	if activation.PlannerItemID == nil || *activation.PlannerItemID != "todo/1" {
		t.Fatalf("unexpected planner target: %+v", activation)
	}
}

func TestNotificationActivationHandoffIsBoundedInMemoryAndRejectsUnrelatedURLs(t *testing.T) {
	a := &App{}
	a.handleNotificationURL("https://example.com/not-a-lum-harbor-activation")
	if got := a.ConsumeNotificationActivations(); len(got) != 0 {
		t.Fatalf("unrelated URL entered activation handoff: %+v", got)
	}

	a.handleNotificationURL("lumharbor://notification/activate?notificationId=n-1&workItemId=wi-1")
	got := a.ConsumeNotificationActivations()
	if len(got) != 1 || got[0].WorkItemID == nil || *got[0].WorkItemID != "wi-1" {
		t.Fatalf("unexpected activation handoff: %+v", got)
	}
	if again := a.ConsumeNotificationActivations(); len(again) != 0 {
		t.Fatalf("activation handoff must drain once: %+v", again)
	}

	for i := 0; i < maxPendingNotificationActivations+2; i++ {
		a.handleNotificationURL(fmt.Sprintf("lumharbor://notification/activate?notificationId=n-%d", i))
	}
	bounded := a.ConsumeNotificationActivations()
	if len(bounded) != maxPendingNotificationActivations {
		t.Fatalf("activation handoff size = %d, want %d", len(bounded), maxPendingNotificationActivations)
	}
	if bounded[0].NotificationID != "n-2" {
		t.Fatalf("activation handoff retained %q first, want newest bounded window", bounded[0].NotificationID)
	}

	a.notificationActivationUIReady = true
	a.handleNotificationURL("lumharbor://notification/activate?notificationId=live")
	if queued := a.ConsumeNotificationActivations(); len(queued) != 0 {
		t.Fatalf("live activation must use the event path only, got queued copy: %+v", queued)
	}
}
