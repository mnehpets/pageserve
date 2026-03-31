package pageserve

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mnehpets/http/endpoint"
	"gopkg.in/yaml.v3"
)

// buildableConfig returns a Config that Build can assemble without any network
// calls (no auth/oauth routes, so OIDC discovery is never triggered).
func buildableConfig() Config {
	return Config{
		Server: ServerConfig{Listeners: []ListenerConfig{{Address: ":8080"}}},
		Session: SessionConfig{
			CookieName: "sess",
			Keys:       []SessionKey{{ID: "k1", Env: "KEY1", Value: "aaaabbbbccccddddeeeeffffgggghhhh"}}, // 32 bytes
		},
	}
}

// rawNode builds a *yaml.Node from a map for use in RouteConfig.Raw.
func rawNode(t *testing.T, kvs map[string]any) *yaml.Node {
	t.Helper()
	b, err := yaml.Marshal(kvs)
	if err != nil {
		t.Fatalf("rawNode marshal: %v", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("rawNode unmarshal: %v", err)
	}
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0]
	}
	return &doc
}

func TestBuild_EmptyRoutes(t *testing.T) {
	cfg := buildableConfig()
	srv, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if srv == nil {
		t.Fatal("Build returned nil server")
	}
}

func TestBuild_ServerImplementsHandler(t *testing.T) {
	cfg := buildableConfig()
	srv, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var _ http.Handler = srv // compile-time check
}

func TestBuild_RedirectRoute(t *testing.T) {
	cfg := buildableConfig()
	cfg.Routes = []RouteConfig{
		{
			Path:    "/old",
			Handler: "redirect",
			Raw:     rawNode(t, map[string]any{"path": "/old", "to": "/new", "code": 301}),
		},
	}

	srv, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/old", nil)
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusMovedPermanently {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMovedPermanently)
	}
	if got := w.Header().Get("Location"); got != "/new" {
		t.Errorf("Location = %q, want /new", got)
	}
}

func TestBuild_ProxyRoute(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.URL.Path)
	}))
	defer upstream.Close()

	cfg := buildableConfig()
	cfg.Routes = []RouteConfig{
		{
			Path:    "/api/",
			Handler: "proxy",
			Raw:     rawNode(t, map[string]any{"path": "/api/", "to": upstream.URL}),
		},
	}

	srv, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/users", nil)
	srv.ServeHTTP(w, r)

	if body := w.Body.String(); body != "/users" {
		t.Errorf("upstream received path %q, want /users", body)
	}
}

func TestBuild_InvalidHandlerType(t *testing.T) {
	cfg := buildableConfig()
	cfg.Routes = []RouteConfig{
		{Path: "/", Handler: "unknown-type"},
	}
	_, err := Build(cfg)
	if err == nil {
		t.Fatal("expected error for unknown handler type")
	}
}

func TestBuild_CustomHandler(t *testing.T) {
	cfg := buildableConfig()
	cfg.Routes = []RouteConfig{
		{Path: "/custom", Handler: "mytype"},
	}

	factory := func(*yaml.Node) (HandlerBuilder, error) {
		return &simpleBuilder{ep: func(w http.ResponseWriter, r *http.Request, _ Params) (endpoint.Renderer, error) {
			w.WriteHeader(http.StatusTeapot)
			return nil, nil
		}}, nil
	}

	srv, err := Build(cfg, WithHandler("mytype", factory))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/custom", nil)
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusTeapot {
		t.Errorf("status = %d, want %d", w.Code, http.StatusTeapot)
	}
}

func TestBuild_MultipleRoutes(t *testing.T) {
	cfg := buildableConfig()
	cfg.Routes = []RouteConfig{
		{Path: "/a", Handler: "redirect", Raw: rawNode(t, map[string]any{"path": "/a", "to": "/a-dest"})},
		{Path: "/b", Handler: "redirect", Raw: rawNode(t, map[string]any{"path": "/b", "to": "/b-dest"})},
	}

	srv, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	for _, tc := range []struct{ path, want string }{
		{"/a", "/a-dest"},
		{"/b", "/b-dest"},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", tc.path, nil)
		srv.ServeHTTP(w, r)
		if got := w.Header().Get("Location"); got != tc.want {
			t.Errorf("GET %s → Location %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestBuild_FailsOnHandlerValidateError(t *testing.T) {
	// proxy with non-absolute URL should fail at validate phase.
	cfg := buildableConfig()
	cfg.Routes = []RouteConfig{
		{
			Path:    "/api/",
			Handler: "proxy",
			Raw:     rawNode(t, map[string]any{"path": "/api/", "to": "not-absolute"}),
		},
	}
	_, err := Build(cfg)
	if err == nil {
		t.Fatal("expected Build to fail when proxy to is not absolute")
	}
}

func TestBuild_ValidationRunsFirst(t *testing.T) {
	cfg := buildableConfig()
	cfg.Server.Listeners = nil // invalid
	_, err := Build(cfg)
	if err == nil {
		t.Fatal("expected Build to fail validation")
	}
}

// simpleBuilder is a minimal HandlerBuilder for use in tests.
type simpleBuilder struct {
	ep Endpoint
}

func (b *simpleBuilder) Validate(cfg Config) error                       { return nil }
func (b *simpleBuilder) Build(cfg Config, srv *Server) (Endpoint, error) { return b.ep, nil }

// TestLogoutEndpoint_RequiresPOST verifies that the logout endpoint only accepts
// POST requests, mirroring the "POST "+logoutPath registration in Build.
// GET must return 405 to prevent CSRF logout via image tags or navigations.
func TestLogoutEndpoint_RequiresPOST(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("POST /auth/logout", endpoint.HandleFunc(logoutEndpoint))

	cases := []struct {
		method string
		want   int
	}{
		{"GET", http.StatusMethodNotAllowed},
		{"POST", http.StatusFound},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(tc.method, "/auth/logout", nil))
		if w.Code != tc.want {
			t.Errorf("%s /auth/logout: status = %d, want %d", tc.method, w.Code, tc.want)
		}
	}
}

// --- sanitizeExpvarName ---

func TestSanitizeExpvarName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"/notes/", "_notes_"},
		{"GET /api/", "GET__api_"},
		{"/debug/{path...}", "_debug__path____"},
	}
	for _, tc := range cases {
		got := sanitizeExpvarName(tc.in)
		if got != tc.want {
			t.Errorf("sanitizeExpvarName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
