package services

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// ProviderID is a stable string identity. New providers use UUID v4 while the
// decoder still accepts legacy numeric IDs so existing configuration can be
// loaded and saved without breaking group references or historical state.
type ProviderID string

func NewProviderID() ProviderID {
	return ProviderID(uuid.NewString())
}

func (id ProviderID) String() string {
	return string(id)
}

func (id ProviderID) IsZero() bool {
	return strings.TrimSpace(string(id)) == ""
}

// Scan keeps historical SQLite INTEGER columns readable after Provider IDs
// moved to UUID strings. New TEXT values and legacy numeric values normalize to
// the same in-memory string representation.
func (id *ProviderID) Scan(value any) error {
	if id == nil {
		return fmt.Errorf("cannot scan provider id into nil receiver")
	}
	switch value := value.(type) {
	case nil:
		*id = ""
	case string:
		*id = ProviderID(strings.TrimSpace(value))
	case []byte:
		*id = ProviderID(strings.TrimSpace(string(value)))
	case int64:
		*id = ProviderID(strconv.FormatInt(value, 10))
	default:
		return fmt.Errorf("unsupported provider id database type %T", value)
	}
	return nil
}

func (id ProviderID) Value() (driver.Value, error) {
	return string(id), nil
}

func (id *ProviderID) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*id = ""
		return nil
	}
	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*id = ProviderID(strings.TrimSpace(value))
		return nil
	}

	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return fmt.Errorf("invalid provider id: %w", err)
	}
	value := number.String()
	if strings.ContainsAny(value, ".eE") {
		return fmt.Errorf("invalid legacy provider id %q", value)
	}
	*id = ProviderID(value)
	return nil
}
