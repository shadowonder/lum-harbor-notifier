package source

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
)

// CanonicalizePayload decodes one JSON value with number precision retained,
// then re-encodes it using encoding/json's deterministic object-key ordering.
// Array order is preserved because it can be semantically significant.
func CanonicalizePayload(payload json.RawMessage) (json.RawMessage, string, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, "", fmt.Errorf("decode JSON payload: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, "", err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, "", fmt.Errorf("encode canonical JSON payload: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return canonical, hex.EncodeToString(digest[:]), nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("JSON payload must contain exactly one value")
		}
		return fmt.Errorf("decode trailing JSON payload: %w", err)
	}
	return nil
}
