package pageserve

import (
	"fmt"
	"strings"
)

// builtinHandlers is the set of handler type names provided by pageserve.
var builtinHandlers = map[string]bool{
	"pages":      true,
	"files":      true,
	"redirect":   true,
	"proxy":      true,
	"auth":       true,
	"defaultmux": true,
}

// validate checks the Config for structural and referential correctness.
//
// customHandlers is the set of handler factories registered via WithHandler.
// When nil (called from Load), handler type validation is skipped because
// Load does not know about Build-time custom handlers — they are validated
// again at Build time when all factories are known.
func validate(cfg Config, customHandlers map[string]HandlerFactory) error {
	var errs []string

	// Required fields.
	if len(cfg.Server.Listeners) == 0 {
		errs = append(errs, "server.listeners: at least one listener is required")
	}
	for i, l := range cfg.Server.Listeners {
		if l.Address == "" {
			errs = append(errs, fmt.Sprintf("server.listeners[%d]: address is required", i))
		}
		if l.TLS != nil {
			if l.TLS.CertFile == "" {
				errs = append(errs, fmt.Sprintf("server.listeners[%d].tls: cert_file is required", i))
			}
			if l.TLS.KeyFile == "" {
				errs = append(errs, fmt.Sprintf("server.listeners[%d].tls: key_file is required", i))
			}
		}
	}
	if len(cfg.Session.Keys) == 0 {
		errs = append(errs, "session.keys: at least one key is required")
	}

	// Secret field resolution: all declared env vars must be present.
	for i, p := range cfg.OAuth.Providers {
		if p.ClientSecret.Env != "" && p.ClientSecret.Value == "" {
			errs = append(errs, fmt.Sprintf(
				"oauth.providers[%d].client_secret: env var %q not found",
				i, p.ClientSecret.Env,
			))
		}
	}
	for i, k := range cfg.Session.Keys {
		if k.Env != "" && k.Value == "" {
			errs = append(errs, fmt.Sprintf(
				"session.keys[%d] (id=%q): env var %q not found",
				i, k.ID, k.Env,
			))
		}
	}

	// OAuth provider types: only "google" is supported.
	for i, p := range cfg.OAuth.Providers {
		if p.Provider != "google" {
			errs = append(errs, fmt.Sprintf(
				"oauth.providers[%d]: unsupported provider %q (only \"google\" is supported)",
				i, p.Provider,
			))
		}
	}

	// Route validation: access references and (when customHandlers is known) handler types.
	for i, r := range cfg.Routes {
		if r.Access != "" {
			if _, ok := cfg.Access[r.Access]; !ok {
				errs = append(errs, fmt.Sprintf(
					"routes[%d] (path=%q): access policy %q is not defined",
					i, r.Path, r.Access,
				))
			}
		}
		// Handler type validation only when customHandlers is provided (Build time).
		if customHandlers != nil {
			if !builtinHandlers[r.Handler] {
				if _, ok := customHandlers[r.Handler]; !ok {
					errs = append(errs, fmt.Sprintf(
						"routes[%d] (path=%q): unknown handler type %q",
						i, r.Path, r.Handler,
					))
				}
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("pageserve: config validation failed:\n  %s", strings.Join(errs, "\n  "))
	}
	return nil
}
