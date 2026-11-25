package config

import (
	"reflect"
	"testing"
)

func TestParseIssuerList(t *testing.T) {
	for in, want := range map[string][]string{
		"":                                       nil,
		" , ,":                                   nil,
		"https://auth.air-ops.net/realms/aether": {"https://auth.air-ops.net/realms/aether"},
		" https://a/realms/aether , http://b/realms/aether/ ,,": {"https://a/realms/aether", "http://b/realms/aether/"},
	} {
		if got := parseIssuerList(in); !reflect.DeepEqual(got, want) {
			t.Errorf("parseIssuerList(%q) = %#v, want %#v", in, got, want)
		}
	}
}

func TestLoadReadsKeycloakAllowedIssuers(t *testing.T) {
	t.Setenv("KEYCLOAK_ALLOWED_ISSUERS", "https://auth.air-ops.net/realms/aether, https://keycloak.tas.scharber.com/realms/aether")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Keycloak.AllowedIssuers
	want := []string{"https://auth.air-ops.net/realms/aether", "https://keycloak.tas.scharber.com/realms/aether"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	t.Setenv("KEYCLOAK_ALLOWED_ISSUERS", "")
	if cfg, err = Load(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Keycloak.AllowedIssuers) != 0 {
		t.Fatalf("unset must mean no extra issuers, got %v", cfg.Keycloak.AllowedIssuers)
	}
}
