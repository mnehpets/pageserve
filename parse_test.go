package pageserve

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func b64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(f, []byte(content), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return f
}

const minimalConfig = `
server:
  listeners:
    - address: ":8080"
session:
  cookie_name: sess
  keys:
    - id: k1
      env: KEY1
routes: []
`

func TestParse_MinimalConfig(t *testing.T) {
	path := writeConfig(t, minimalConfig)
	cfg, err := parse(path, map[string]string{"KEY1": b64("secret")})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cfg.Server.Listeners) != 1 || cfg.Server.Listeners[0].Address != ":8080" {
		t.Errorf("Server.Listeners = %v, want [{:8080}]", cfg.Server.Listeners)
	}
	if cfg.Session.CookieName != "sess" {
		t.Errorf("Session.CookieName = %q, want %q", cfg.Session.CookieName, "sess")
	}
	if len(cfg.Session.Keys) != 1 {
		t.Fatalf("len(Session.Keys) = %d, want 1", len(cfg.Session.Keys))
	}
	if cfg.Session.Keys[0].ID != "k1" {
		t.Errorf("Session.Keys[0].ID = %q, want %q", cfg.Session.Keys[0].ID, "k1")
	}
}

func TestParse_SecretResolution(t *testing.T) {
	yaml := `
server:
  listeners:
    - address: ":8080"
oauth:
  providers:
    - provider: google
      client_id: myclient
      client_secret:
        env: OAUTH_SECRET
session:
  cookie_name: sess
  keys:
    - id: k1
      env: KEY1
    - id: k2
      env: KEY2
routes: []
`
	path := writeConfig(t, yaml)
	env := map[string]string{
		"OAUTH_SECRET": "oauth-value",
		"KEY1":         b64("key1-value"),
		"KEY2":         b64("key2-value"),
	}
	cfg, err := parse(path, env)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got := cfg.OAuth.Providers[0].ClientSecret.Value; got != "oauth-value" {
		t.Errorf("ClientSecret.Value = %q, want %q", got, "oauth-value")
	}
	if got := cfg.Session.Keys[0].Value; got != "key1-value" {
		t.Errorf("Keys[0].Value = %q, want %q", got, "key1-value")
	}
	if got := cfg.Session.Keys[1].Value; got != "key2-value" {
		t.Errorf("Keys[1].Value = %q, want %q", got, "key2-value")
	}
}

func TestParse_SecretEnvNotPresent_ValueEmpty(t *testing.T) {
	// parse() resolves secrets but does not error on missing vars — that's validate's job.
	path := writeConfig(t, minimalConfig)
	cfg, err := parse(path, map[string]string{}) // KEY1 absent
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Session.Keys[0].Value != "" {
		t.Errorf("expected empty Value when env var absent, got %q", cfg.Session.Keys[0].Value)
	}
}

func TestParse_RawNodesAttachedToRoutes(t *testing.T) {
	yaml := `
server:
  listeners:
    - address: ":8080"
session:
  cookie_name: sess
  keys:
    - id: k1
      env: K
routes:
  - path: /
    handler: redirect
    to: /home/
  - path: /about
    handler: redirect
    to: /about/
`
	path := writeConfig(t, yaml)
	cfg, err := parse(path, map[string]string{"K": b64("v")})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cfg.Routes) != 2 {
		t.Fatalf("len(Routes) = %d, want 2", len(cfg.Routes))
	}
	for i, r := range cfg.Routes {
		if r.Raw == nil {
			t.Errorf("Routes[%d].Raw is nil", i)
		}
	}
}

func TestParse_AllSections(t *testing.T) {
	yaml := `
server:
  listeners:
    - address: ":9090"
    - address: ":9443"
      tls:
        cert_file: /etc/tls/cert.pem
        key_file:  /etc/tls/key.pem
oauth:
  providers:
    - provider: google
      client_id: cid
      client_secret:
        env: CS
session:
  cookie_name: myapp
  keys:
    - id: primary
      env: KEY
access:
  staff:
    allow: ["*@example.com"]
site:
  base_url: https://example.com
  name: My Site
  lang: fr
routes:
  - path: /
    handler: redirect
    to: /home/
`
	path := writeConfig(t, yaml)
	cfg, err := parse(path, map[string]string{"CS": "s", "KEY": b64("k")})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cfg.Server.Listeners) != 2 {
		t.Fatalf("len(Server.Listeners) = %d, want 2", len(cfg.Server.Listeners))
	}
	if cfg.Server.Listeners[0].Address != ":9090" || cfg.Server.Listeners[0].TLS != nil {
		t.Errorf("Listeners[0] = %+v, want plain :9090", cfg.Server.Listeners[0])
	}
	l1 := cfg.Server.Listeners[1]
	if l1.Address != ":9443" || l1.TLS == nil || l1.TLS.CertFile != "/etc/tls/cert.pem" || l1.TLS.KeyFile != "/etc/tls/key.pem" {
		t.Errorf("Listeners[1] = %+v, want TLS :9443", l1)
	}
	if cfg.Site.BaseURL != "https://example.com" {
		t.Errorf("Site.BaseURL = %q", cfg.Site.BaseURL)
	}
	if cfg.Site.Lang != "fr" {
		t.Errorf("Site.Lang = %q, want fr", cfg.Site.Lang)
	}
	if _, ok := cfg.Access["staff"]; !ok {
		t.Error("access policy 'staff' not parsed")
	}
	if cfg.Access["staff"].Allow[0] != "*@example.com" {
		t.Errorf("Access[staff].Allow[0] = %q", cfg.Access["staff"].Allow[0])
	}
}

func TestParse_InvalidYAML(t *testing.T) {
	path := writeConfig(t, "{ invalid yaml: [")
	_, err := parse(path, nil)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestParse_FileNotFound(t *testing.T) {
	_, err := parse("/nonexistent/config.yaml", nil)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestParse_HandlerConfigInRaw(t *testing.T) {
	// Handler-specific fields (dir, index_html, etc.) are not decoded into RouteConfig;
	// they are available via Raw for the handler factory to decode at Build time.
	yamlDoc := `
server:
  listeners:
    - address: ":8080"
session:
  cookie_name: s
  keys:
    - id: k
      env: K
routes:
  - path: /assets/
    handler: files
    dir: ./static
    index_html: false
`
	path := writeConfig(t, yamlDoc)
	cfg, err := parse(path, map[string]string{"K": b64("v")})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Routes[0].Raw == nil {
		t.Fatal("Raw should be populated so handler factories can decode handler-specific fields")
	}
	// Verify the raw node contains the dir and index_html values.
	var rc struct {
		Dir       string `yaml:"dir"`
		IndexHTML *bool  `yaml:"index_html"`
	}
	if err := cfg.Routes[0].Raw.Decode(&rc); err != nil {
		t.Fatalf("decode Raw: %v", err)
	}
	if rc.Dir != "./static" {
		t.Errorf("dir = %q, want %q", rc.Dir, "./static")
	}
	if rc.IndexHTML == nil || *rc.IndexHTML != false {
		t.Errorf("index_html should decode to false from Raw")
	}
}
