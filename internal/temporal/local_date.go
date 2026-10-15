package temporal

import "time"

// LocalDate represents a calendar date without a time-of-day or timezone.
//
// Example:
//
//	2026-09-18
//
// A LocalDate must never be converted into an artificial midnight
// timestamp merely for storage or comparison.
type LocalDate string

const LocalDateLayout = "2006-01-02"

// ParseLocalDate validates a YYYY-MM-DD value and converts it into
// a LocalDate.
func ParseLocalDate(value string) (LocalDate, error) {
	if _, err := time.Parse(LocalDateLayout, value); err != nil {
		return "", err
	}

	return LocalDate(value), nil
}

// Valid reports whether the value represents a valid calendar date.
func (d LocalDate) Valid() bool {
	_, err := time.Parse(LocalDateLayout, string(d))
	return err == nil
}

// String returns the database/transport representation of the date.
func (d LocalDate) String() string {
	return string(d)
}
