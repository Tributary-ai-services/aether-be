package config

import "testing"

// The C.6 flag defaults off, so merging the change is inert.
func TestMCPProxyDefaultsOff(t *testing.T) {
	t.Setenv("AETHER_MCP_VIA_PROXY", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MCPProxy.Enabled {
		t.Fatal("AETHER_MCP_VIA_PROXY must default to false")
	}
	if cfg.MCPProxy.PostgresServerID != "postgres-mcp" || cfg.MCPProxy.ServerIDs["mcp-postgres"] != "postgres-mcp" {
		t.Fatalf("defaults: %+v", cfg.MCPProxy)
	}
}

func TestParseServerIDMap(t *testing.T) {
	if got := parseServerIDMap(""); len(got) != len(DefaultProxyServerIDs) {
		t.Fatalf("empty should use the defaults, got %v", got)
	}
	got := parseServerIDMap(" mcp-postgres = pg-2 , broken, =x, mcp-kafka=kafka-mcp ")
	if len(got) != 2 || got["mcp-postgres"] != "pg-2" || got["mcp-kafka"] != "kafka-mcp" {
		t.Fatalf("got %v", got)
	}
	// The defaults map is never aliased by the returned copy.
	parseServerIDMap("")["mcp-postgres"] = "changed"
	if DefaultProxyServerIDs["mcp-postgres"] != "postgres-mcp" {
		t.Fatal("parseServerIDMap returned the shared defaults map")
	}
}
