package services

import (
	"testing"

	"github.com/Tributary-ai-services/aether-be/internal/models"
)

// The source id decides which database a query runs against. DBHub tolerates a
// missing or unknown source while only one is configured, so a wrong value
// fails silently today and starts querying the wrong database the moment a
// second source is added. These tests pin the resolution rules down.
func TestResolveSource(t *testing.T) {
	tests := []struct {
		name          string
		defaultSource string
		db            *models.Database
		wantSource    string
		wantOK        bool
	}{
		{
			name:          "recorded source wins",
			defaultSource: "fallback-source",
			db:            &models.Database{ID: "abc", CRDName: "tas-postgres"},
			wantSource:    "tas-postgres",
			wantOK:        true,
		},
		{
			name:          "configured default is used when nothing is recorded",
			defaultSource: "tas-postgres",
			db:            &models.Database{ID: "abc", CRDName: ""},
			wantSource:    "tas-postgres",
			wantOK:        true,
		},
		{
			name:          "no source recorded and no default: omit rather than guess",
			defaultSource: "",
			db:            &models.Database{ID: "abc", CRDName: ""},
			wantSource:    "",
			wantOK:        false,
		},
		{
			name:          "legacy generated CR name is not a source",
			defaultSource: "",
			db:            &models.Database{ID: "abc", CRDName: "db-a1b2c3d4"},
			wantSource:    "",
			wantOK:        false,
		},
		{
			name:          "legacy generated CR name falls through to the default",
			defaultSource: "tas-postgres",
			db:            &models.Database{ID: "abc", CRDName: "db-a1b2c3d4"},
			wantSource:    "tas-postgres",
			wantOK:        true,
		},
		{
			name:          "a real source that merely starts with db- is kept",
			defaultSource: "",
			db:            &models.Database{ID: "abc", CRDName: "db-analytics"},
			wantSource:    "db-analytics",
			wantOK:        true,
		},
		{
			name:          "nil database falls back to the default",
			defaultSource: "tas-postgres",
			db:            nil,
			wantSource:    "tas-postgres",
			wantOK:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &DBHubService{defaultSource: tt.defaultSource}
			got, ok := s.resolveSource(tt.db)
			if got != tt.wantSource || ok != tt.wantOK {
				t.Errorf("resolveSource() = (%q, %v), want (%q, %v)",
					got, ok, tt.wantSource, tt.wantOK)
			}
		})
	}
}

func TestIsLegacyGeneratedCRDName(t *testing.T) {
	legacy := []string{"db-a1b2c3d4", "db-00000000", "db-deadbeef"}
	for _, name := range legacy {
		if !isLegacyGeneratedCRDName(name) {
			t.Errorf("isLegacyGeneratedCRDName(%q) = false, want true", name)
		}
	}

	// Real source ids must never be mistaken for the generated form.
	real := []string{"tas-postgres", "db-analytics", "db-a1b2c3d", "db-a1b2c3d44", "db-a1b2c3g4", "", "database"}
	for _, name := range real {
		if isLegacyGeneratedCRDName(name) {
			t.Errorf("isLegacyGeneratedCRDName(%q) = true, want false", name)
		}
	}
}

// DBHub only speaks SQL. Routing a minio, kafka or grafana connection to it
// produces a confusing DBHub-side failure instead of a clear rejection, and
// six of the ten connections registered in production are those types.
func TestDBHubSupportsType(t *testing.T) {
	supported := []models.DatabaseType{
		models.DatabaseTypePostgres,
		models.DatabaseTypeMySQL,
		models.DatabaseTypeMariaDB,
		models.DatabaseTypeSQLServer,
		models.DatabaseTypeSQLite,
	}
	for _, dt := range supported {
		if !dbhubSupportsType(dt) {
			t.Errorf("dbhubSupportsType(%q) = false, want true", dt)
		}
	}

	unsupported := []models.DatabaseType{
		models.DatabaseTypeNeo4j,
		models.DatabaseTypeMinio,
		models.DatabaseTypeKafka,
		models.DatabaseTypeGrafana,
		models.DatabaseType("unknown"),
	}
	for _, dt := range unsupported {
		if dbhubSupportsType(dt) {
			t.Errorf("dbhubSupportsType(%q) = true, want false", dt)
		}
	}
}
