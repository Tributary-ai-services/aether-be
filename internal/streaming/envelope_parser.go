package streaming

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"

	tasevents "github.com/Tributary-ai-services/aether-shared/go-events"
	"github.com/Tributary-ai-services/aether-shared/go-events/kafkabind"
)

// ParseMessage detects the message format (CloudEvents vs legacy) and returns
// a normalized CloudEvents Event. During the migration window, legacy messages
// are converted to CE shape so the hub and Spark see one schema.
func ParseMessage(msg kafka.Message) (tasevents.Event, error) {
	if kafkabind.IsCloudEvent(msg.Headers) {
		return kafkabind.Decode(msg.Value, msg.Headers)
	}
	return parseLegacy(msg.Value)
}

// legacyEnvelope matches the old v1 wire format from audimodal/Gatekeeper.
type legacyEnvelope struct {
	SchemaVersion string          `json:"schema_version"`
	EventID       string          `json:"event_id"`
	EventType     string          `json:"event_type"`
	Timestamp     time.Time       `json:"timestamp"`
	TenantID      string          `json:"tenant_id"`
	SpaceID       string          `json:"space_id"`
	UserID        string          `json:"user_id"`
	RequestID     string          `json:"request_id"`
	SourceService string          `json:"source_service"`
	Severity      string          `json:"severity"`
	Frameworks    []string        `json:"frameworks"`
	Payload       json.RawMessage `json:"payload"`
}

// legacyTypeMap converts old event_type values to CloudEvents type strings.
var legacyTypeMap = map[string]string{
	"document.uploaded":  "com.tas.activity.document.uploaded",
	"document.processed": "com.tas.activity.document.processed",
	"document.failed":    "com.tas.activity.document.failed",
	"finding":            "com.tas.compliance.finding.detected",
	"action":             "com.tas.compliance.action.executed",
	"audit":              "com.tas.compliance.audit.recorded",
}

func parseLegacy(value []byte) (tasevents.Event, error) {
	var leg legacyEnvelope
	if err := json.Unmarshal(value, &leg); err != nil {
		return tasevents.Event{}, fmt.Errorf("unmarshal legacy: %w", err)
	}

	ceType, ok := legacyTypeMap[leg.EventType]
	if !ok {
		ceType = "com.tas.legacy." + leg.EventType
	}

	source := "urn:tas:service:" + leg.SourceService

	return tasevents.Event{
		SpecVersion:     tasevents.SpecVersion,
		ID:              leg.EventID,
		Source:          source,
		Type:            ceType,
		Time:            leg.Timestamp,
		DataContentType: "application/json",
		TenantID:        leg.TenantID,
		SpaceID:         leg.SpaceID,
		UserID:          leg.UserID,
		RequestID:       leg.RequestID,
		Severity:        tasevents.Severity(leg.Severity),
		Frameworks:      leg.Frameworks,
		Data:            leg.Payload,
	}, nil
}
