package streaming

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"

	tasevents "github.com/Tributary-ai-services/aether-shared/go-events"
	"github.com/Tributary-ai-services/aether-shared/go-events/kafkabind"
)

func TestParseMessage_CloudEvents(t *testing.T) {
	ce := tasevents.New("com.tas.activity.document.uploaded", "urn:tas:service:audimodal",
		tasevents.WithTenant("t-acme", "sp-1"),
		tasevents.WithData(map[string]string{"file_id": "f1"}),
	)
	value, headers, _ := kafkabind.Encode(ce)

	msg := kafka.Message{Value: value, Headers: headers}
	parsed, err := ParseMessage(msg)
	if err != nil {
		t.Fatalf("ParseMessage CE: %v", err)
	}
	if parsed.SpecVersion != "1.0" {
		t.Errorf("specversion = %q", parsed.SpecVersion)
	}
	if parsed.Type != "com.tas.activity.document.uploaded" {
		t.Errorf("type = %q", parsed.Type)
	}
	if parsed.TenantID != "t-acme" {
		t.Errorf("tenantid = %q", parsed.TenantID)
	}
}

func TestParseMessage_Legacy(t *testing.T) {
	legacy := map[string]interface{}{
		"schema_version": "1",
		"event_id":       "legacy-uuid",
		"event_type":     "document.uploaded",
		"timestamp":      time.Now().UTC().Format(time.RFC3339),
		"tenant_id":      "t-acme",
		"user_id":        "u-alice",
		"request_id":     "r-1",
		"source_service": "audimodal",
		"payload":        map[string]string{"file_id": "f1"},
	}
	data, _ := json.Marshal(legacy)

	msg := kafka.Message{Value: data} // no CE headers
	parsed, err := ParseMessage(msg)
	if err != nil {
		t.Fatalf("ParseMessage legacy: %v", err)
	}
	if parsed.SpecVersion != "1.0" {
		t.Errorf("specversion = %q", parsed.SpecVersion)
	}
	if parsed.ID != "legacy-uuid" {
		t.Errorf("id = %q, want legacy-uuid", parsed.ID)
	}
	if parsed.Type != "com.tas.activity.document.uploaded" {
		t.Errorf("type = %q", parsed.Type)
	}
	if parsed.Source != "urn:tas:service:audimodal" {
		t.Errorf("source = %q", parsed.Source)
	}
	if parsed.TenantID != "t-acme" {
		t.Errorf("tenantid = %q", parsed.TenantID)
	}
}

func TestParseMessage_UnknownLegacyType(t *testing.T) {
	legacy := map[string]interface{}{
		"schema_version": "1",
		"event_id":       "u1",
		"event_type":     "some.future.type",
		"timestamp":      time.Now().UTC().Format(time.RFC3339),
		"source_service": "unknown-svc",
	}
	data, _ := json.Marshal(legacy)

	msg := kafka.Message{Value: data}
	parsed, err := ParseMessage(msg)
	if err != nil {
		t.Fatalf("ParseMessage unknown: %v", err)
	}
	if parsed.Type != "com.tas.legacy.some.future.type" {
		t.Errorf("type = %q, expected com.tas.legacy.some.future.type", parsed.Type)
	}
}
