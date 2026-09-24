package config

import "testing"

// TestValidateAcceptsEmptyKeycloakClientSecret guards a change that has to hold
// before the KEYCLOAK_CLIENT_SECRET key can be removed from the live Secret:
// with the key gone, ClientSecret loads as "" and Validate must still pass. If
// this regresses, every aether-backend pod fails to start on the next restart.
//
// The secret is not used by any live path — verification is RSA against the
// realm keys and user creation authenticates as admin-cli — so requiring it
// only ever forced a value nobody read, which is how a rotated-away credential
// sat in the cluster unnoticed.
func TestValidateAcceptsEmptyKeycloakClientSecret(t *testing.T) {
	base := func() *Config {
		return &Config{
			Neo4j: DatabaseConfig{Password: "set"},
			Keycloak: KeycloakConfig{
				Enabled:  true,
				URL:      "http://keycloak-shared.tas-shared:8080",
				Realm:    "aether",
				ClientID: "aether-backend",
			},
		}
	}

	t.Run("empty client secret is accepted", func(t *testing.T) {
		c := base()
		c.Keycloak.ClientSecret = ""
		if err := c.Validate(); err != nil {
			t.Fatalf("Validate() with an empty Keycloak client secret returned %v, want nil", err)
		}
	})

	t.Run("a set client secret is still accepted", func(t *testing.T) {
		c := base()
		c.Keycloak.ClientSecret = "anything"
		if err := c.Validate(); err != nil {
			t.Fatalf("Validate() returned %v, want nil", err)
		}
	})

	// The neighbouring requirements must not have been loosened by accident.
	t.Run("an empty Neo4j password is still rejected", func(t *testing.T) {
		c := base()
		c.Neo4j.Password = ""
		if err := c.Validate(); err == nil {
			t.Fatal("Validate() accepted an empty NEO4J_PASSWORD, want an error")
		}
	})
}
