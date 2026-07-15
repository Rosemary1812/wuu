package toolresult

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStructuredContentIndexJSONPreservesBoundedSemanticValues(t *testing.T) {
	secret := strings.Repeat("evidence-", 100)
	encoded := StructuredContentIndexJSON(json.RawMessage(`{"nested":{"token":"abc"},"status":"ready","count":3,"payload":"` + secret + `"}`))
	if encoded == "" {
		t.Fatal("structured index is empty")
	}
	var index map[string]any
	if err := json.Unmarshal([]byte(encoded), &index); err != nil {
		t.Fatalf("decode index: %v", err)
	}
	preview, ok := index["value_preview"].(map[string]any)
	if !ok || preview["status"] != "ready" || preview["count"] != float64(3) {
		t.Fatalf("semantic preview = %#v", index["value_preview"])
	}
	if strings.Contains(encoded, secret) || index["preview_truncated"] != true {
		t.Fatalf("structured preview was not bounded: %s", encoded)
	}
	if hash, _ := index["sha256"].(string); len(hash) != 64 {
		t.Fatalf("sha256 = %q", hash)
	}
}

func TestStructuredContentIndexJSONCanonicalHashIgnoresObjectKeyOrder(t *testing.T) {
	first := StructuredContentIndexJSON(json.RawMessage(`{"b":2,"a":1}`))
	second := StructuredContentIndexJSON(json.RawMessage(`{"a":1,"b":2}`))
	var firstIndex, secondIndex map[string]any
	if err := json.Unmarshal([]byte(first), &firstIndex); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(second), &secondIndex); err != nil {
		t.Fatal(err)
	}
	if firstIndex["sha256"] != secondIndex["sha256"] {
		t.Fatalf("canonical hashes differ: %v != %v", firstIndex["sha256"], secondIndex["sha256"])
	}
}
