// Package pageserve assembles a complete HTTP server from a YAML config file.
// It sits above the page content rendering library and handles routing, auth,
// and session management.
//
// Usage:
//
//	cfg, err := pageserve.Load("config.yaml", pageserve.WithEnv(env))
//	srv, err := pageserve.Build(cfg)
package pageserve

import (
	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration struct for pageserve.
// Load returns a validated Config; the caller may mutate it before passing to Build.
type Config struct {
	Server  ServerConfig            `yaml:"server"`
	OAuth   OAuthConfig             `yaml:"oauth"`
	Session SessionConfig           `yaml:"session"`
	Access  map[string]AccessPolicy `yaml:"access"`
	CORS        *CORSConfig             `yaml:"cors"`
	CrossOrigin *CrossOriginConfig      `yaml:"cross_origin"`
	Site    SiteConfig              `yaml:"site"`
	Routes  []RouteConfig           `yaml:"routes"`
}

// ServerConfig holds the server listener configuration.
type ServerConfig struct {
	Listeners []ListenerConfig `yaml:"listeners"`
}

// ListenerConfig describes a single network listener.
// If TLS is non-nil the listener uses HTTPS; otherwise plain HTTP.
type ListenerConfig struct {
	Address string     `yaml:"address"`
	TLS     *TLSConfig `yaml:"tls,omitempty"`
}

// TLSConfig holds the certificate and key paths for a TLS listener.
type TLSConfig struct {
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// OAuthConfig holds the OAuth provider list.
type OAuthConfig struct {
	Providers []OAuthProvider `yaml:"providers"`
}

// OAuthProvider is a single OAuth provider entry.
type OAuthProvider struct {
	Provider     string      `yaml:"provider"`
	ClientID     string      `yaml:"client_id"`
	ClientSecret SecretField `yaml:"client_secret"`
}

// SecretField is a standalone secret value referenced by environment variable name.
// The Env field names the variable; Value is populated at Parse time from the
// provided env map and is never present in config.yaml.
type SecretField struct {
	Env   string `yaml:"env"`
	Value string `yaml:"-"` // resolved at Parse time; not from YAML
}

// SessionConfig holds session cookie configuration.
type SessionConfig struct {
	CookieName string       `yaml:"cookie_name"`
	Keys       []SessionKey `yaml:"keys"`
}

// SessionKey is a session signing key referenced by environment variable name.
// The Env field names the variable; Value is populated at Parse time.
type SessionKey struct {
	ID    string `yaml:"id"`
	Env   string `yaml:"env"`
	Value string `yaml:"-"` // resolved at Parse time; not from YAML
}

// AccessPolicy is a named email allow-list policy.
type AccessPolicy struct {
	Allow []string `yaml:"allow"`
}

// CORSConfig holds cross-origin resource sharing settings applied globally to
// all routes. Omitting this section disables CORS headers entirely.
type CORSConfig struct {
	AllowedOrigins   []string `yaml:"allowed_origins"`
	AllowedMethods   []string `yaml:"allowed_methods"`
	AllowedHeaders   []string `yaml:"allowed_headers"`
	ExposedHeaders   []string `yaml:"exposed_headers"`
	AllowCredentials bool     `yaml:"allow_credentials"`
	MaxAge           int      `yaml:"max_age"`
}

// CrossOriginConfig holds the three cross-origin response headers applied globally.
// Omitting this section uses the defaults: COOP=same-origin-allow-popups,
// COEP=unsafe-none, CORP=same-origin.
type CrossOriginConfig struct {
	// COOP is the Cross-Origin-Opener-Policy value.
	// Default: "same-origin-allow-popups" (permits OAuth popup flows).
	COOP string `yaml:"coop"`
	// COEP is the Cross-Origin-Embedder-Policy value.
	// Default: "unsafe-none" (permits third-party embeds such as YouTube).
	COEP string `yaml:"coep"`
	// CORP is the Cross-Origin-Resource-Policy value.
	// Default: "same-origin".
	CORP string `yaml:"corp"`
}

// SiteConfig holds site-level rendering configuration.
type SiteConfig struct {
	BaseURL string `yaml:"base_url"`
	Name    string `yaml:"name"`
	Lang    string `yaml:"lang"`
}

// RouteConfig holds the common fields for a route entry. Handler-specific
// configuration is decoded from Raw by each HandlerFactory.
type RouteConfig struct {
	// Path is the http.ServeMux pattern for this route (e.g. "/", "GET /notes/").
	Path string `yaml:"path"`
	// Handler is the handler type name (e.g. "pages", "files", "redirect").
	Handler string `yaml:"handler"`
	// Access names a policy in the top-level access: section. When set, requests
	// without a valid matching session are redirected to OAuth login.
	Access string `yaml:"access,omitempty"`

	// Raw is the raw YAML mapping node for this route, populated at Parse time.
	// It is passed to HandlerFactory (both built-in and custom).
	Raw *yaml.Node `yaml:"-"`

	// builder is the HandlerBuilder produced by the factory during Build's parse
	// phase. It holds the decoded handler config and implements Validate and Build.
	builder HandlerBuilder
}
