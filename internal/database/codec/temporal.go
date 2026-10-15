package codec

import (
	"time"

	"lum-harbor-notifier/internal/temporal"
)

const databaseTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"

// ParseTime converts a required database timestamp into a UTC time.Time.
func ParseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}

	return parsed.UTC(), nil
}

// ParseOptionalTime converts nullable database timestamp text into
// an optional UTC time.Time.
func ParseOptionalTime(value *string) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}

	parsed, err := ParseTime(*value)
	if err != nil {
		return nil, err
	}

	return &parsed, nil
}

// FormatTime converts a timestamp into Lum Harbor's canonical
// SQLite TEXT representation.
func FormatTime(value time.Time) string {
	return value.UTC().Format(databaseTimeFormat)
}

// FormatOptionalTime converts an optional timestamp into nullable
// SQLite TEXT.
func FormatOptionalTime(value *time.Time) *string {
	if value == nil {
		return nil
	}

	formatted := FormatTime(*value)
	return &formatted
}

// ParseOptionalLocalDate converts nullable database date text into
// the shared date-only domain value.
func ParseOptionalLocalDate(
	value *string,
) (*temporal.LocalDate, error) {
	if value == nil {
		return nil, nil
	}

	date, err := temporal.ParseLocalDate(*value)
	if err != nil {
		return nil, err
	}

	return &date, nil
}

// FormatOptionalLocalDate converts an optional LocalDate into nullable
// YYYY-MM-DD database text.
func FormatOptionalLocalDate(
	value *temporal.LocalDate,
) *string {
	if value == nil {
		return nil
	}

	formatted := string(*value)
	return &formatted
}
