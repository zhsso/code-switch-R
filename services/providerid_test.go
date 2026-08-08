package services

import (
	"database/sql/driver"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestProviderIDAcceptsLegacyJSONNumberAndWritesString(t *testing.T) {
	var provider Provider
	if err := json.Unmarshal([]byte(`{"id":42,"name":"legacy"}`), &provider); err != nil {
		t.Fatalf("decode legacy provider ID: %v", err)
	}
	if provider.ID != "42" {
		t.Fatalf("legacy provider ID = %q, want 42", provider.ID)
	}
	encoded, err := json.Marshal(provider)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["id"] != "42" {
		t.Fatalf("migrated provider ID should be serialized as a string: %s", encoded)
	}
}

func TestNewProviderIDIsUUID(t *testing.T) {
	first := NewProviderID()
	second := NewProviderID()
	if first == second || first.IsZero() || second.IsZero() {
		t.Fatalf("generated IDs must be non-empty and unique: %q %q", first, second)
	}
	if _, err := uuid.Parse(first.String()); err != nil {
		t.Fatalf("generated Provider ID is not a UUID: %v", err)
	}
}

func TestProviderIDScansLegacyDatabaseInteger(t *testing.T) {
	var id ProviderID
	if err := id.Scan(int64(42)); err != nil {
		t.Fatalf("scan legacy integer provider ID: %v", err)
	}
	if id != "42" {
		t.Fatalf("scanned provider ID = %q, want 42", id)
	}
	value, err := id.Value()
	if err != nil {
		t.Fatalf("convert provider ID to driver value: %v", err)
	}
	if value != driver.Value("42") {
		t.Fatalf("driver value = %#v, want string 42", value)
	}
}
