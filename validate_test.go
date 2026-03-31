package pageserve

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// resolvedConfig returns a Config with all required fields set and secrets resolved.
func resolvedConfig() Config {
	return Config{
		Server: ServerConfig{Listeners: []ListenerConfig{{Address: ":8080"}}},
		Session: SessionConfig{
			CookieName: "sess",
			Keys:       []SessionKey{{ID: "k1", Env: "KEY1", Value: "secret"}},
		},
	}
}

func TestValidate_Valid(t *testing.T) {
	cfg := resolvedConfig()
	if err := validate(cfg, nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_NoListeners(t *testing.T) {
	cfg := resolvedConfig()
	cfg.Server.Listeners = nil
	err := validate(cfg, nil)
	assertValidationError(t, err, "server.listeners")
}

func TestValidate_ListenerMissingAddress(t *testing.T) {
	cfg := resolvedConfig()
	cfg.Server.Listeners = []ListenerConfig{{Address: ""}}
	err := validate(cfg, nil)
	assertValidationError(t, err, "server.listeners[0]: address is required")
}

func TestValidate_TLSMissingCertFile(t *testing.T) {
	cfg := resolvedConfig()
	cfg.Server.Listeners = []ListenerConfig{{Address: ":8443", TLS: &TLSConfig{KeyFile: "key.pem"}}}
	err := validate(cfg, nil)
	assertValidationError(t, err, "cert_file")
}

func TestValidate_TLSMissingKeyFile(t *testing.T) {
	cfg := resolvedConfig()
	cfg.Server.Listeners = []ListenerConfig{{Address: ":8443", TLS: &TLSConfig{CertFile: "cert.pem"}}}
	err := validate(cfg, nil)
	assertValidationError(t, err, "key_file")
}

func TestValidate_MissingCookieName(t *testing.T) {
	cfg := resolvedConfig()
	cfg.Session.CookieName = ""
	err := validate(cfg, nil)
	assertValidationError(t, err, "session.cookie_name")
}

func TestValidate_NoSessionKeys(t *testing.T) {
	cfg := resolvedConfig()
	cfg.Session.Keys = nil
	err := validate(cfg, nil)
	assertValidationError(t, err, "session.keys")
}

func TestValidate_UnresolvedClientSecret(t *testing.T) {
	cfg := resolvedConfig()
	cfg.OAuth.Providers = []OAuthProvider{{
		Provider:     "google",
		ClientID:     "cid",
		ClientSecret: SecretField{Env: "MISSING_VAR", Value: ""},
	}}
	err := validate(cfg, nil)
	assertValidationError(t, err, "MISSING_VAR")
}

func TestValidate_UnresolvedSessionKey(t *testing.T) {
	cfg := resolvedConfig()
	cfg.Session.Keys = []SessionKey{{ID: "k1", Env: "MISSING_KEY", Value: ""}}
	err := validate(cfg, nil)
	assertValidationError(t, err, "MISSING_KEY")
}

func TestValidate_UnsupportedOAuthProvider(t *testing.T) {
	cfg := resolvedConfig()
	cfg.OAuth.Providers = []OAuthProvider{{
		Provider:     "github",
		ClientID:     "cid",
		ClientSecret: SecretField{Env: "S", Value: "v"},
	}}
	err := validate(cfg, nil)
	assertValidationError(t, err, "github")
}

func TestValidate_UnknownAccessReference(t *testing.T) {
	cfg := resolvedConfig()
	cfg.Routes = []RouteConfig{{Path: "/private", Handler: "redirect", Access: "nonexistent"}}
	err := validate(cfg, nil)
	assertValidationError(t, err, "nonexistent")
}

func TestValidate_AccessReferenceResolved(t *testing.T) {
	cfg := resolvedConfig()
	cfg.Access = map[string]AccessPolicy{"members": {Allow: []string{"*@example.com"}}}
	cfg.Routes = []RouteConfig{{Path: "/private", Handler: "redirect", Access: "members"}}
	if err := validate(cfg, nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_UnknownHandlerType_NilCustomHandlers(t *testing.T) {
	// When customHandlers is nil (Load path), unknown handler types are NOT checked.
	cfg := resolvedConfig()
	cfg.Routes = []RouteConfig{{Path: "/", Handler: "mytype"}}
	if err := validate(cfg, nil); err != nil {
		t.Errorf("Load path should not check handler types: %v", err)
	}
}

func TestValidate_UnknownHandlerType_BuildPath(t *testing.T) {
	// When customHandlers is provided (Build path), unknown handler types fail.
	cfg := resolvedConfig()
	cfg.Routes = []RouteConfig{{Path: "/", Handler: "unknown-type"}}
	err := validate(cfg, map[string]HandlerFactory{}) // empty custom map
	assertValidationError(t, err, "unknown-type")
}

func TestValidate_CustomHandlerType_Registered(t *testing.T) {
	cfg := resolvedConfig()
	cfg.Routes = []RouteConfig{{Path: "/", Handler: "mytype"}}
	custom := map[string]HandlerFactory{
		"mytype": func(*yaml.Node) (HandlerBuilder, error) { return new(simpleBuilder), nil },
	}
	if err := validate(cfg, custom); err != nil {
		t.Errorf("registered custom handler should be valid: %v", err)
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	cfg := Config{} // empty — many required fields missing
	err := validate(cfg, nil)
	if err == nil {
		t.Fatal("expected errors for empty config")
	}
	// Should report multiple issues in one error.
	msg := err.Error()
	for _, want := range []string{"server.listeners", "session.cookie_name", "session.keys"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q: %s", want, msg)
		}
	}
}

func TestValidate_BuiltinHandlerTypes_AllValid(t *testing.T) {
	cfg := resolvedConfig()
	cfg.Routes = []RouteConfig{
		{Path: "/", Handler: "pages"},
		{Path: "/assets/", Handler: "files"},
		{Path: "/old", Handler: "redirect"},
		{Path: "/api/", Handler: "proxy"},
		{Path: "/auth/", Handler: "auth"},
		{Path: "/debug/", Handler: "defaultmux"},
	}
	// All built-in types must pass even with an empty custom handler map.
	if err := validate(cfg, map[string]HandlerFactory{}); err != nil {
		t.Errorf("unexpected error for built-in handler types: %v", err)
	}
}

// assertValidationError fails the test if err is nil or does not contain want.
func assertValidationError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not contain %q", err.Error(), want)
	}
}
