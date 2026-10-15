package source

import (
	"encoding/json"
	"testing"
)

func TestCanonicalizePayloadIgnoresObjectFormattingButPreservesMeaning(t *testing.T) {
	t.Parallel()

	canonicalA, hashA, err := CanonicalizePayload(json.RawMessage(`{"a":1,"b":[2,3]}`))
	if err != nil {
		t.Fatal(err)
	}
	canonicalB, hashB, err := CanonicalizePayload(json.RawMessage("{\n  \"b\": [2, 3],\n  \"a\": 1\n}"))
	if err != nil {
		t.Fatal(err)
	}
	_, hashChanged, err := CanonicalizePayload(json.RawMessage(`{"a":2,"b":[2,3]}`))
	if err != nil {
		t.Fatal(err)
	}

	if string(canonicalA) != string(canonicalB) || hashA != hashB {
		t.Fatalf("equivalent JSON differed: %s/%s vs %s/%s", canonicalA, hashA, canonicalB, hashB)
	}
	if hashChanged == hashA {
		t.Fatal("changed JSON value produced the same hash")
	}
	if string(canonicalA) != `{"a":1,"b":[2,3]}` {
		t.Fatalf("unexpected canonical payload: %s", canonicalA)
	}
}

func TestCanonicalizePayloadRejectsInvalidOrTrailingJSON(t *testing.T) {
	t.Parallel()
	for _, payload := range []string{"", `{`, `{} {}`} {
		if _, _, err := CanonicalizePayload(json.RawMessage(payload)); err == nil {
			t.Fatalf("expected %q to be rejected", payload)
		}
	}
}
