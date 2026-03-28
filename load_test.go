package pageserve

import (
	"strings"
	"testing"
)

const validLoadConfig = `
server:
  address: ":8080"
session:
  cookie_name: sess
  keys:
    - id: k1
      env: KEY1
routes: []
`

func TestLoad_ValidConfig(t *testing.T) {
	path := writeConfig(t, validLoadConfig)
	cfg, err := Load(path, WithEnv(map[string]string{"KEY1": b64("secret")}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Address != ":8080" {
		t.Errorf("Server.Address = %q", cfg.Server.Address)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_MissingRequiredField(t *testing.T) {
	// No server.address
	path := writeConfig(t, `
session:
  cookie_name: sess
  keys:
    - id: k1
      env: KEY1
routes: []
`)
	_, err := Load(path, WithEnv(map[string]string{"KEY1": b64("v")}))
	if err == nil {
		t.Fatal("expected error for missing server.address")
	}
	if !strings.Contains(err.Error(), "server.address") {
		t.Errorf("error should mention server.address: %v", err)
	}
}

func TestLoad_UnresolvedSecret(t *testing.T) {
	path := writeConfig(t, validLoadConfig)
	// KEY1 not provided in env
	_, err := Load(path, WithEnv(map[string]string{}))
	if err == nil {
		t.Fatal("expected error for unresolved secret")
	}
	if !strings.Contains(err.Error(), "KEY1") {
		t.Errorf("error should mention KEY1: %v", err)
	}
}

func TestLoad_NoWithEnv_UsesEmptyMap(t *testing.T) {
	// Without WithEnv, an empty map is used — secrets fail validation.
	path := writeConfig(t, validLoadConfig)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error when no env provided and secret declared")
	}
}

func TestLoad_UnknownAccessPolicyRef(t *testing.T) {
	path := writeConfig(t, `
server:
  address: ":8080"
session:
  cookie_name: sess
  keys:
    - id: k1
      env: KEY1
routes:
  - path: /private
    handler: redirect
    to: /
    access: undefined-policy
`)
	_, err := Load(path, WithEnv(map[string]string{"KEY1": b64("v")}))
	if err == nil {
		t.Fatal("expected error for undefined auth policy")
	}
	if !strings.Contains(err.Error(), "undefined-policy") {
		t.Errorf("error should mention the policy name: %v", err)
	}
}

func TestLoad_UnsupportedOAuthProvider(t *testing.T) {
	path := writeConfig(t, `
server:
  address: ":8080"
oauth:
  providers:
    - provider: github
      client_id: cid
      client_secret:
        env: SECRET
session:
  cookie_name: sess
  keys:
    - id: k1
      env: KEY1
routes: []
`)
	_, err := Load(path, WithEnv(map[string]string{"SECRET": "s", "KEY1": b64("k")}))
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
	if !strings.Contains(err.Error(), "github") {
		t.Errorf("error should mention the provider name: %v", err)
	}
}

func TestLoad_ReturnedConfigIsMutable(t *testing.T) {
	path := writeConfig(t, validLoadConfig)
	cfg, err := Load(path, WithEnv(map[string]string{"KEY1": b64("v")}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Caller should be able to mutate the config (e.g. CLI flag override).
	cfg.Server.Address = ":9090"
	if cfg.Server.Address != ":9090" {
		t.Error("config should be mutable after Load")
	}
}
