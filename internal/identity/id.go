package identity

import (
	"fmt"

	"github.com/google/uuid"
)

// New returns a new UUIDv7 string.
func New() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate UUIDv7: %w", err)
	}

	return id.String(), nil
}
