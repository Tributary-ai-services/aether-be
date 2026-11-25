package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

// AIQG-112: the public AIQG UI signs in through https://auth.air-ops.net —
// the same realm and keys as the internal Keycloak, under another hostname.
const publicIssuer = "https://auth.air-ops.net/realms/aether"

// fakeKeycloak serves OIDC discovery and a JWKS for one RSA key, standing in
// for the internal Keycloak that KEYCLOAK_URL points at.
type fakeKeycloak struct {
	srv *httptest.Server
	key *rsa.PrivateKey
	kid string
}

func newFakeKeycloak(t *testing.T) *fakeKeycloak {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	fk := &fakeKeycloak{key: key, kid: "realm-key"}
	mux := http.NewServeMux()
	mux.HandleFunc("/realms/aether/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		base := fk.srv.URL + "/realms/aether"
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                base,
			"authorization_endpoint":                base + "/protocol/openid-connect/auth",
			"token_endpoint":                        base + "/protocol/openid-connect/token",
			"jwks_uri":                              base + "/protocol/openid-connect/certs",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/realms/aether/protocol/openid-connect/certs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{jwk(&key.PublicKey, fk.kid)}})
	})
	fk.srv = httptest.NewServer(mux)
	t.Cleanup(fk.srv.Close)
	return fk
}

func jwk(pub *rsa.PublicKey, kid string) map[string]string {
	return map[string]string{
		"kty": "RSA", "alg": "RS256", "use": "sig", "kid": kid,
		"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}
}

// signToken builds an RS256 JWT with the given issuer, signed by key.
func signToken(t *testing.T, key *rsa.PrivateKey, kid, iss string) string {
	t.Helper()
	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	now := time.Now()
	signingInput := enc(map[string]string{"alg": "RS256", "typ": "JWT", "kid": kid}) + "." + enc(map[string]any{
		"iss": iss, "sub": "user-1", "aud": "aiqg-ui", "azp": "aiqg-ui",
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
		"preferred_username": "tester",
	})
	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func newClient(t *testing.T, fk *fakeKeycloak, extra []string) *KeycloakClient {
	t.Helper()
	log, err := logger.NewDefault()
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewKeycloakClient(config.KeycloakConfig{
		URL: fk.srv.URL, Realm: "aether", ClientID: "aether-backend", AllowedIssuers: extra,
	}, log)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestVerifyIDToken_ConfiguredExtraIssuerAccepted(t *testing.T) {
	fk := newFakeKeycloak(t)
	c := newClient(t, fk, []string{publicIssuer})

	claims, err := c.VerifyIDToken(context.Background(), signToken(t, fk.key, fk.kid, publicIssuer))
	if err != nil {
		t.Fatalf("public-host token with a realm signature was rejected: %v", err)
	}
	if claims.Iss != publicIssuer || claims.Sub != "user-1" {
		t.Fatalf("unexpected claims: iss=%q sub=%q", claims.Iss, claims.Sub)
	}

	// The configured provider's own issuer still works alongside it.
	if _, err := c.VerifyIDToken(context.Background(), signToken(t, fk.key, fk.kid, fk.srv.URL+"/realms/aether")); err != nil {
		t.Fatalf("provider issuer rejected once extras are configured: %v", err)
	}
}

func TestVerifyIDToken_ExtraIssuerRejectedWhenNotConfigured(t *testing.T) {
	fk := newFakeKeycloak(t)
	c := newClient(t, fk, nil)

	_, err := c.VerifyIDToken(context.Background(), signToken(t, fk.key, fk.kid, publicIssuer))
	if err == nil || !strings.Contains(err.Error(), "unauthorized issuer") {
		t.Fatalf("expected unauthorized-issuer rejection, got %v", err)
	}
}

func TestVerifyIDToken_LookAlikeIssuersRejected(t *testing.T) {
	fk := newFakeKeycloak(t)
	c := newClient(t, fk, []string{publicIssuer})

	for _, iss := range []string{
		"https://auth.air-ops.net/realms/other",
		"https://auth.air-ops.net/realms/aether/",
		"https://auth.air-ops.net/realms/aether//",
		"https://auth.air-ops.net/realms/aethe",
		"https://auth.air-ops.net/realms/aether-evil",
		"http://auth.air-ops.net/realms/aether",
		"https://auth.air-ops.net:443/realms/aether",
		"https://AUTH.air-ops.net/realms/aether",
		"https://auth.air-ops.net.evil.example/realms/aether",
		" " + publicIssuer,
		"",
	} {
		t.Run(iss, func(t *testing.T) {
			_, err := c.VerifyIDToken(context.Background(), signToken(t, fk.key, fk.kid, iss))
			if err == nil || !strings.Contains(err.Error(), "unauthorized issuer") {
				t.Fatalf("look-alike issuer %q: expected unauthorized-issuer rejection, got %v", iss, err)
			}
		})
	}
}

// A trusted `iss` does not make a token trusted: the signature must come from
// the realm key served by KEYCLOAK_URL, never from wherever `iss` points.
func TestVerifyIDToken_AllowedIssuerWithForeignSignatureRejected(t *testing.T) {
	fk := newFakeKeycloak(t)
	c := newClient(t, fk, []string{publicIssuer})

	attacker, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for _, kid := range []string{fk.kid, "attacker-key"} {
		_, err := c.VerifyIDToken(context.Background(), signToken(t, attacker, kid, publicIssuer))
		if err == nil || !strings.Contains(err.Error(), "failed to verify ID token") {
			t.Fatalf("kid %q: token signed by a foreign key was not rejected on signature: %v", kid, err)
		}
	}
}

func TestBuildAllowedIssuers_EmptyExtrasKeepsDerivedList(t *testing.T) {
	cfg := config.KeycloakConfig{URL: "http://keycloak-shared.tas-shared:8080", Realm: "aether"}
	want := []string{
		"http://keycloak-shared.tas-shared:8080/realms/aether",
		"http://keycloak.tas.scharber.com/realms/aether",
		"https://keycloak.tas.scharber.com/realms/aether",
	}
	if got := buildAllowedIssuers(cfg); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	// Production's configmap repeats the derived issuers; they must not double up.
	cfg.AllowedIssuers = append(append([]string{}, want...), publicIssuer)
	if got := buildAllowedIssuers(cfg); !reflect.DeepEqual(got, append(want, publicIssuer)) {
		t.Fatalf("with extras: got %v", got)
	}
}
