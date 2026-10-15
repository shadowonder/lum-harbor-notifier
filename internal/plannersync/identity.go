// Package plannersync owns the deterministic reconciliation of durable
// Source Layer evidence into authoritative EXTERNAL PlannerItems. It
// coordinates internal/source (evidence read), internal/foundation
// (external identity read), and internal/planner (external-sync mutation)
// without ever writing Planner, History, or external_refs SQL itself.
package plannersync

import "strings"

// externalSystemReminderPrefix is the stable AUTHORITATIVE external_refs
// "system" namespace for Apple Reminders, per Slice 1's instance-safe
// convention: "<SYSTEM>:<instance-id>".
const externalSystemReminderPrefix = "APPLE_REMINDER"

// ExternalSystemDescription is read-only provenance for one external_refs
// system namespace this package owns: a human label for the external system,
// the Plugin Instance that observed it, and the producer scope (source kind
// and candidate extension IDs) that identifies its SourceRecords.
type ExternalSystemDescription struct {
	Label        string
	InstanceID   string
	SourceKind   string
	ExtensionIDs []string
}

// DescribeExternalSystem decodes a system namespace built by
// ReminderExternalSystem. It reports false for any namespace it does not own.
func DescribeExternalSystem(system string) (ExternalSystemDescription, bool) {
	prefix, instanceID, found := strings.Cut(system, ":")
	if !found || prefix != externalSystemReminderPrefix || strings.TrimSpace(instanceID) == "" {
		return ExternalSystemDescription{}, false
	}
	return ExternalSystemDescription{
		Label:        "Apple Reminders",
		InstanceID:   instanceID,
		SourceKind:   SourceKindReminder,
		ExtensionIDs: []string{ReminderExtensionID, LegacyReminderExtensionID},
	}, true
}

// ReminderExternalSystem builds the instance-safe external_refs "system"
// identity for one Reminders plugin Instance. Two Instances observing the
// same native external ID land in different systems and never collide,
// because instanceID is part of the resulting string.
func ReminderExternalSystem(instanceID string) string {
	return externalSystemReminderPrefix + ":" + strings.TrimSpace(instanceID)
}
