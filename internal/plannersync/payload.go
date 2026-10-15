package plannersync

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/temporal"
)

// reminderDueKind is the closed due-shape vocabulary a Reminder payload must
// declare, matching the three shapes Slice 0 proved EventKit distinguishes.
type reminderDueKind string

const (
	reminderDueNone     reminderDueKind = "NONE"
	reminderDueDateOnly reminderDueKind = "DATE_ONLY"
	reminderDueTimed    reminderDueKind = "TIMED"
)

// reminderPayloadDTO is the minimal typed structured contract a REMINDER
// SourceRecord's current payload must satisfy. It intentionally does not
// carry any identity field: the reconciler always uses the SourceRecord's own
// (extension_id, instance_id, source_kind, external_id) identity, never a
// second identity that might happen to also live in the payload.
//
// List is deliberately decoded but never mapped into Planner native state:
// it exists to prove that a payload can change (and produce a new
// SourceRevision) in a way the Planner domain does not care about at all.
type reminderPayloadDTO struct {
	Title       string  `json:"title"`
	Notes       *string `json:"notes"`
	List        *string `json:"list"`
	Completed   bool    `json:"completed"`
	CompletedAt *string `json:"completedAt"`
	DueKind     string  `json:"dueKind"`
	DueYear     *int    `json:"dueYear"`
	DueMonth    *int    `json:"dueMonth"`
	DueDay      *int    `json:"dueDay"`
	DueHour     *int    `json:"dueHour"`
	DueMinute   *int    `json:"dueMinute"`
	DueSecond   *int    `json:"dueSecond"`
	TimeZone    *string `json:"timeZone"`
}

// reminderNativeState is the decoded, validated Planner-native shape of one
// Reminder observation. It carries exactly the fields Planner's
// external-sync seam (Slice 2A) accepts — no more.
type reminderNativeState struct {
	Title       string
	Details     *string
	Status      planner.Status
	CompletedAt *time.Time
	DueAt       *time.Time
	DueDate     *temporal.LocalDate
}

// errMalformedReminderPayload wraps every payload-shape rejection so callers
// can classify a decode/validation failure distinctly from a downstream
// Planner/Foundation error, without needing to parse error text.
var errMalformedReminderPayload = errors.New("malformed reminder payload")

// decodeReminderPayload parses and validates one REMINDER SourceRecord's
// current canonical payload into reminderNativeState. It never mutates
// anything and never invents a value (e.g. no time.Now() completion
// timestamp, no guessed timezone) — every value is either present and valid
// or the whole record is rejected.
func decodeReminderPayload(raw json.RawMessage) (reminderNativeState, error) {
	var dto reminderPayloadDTO
	if err := json.Unmarshal(raw, &dto); err != nil {
		return reminderNativeState{}, fmt.Errorf("%w: invalid JSON: %v", errMalformedReminderPayload, err)
	}

	dueAt, dueDate, err := mapReminderDue(dto)
	if err != nil {
		return reminderNativeState{}, err
	}
	status, completedAt, err := mapReminderCompletion(dto)
	if err != nil {
		return reminderNativeState{}, err
	}

	return reminderNativeState{
		Title: dto.Title, Details: dto.Notes,
		Status: status, CompletedAt: completedAt,
		DueAt: dueAt, DueDate: dueDate,
	}, nil
}

// mapReminderDue maps the payload's declared dueKind plus its date/time
// components into exactly one of (nil, nil) / (nil, date) / (exact, nil). It
// rejects any shape that contradicts its own declared dueKind rather than
// guessing which fields to trust.
func mapReminderDue(dto reminderPayloadDTO) (*time.Time, *temporal.LocalDate, error) {
	switch reminderDueKind(dto.DueKind) {
	case reminderDueNone:
		if dto.DueYear != nil || dto.DueMonth != nil || dto.DueDay != nil ||
			dto.DueHour != nil || dto.DueMinute != nil || dto.DueSecond != nil || dto.TimeZone != nil {
			return nil, nil, fmt.Errorf("%w: due kind NONE must not include any date or time component", errMalformedReminderPayload)
		}
		return nil, nil, nil

	case reminderDueDateOnly:
		if dto.DueYear == nil || dto.DueMonth == nil || dto.DueDay == nil {
			return nil, nil, fmt.Errorf("%w: due kind DATE_ONLY requires year, month, and day", errMalformedReminderPayload)
		}
		if dto.DueHour != nil || dto.DueMinute != nil || dto.DueSecond != nil {
			return nil, nil, fmt.Errorf("%w: due kind DATE_ONLY must not include hour, minute, or second", errMalformedReminderPayload)
		}
		localDate, err := temporal.ParseLocalDate(fmt.Sprintf("%04d-%02d-%02d", *dto.DueYear, *dto.DueMonth, *dto.DueDay))
		if err != nil {
			return nil, nil, fmt.Errorf("%w: due kind DATE_ONLY has an invalid calendar date: %v", errMalformedReminderPayload, err)
		}
		return nil, &localDate, nil

	case reminderDueTimed:
		if dto.DueYear == nil || dto.DueMonth == nil || dto.DueDay == nil ||
			dto.DueHour == nil || dto.DueMinute == nil || dto.DueSecond == nil {
			return nil, nil, fmt.Errorf("%w: due kind TIMED requires year, month, day, hour, minute, and second", errMalformedReminderPayload)
		}
		if dto.TimeZone == nil || strings.TrimSpace(*dto.TimeZone) == "" {
			return nil, nil, fmt.Errorf("%w: due kind TIMED requires a timezone identifier", errMalformedReminderPayload)
		}
		loc, err := time.LoadLocation(*dto.TimeZone)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: due kind TIMED has an unrecognized timezone %q: %v", errMalformedReminderPayload, *dto.TimeZone, err)
		}
		at := time.Date(*dto.DueYear, time.Month(*dto.DueMonth), *dto.DueDay, *dto.DueHour, *dto.DueMinute, *dto.DueSecond, 0, loc).UTC()
		return &at, nil, nil

	default:
		return nil, nil, fmt.Errorf("%w: unrecognized due kind %q", errMalformedReminderPayload, dto.DueKind)
	}
}

// mapReminderCompletion maps completed/completedAt into a Planner Status and
// an optional completion timestamp, never inventing time.Now(). A reopen
// (completed=false) always clears any completion timestamp, matching the
// EventKit-observed reality (Slice 0) that reopening clears completionDate.
func mapReminderCompletion(dto reminderPayloadDTO) (planner.Status, *time.Time, error) {
	if !dto.Completed {
		return planner.StatusOpen, nil, nil
	}
	if dto.CompletedAt == nil || strings.TrimSpace(*dto.CompletedAt) == "" {
		return "", nil, fmt.Errorf("%w: completed=true requires a completion timestamp", errMalformedReminderPayload)
	}
	parsed, err := time.Parse(time.RFC3339Nano, *dto.CompletedAt)
	if err != nil {
		return "", nil, fmt.Errorf("%w: completion timestamp %q is not valid RFC3339: %v", errMalformedReminderPayload, *dto.CompletedAt, err)
	}
	utc := parsed.UTC()
	return planner.StatusCompleted, &utc, nil
}
