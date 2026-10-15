package ai

import (
	"context"
	"encoding/json"
	"fmt"
)

// ResponseValidator applies action/output-schema-specific validation after all
// post middleware. JSON syntax validation always runs before this extension.
type ResponseValidator interface {
	Validate(ctx context.Context, request Request, response Response) error
}

func validateJSONResponse(response Response) error {
	if len(response.Output) == 0 {
		return fmt.Errorf("response output must not be empty")
	}
	if !json.Valid(response.Output) {
		return fmt.Errorf("response output must be valid JSON")
	}
	return nil
}
