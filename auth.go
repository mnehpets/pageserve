package pageserve

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path"

	httpauth "github.com/mnehpets/http/auth"
	"github.com/mnehpets/http/endpoint"
	"github.com/mnehpets/http/middleware"
	"gopkg.in/yaml.v3"
)

// --- authBuilder ---

type authBuilder struct{}

func (b *authBuilder) Validate(cfg Config) error {
	if len(cfg.OAuth.Providers) == 0 {
		return fmt.Errorf("auth handler: no oauth providers configured")
	}
	return nil
}

func (b *authBuilder) Build(cfg Config, srv *Server) (Endpoint, error) {
	if srv.AuthHandler == nil {
		return nil, fmt.Errorf("auth handler not initialised in server")
	}
	return opaqueHandlerEndpoint(srv.AuthHandler), nil
}

func authHandlerFactory() HandlerFactory {
	return func(*yaml.Node) (HandlerBuilder, error) {
		return new(authBuilder), nil
	}
}

// initAuth populates the auth-related fields of srv: the session processor
// and, if an auth route is declared, the OAuth handler.
// It must be called before any route handler is built.
func initAuth(cfg Config, routes []RouteConfig, srv *Server) error {
	sessProc, err := newSessionProcessor(cfg)
	if err != nil {
		return err
	}
	srv.SessionProc = sessProc

	for _, r := range routes {
		if r.Handler != "auth" {
			continue
		}
		ah, err := newOAuthHandler(cfg, r, sessProc)
		if err != nil {
			return fmt.Errorf("pageserve: build route %q: %w", r.Path, err)
		}
		srv.AuthHandler = ah
		break
	}

	return nil
}

// newSessionProcessor constructs the session processor shared across all
// request paths that need session access.
func newSessionProcessor(cfg Config) (endpoint.Processor, error) {
	if len(cfg.Session.Keys) == 0 {
		return nil, fmt.Errorf("pageserve: session: at least one key is required")
	}

	keyID := cfg.Session.Keys[0].ID
	keys := make(map[string][]byte, len(cfg.Session.Keys))
	for _, k := range cfg.Session.Keys {
		keys[k.ID] = []byte(k.Value)
	}

	opts := []middleware.SessionProcessorOption{}
	if cfg.Session.CookieName != "" {
		opts = append(opts, middleware.WithCookieName(cfg.Session.CookieName))
	}
	sp, err := middleware.NewSessionProcessor(keyID, keys, opts...)
	if err != nil {
		return nil, fmt.Errorf("pageserve: new session processor: %w", err)
	}
	return sp, nil
}

// emailMatchFn compiles the allow list for a named access policy into a match function.
func emailMatchFn(policy AccessPolicy) (func(string) bool, error) {
	fn, err := httpauth.MatchEmailGlob(policy.Allow)
	if err != nil {
		return nil, fmt.Errorf("pageserve: email match fn: %w", err)
	}
	return fn, nil
}

// authnProcessor returns the email-policy processor for a protected route.
// The caller is responsible for prepending the session processor.
func authnProcessor(
	policy AccessPolicy,
	onFailure func(http.ResponseWriter, *http.Request) (endpoint.Renderer, error),
) (endpoint.Processor, error) {
	matchFn, err := emailMatchFn(policy)
	if err != nil {
		return nil, err
	}
	proc, err := httpauth.NewRequireUsernameMatchProcessor(matchFn, onFailure)
	if err != nil {
		return nil, fmt.Errorf("pageserve: authn processor: %w", err)
	}
	return proc, nil
}

// loginRedirector returns the onFailure function used by session-check processors.
// When ah is non-nil, unauthenticated requests are redirected to OAuth login
// with the original URL preserved. When ah is nil, a 401 is returned instead.
func loginRedirector(
	ah *httpauth.AuthHandler,
	authPath string,
	providerID string,
) func(http.ResponseWriter, *http.Request) (endpoint.Renderer, error) {
	if ah == nil {
		return func(w http.ResponseWriter, r *http.Request) (endpoint.Renderer, error) {
			return nil, endpoint.Error(401, "authentication required", nil)
		}
	}
	return func(w http.ResponseWriter, r *http.Request) (endpoint.Renderer, error) {
		// Save the current URL in the auth state, as the post-login redirect URL.
		nextURL := httpauth.ValidateNextURLIsLocal(r.URL.RequestURI())
		stateKey, err := ah.PrepareAuth(w, r, httpauth.AuthParams{NextURL: nextURL})
		if err != nil {
			return nil, endpoint.Error(http.StatusInternalServerError, "failed to prepare auth", err)
		}
		loginURL := path.Join(authPath, "login", providerID) + "?state_key=" + url.QueryEscape(stateKey)
		return &endpoint.RedirectRenderer{URL: loginURL, Status: http.StatusFound}, nil
	}
}

// newOAuthHandler constructs the auth.AuthHandler for the declared auth route.
func newOAuthHandler(
	cfg Config,
	authRoute RouteConfig,
	sessProc endpoint.Processor,
) (*httpauth.AuthHandler, error) {
	if len(cfg.OAuth.Providers) == 0 {
		return nil, fmt.Errorf("pageserve: auth handler: no oauth providers configured")
	}
	provider := cfg.OAuth.Providers[0]
	basePath := routePathOnly(authRoute.Path)

	matchFns := make(map[string]func(string) bool, len(cfg.Access))
	for name, policy := range cfg.Access {
		fn, err := emailMatchFn(policy)
		if err != nil {
			return nil, fmt.Errorf("pageserve: auth policy %q: %w", name, err)
		}
		matchFns[name] = fn
	}

	resultEndpoint := httpauth.ResultEndpoint(func(w http.ResponseWriter, r *http.Request, result *httpauth.AuthResult) (endpoint.Renderer, error) {
		if result.Error != nil {
			return nil, result.Error
		}

		email, ok := httpauth.GetVerifiedEmail(result.IDToken)
		if !ok {
			return nil, endpoint.Error(http.StatusBadRequest, "email not verified", nil)
		}

		allowed := false
		for _, matchFn := range matchFns {
			if matchFn(email) {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, endpoint.Error(http.StatusForbidden, "access denied: no matching policy", nil)
		}

		sess, ok := middleware.SessionFromContext(r.Context())
		if !ok {
			return nil, endpoint.Error(http.StatusInternalServerError, "session not available", nil)
		}
		if err := sess.Login(email); err != nil {
			return nil, endpoint.Error(http.StatusInternalServerError, "failed to create session", err)
		}

		nextURL := "/"
		if result.AuthParams != nil && result.AuthParams.NextURL != "" {
			nextURL = result.AuthParams.NextURL
		}
		return &endpoint.RedirectRenderer{URL: nextURL, Status: http.StatusFound}, nil
	})

	// First key is the active key. Other keys are a valid for existing session cookies,
	// but won't be used for encoding cookies.
	keyID := cfg.Session.Keys[0].ID
	keys := make(map[string][]byte, len(cfg.Session.Keys))
	for _, k := range cfg.Session.Keys {
		keys[k.ID] = []byte(k.Value)
	}

	reg, err := oidcRegistry(provider)
	if err != nil {
		return nil, err
	}

	h, err := httpauth.NewHandler(
		reg,
		httpauth.DefaultCookieName,
		keyID,
		keys,
		cfg.Site.BaseURL,
		basePath,
		httpauth.WithProcessors(sessProc),
		httpauth.WithResultEndpoint(resultEndpoint),
	)
	if err != nil {
		return nil, fmt.Errorf("pageserve: new oauth handler: %w", err)
	}
	return h, nil
}

// logoutEndpoint clears the session and redirects to "/".
func logoutEndpoint(w http.ResponseWriter, r *http.Request, _ struct{}) (endpoint.Renderer, error) {
	if sess, ok := middleware.SessionFromContext(r.Context()); ok {
		if err := sess.Logout(); err != nil {
			log.Printf("pageserve: logout: %v", err)
		}
	}
	return &endpoint.RedirectRenderer{URL: "/", Status: http.StatusFound}, nil
}

// oidcRegistry creates a Registry and registers the given OAuthProvider.
// Currently only "google" is supported. OIDC discovery is performed eagerly (network call).
func oidcRegistry(p OAuthProvider) (*httpauth.Registry, error) {
	reg := httpauth.NewRegistry()
	if err := reg.RegisterOIDCProvider(
		context.Background(),
		"google",
		"https://accounts.google.com",
		p.ClientID,
		p.ClientSecret.Value,
		[]string{"openid", "email"},
		"",
	); err != nil {
		return nil, fmt.Errorf("pageserve: register google OIDC provider: %w", err)
	}
	return reg, nil
}
