package pageserve

import (
	"expvar"
	"fmt"
	"maps"
	"net/http"
	"path"
	"strings"
	"time"

	httpauth "github.com/mnehpets/http/auth"
	"github.com/mnehpets/http/endpoint"
	"github.com/mnehpets/http/middleware"
	"github.com/zserge/metric"
)

// Server is the assembled HTTP server produced by Build.
// It holds server-level resources initialised before any route is built,
// and implements http.Handler by delegating to its internal mux.
type Server struct {
	SessionProc endpoint.Processor
	AuthHandler *httpauth.AuthHandler // nil if no auth route is configured
	mux         *http.ServeMux
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// buildOptions holds functional options for Build.
type buildOptions struct {
	handlers map[string]HandlerFactory
}

// BuildOption configures the Build function.
type BuildOption func(*buildOptions)

// WithHandler registers a custom handler factory for the given handler type name.
// The factory parses the raw YAML route node into a HandlerBuilder whose Validate
// and Build methods are called during the corresponding phases of Build.
func WithHandler(name string, factory HandlerFactory) BuildOption {
	return func(o *buildOptions) {
		if o.handlers == nil {
			o.handlers = make(map[string]HandlerFactory)
		}
		o.handlers[name] = factory
	}
}

// Build assembles a validated Config into a *Server.
//
// Build proceeds in three phases:
//  1. Parse: each route's HandlerFactory decodes the YAML node into a HandlerBuilder.
//  2. Validate: each HandlerBuilder's Validate method is called with the full Config.
//  3. Build: server-level resources are initialised, then each HandlerBuilder's
//     Build method is called to produce the route's http.Handler.
//
// All initialisation is eager: if any phase fails, Build returns an error
// immediately with no goroutines left running.
func Build(cfg Config, opts ...BuildOption) (*Server, error) {
	buildStart := time.Now()

	o := &buildOptions{}
	for _, opt := range opts {
		opt(o)
	}
	if o.handlers == nil {
		o.handlers = make(map[string]HandlerFactory)
	}

	// Structural validation (required fields, secret resolution, auth refs, handler types).
	if err := validate(cfg, o.handlers); err != nil {
		return nil, err
	}

	// Unified factory registry: built-ins and custom handlers in the same map.
	factories := map[string]HandlerFactory{
		"redirect":   redirectHandlerFactory(),
		"proxy":      proxyHandlerFactory(),
		"files":      filesHandlerFactory(),
		"pages":      pagesHandlerFactory(),
		"defaultmux": defaultMuxHandlerFactory(),
		"auth":       authHandlerFactory(),
	}
	maps.Copy(factories, o.handlers)

	// Phase 1: parse — each factory decodes Raw into a HandlerBuilder.
	for i, r := range cfg.Routes {
		b, err := factories[r.Handler](r.Raw)
		if err != nil {
			return nil, fmt.Errorf("pageserve: parse route %q config: %w", r.Path, err)
		}
		cfg.Routes[i].builder = b
	}

	// Phase 2: validate — handler-specific config checks.
	for _, r := range cfg.Routes {
		if err := r.builder.Validate(cfg); err != nil {
			return nil, fmt.Errorf("pageserve: route %q: %w", r.Path, err)
		}
	}

	// Initialise server-level resources: session processor and auth handler.
	srv := &Server{}
	if err := initAuth(cfg, cfg.Routes, srv); err != nil {
		return nil, err
	}

	// Derive the login redirector for protected routes.
	var authBasePath, authProviderID string
	for _, r := range cfg.Routes {
		if r.Handler == "auth" {
			authBasePath = routePathOnly(r.Path)
			// Use the first declared provider as the default login redirect target.
			authProviderID = "google"
			if len(cfg.OAuth.Providers) > 0 {
				authProviderID = cfg.OAuth.Providers[0].Provider
			}
			break
		}
	}
	onFailure := loginRedirector(srv.AuthHandler, authBasePath, authProviderID)

	// Phase 3: build — construct each route's endpoint, wrap with processors,
	// and register on the mux. This is the sole site of processor assembly.
	coop := "same-origin-allow-popups" // allow OAuth popup flows
	coep := "unsafe-none"              // allow third-party embeds (YouTube, maps, etc.)
	corp := "same-origin"
	if co := cfg.CrossOrigin; co != nil {
		if co.COOP != "" {
			coop = co.COOP
		}
		if co.COEP != "" {
			coep = co.COEP
		}
		if co.CORP != "" {
			corp = co.CORP
		}
	}
	securityOpts := []middleware.SecurityHeadersOption{
		middleware.WithCrossOriginPolicies(coop, coep, corp),
	}
	if cfg.CSP != nil && len(cfg.CSP.ExtraDirectives) > 0 {
		const defaultCSP = "default-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; upgrade-insecure-requests"
		csp := defaultCSP + "; " + strings.Join(cfg.CSP.ExtraDirectives, "; ")
		securityOpts = append(securityOpts, middleware.WithCSP(csp))
	}
	if cfg.CORS != nil {
		securityOpts = append(securityOpts, middleware.WithCORS(&middleware.CORSConfig{
			AllowedOrigins:   cfg.CORS.AllowedOrigins,
			AllowedMethods:   cfg.CORS.AllowedMethods,
			AllowedHeaders:   cfg.CORS.AllowedHeaders,
			ExposedHeaders:   cfg.CORS.ExposedHeaders,
			AllowCredentials: cfg.CORS.AllowCredentials,
			MaxAge:           cfg.CORS.MaxAge,
		}))
	}
	securityProc := middleware.NewSecurityHeadersProcessor(securityOpts...)
	srv.mux = http.NewServeMux()

	// Build each route's endpoint and register on the mux
	for _, r := range cfg.Routes {
		ep, err := r.builder.Build(cfg, srv)
		if err != nil {
			return nil, fmt.Errorf("pageserve: build route %q: %w", r.Path, err)
		}

		// Add common processors for all routes: session processor, so endpoints can
		// access session data if needed, and security headers.
		procs := []endpoint.Processor{securityProc, srv.SessionProc}
		if r.Access != "" {
			policy, ok := cfg.Access[r.Access]
			if !ok {
				return nil, fmt.Errorf("pageserve: route %q: access policy %q not defined", r.Path, r.Access)
			}
			proc, err := authnProcessor(policy, onFailure)
			if err != nil {
				return nil, fmt.Errorf("pageserve: route %q: authn processor: %w", r.Path, err)
			}
			// Append the authn processor after the session processor, since it needs session data to check authentication.
			procs = append(procs, proc)
		}

		// Handlers that serve sub-paths (files, pages) register with /prefix/{path...}
		// so that the path can be unmarshaled into the endpoint param.
		pattern := r.Path
		if sp, ok := r.builder.(subPathBuilder); ok {
			pattern = sp.muxPattern(r.Path)
		}

		srv.mux.Handle(pattern, wrapWithStats(r.Path, endpoint.HandleFunc(ep, procs...)))

		if r.Handler == "auth" {
			// Special case for the auth handler: also register a logout endpoint at
			// POST /prefix/logout. POST-only prevents CSRF logout via image tags or
			// navigations — a form submission or fetch with method POST is required.
			logoutPath := path.Join(routePathOnly(r.Path), "logout")
			srv.mux.Handle("POST "+logoutPath, wrapWithStats(logoutPath, endpoint.HandleFunc(logoutEndpoint, srv.SessionProc)))
		}
	}

	expvarFloat("pageserve.build_duration_ms").Set(
		float64(time.Since(buildStart).Milliseconds()),
	)

	return srv, nil
}

// subPathBuilder is implemented by handlers that serve sub-paths (files, pages).
// The mux pattern returned by muxPattern has {path...} appended so the mux
// populates r.PathValue("path") for each request.
type subPathBuilder interface {
	muxPattern(routePath string) string
}

// globalLatency is a server-wide latency histogram over rolling windows.
// It is intentionally an http.Handler wrapper rather than an endpoint.Processor
// so that latency includes response rendering and body writes — which can be
// significant for large files over slow links where TCP back-pressure applies.
//
// Each frame: "{total}{unit}{interval}{unit}" — 15m@1m, 1h@5m, 24h@1h.
var globalLatency = metric.NewHistogram("15m1m", "1h5m", "24h1h")

func init() {
	expvarMap("pageserve").Set("latency_ms", globalLatency)
}

// wrapWithStats wraps h with a per-route request counter and records latency
// in the server-wide histogram.
func wrapWithStats(name string, h http.Handler) http.Handler {
	// Each frame: "{total}{unit}{interval}{unit}" — 15m@1m, 1h@5m, 24h@1h.
	requests := metric.NewCounter("15m1m", "1h5m", "24h1h")
	expvarMap("pageserve.route."+sanitizeExpvarName(name)).Set("requests", requests)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		h.ServeHTTP(w, r)
		requests.Add(1)
		globalLatency.Add(float64(time.Since(start).Milliseconds()))
	})
}

// expvarFloat returns the named expvar.Float, creating it if not yet registered.
func expvarFloat(name string) *expvar.Float {
	if v := expvar.Get(name); v != nil {
		if f, ok := v.(*expvar.Float); ok {
			return f
		}
	}
	return expvar.NewFloat(name)
}

// expvarMap returns the named expvar.Map, creating it if not yet registered.
func expvarMap(name string) *expvar.Map {
	if v := expvar.Get(name); v != nil {
		if m, ok := v.(*expvar.Map); ok {
			return m
		}
	}
	return expvar.NewMap(name)
}

// sanitizeExpvarName replaces characters not suitable for expvar map keys.
func sanitizeExpvarName(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '/', ' ', '{', '}', '.':
			out = append(out, '_')
		default:
			out = append(out, c)
		}
	}
	return string(out)
}
