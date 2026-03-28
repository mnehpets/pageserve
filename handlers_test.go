package pageserve

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mnehpets/http/endpoint"
	"gopkg.in/yaml.v3"
)

// routeNode builds a *yaml.Node from a map for use in factory calls.
func routeNode(t *testing.T, kvs map[string]any) *yaml.Node {
	t.Helper()
	b, err := yaml.Marshal(kvs)
	if err != nil {
		t.Fatalf("routeNode marshal: %v", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("routeNode unmarshal: %v", err)
	}
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0]
	}
	return &doc
}

// buildHandler is a test helper that parses, validates, and builds a handler
// from a factory and node, failing the test on any error.
func buildHandler(t *testing.T, factory HandlerFactory, node *yaml.Node) http.Handler {
	t.Helper()
	bldr, err := factory(node)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if err := bldr.Validate(Config{}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	ep, err := bldr.Build(Config{}, &Server{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return endpoint.HandleFunc(ep)
}

// --- routePathOnly ---

func TestRoutePathOnly(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"/notes/", "/notes/"},
		{"GET /notes/", "/notes/"},
		{"POST /api/submit", "/api/submit"},
		{"/", "/"},
	}
	for _, tc := range cases {
		got := routePathOnly(tc.in)
		if got != tc.want {
			t.Errorf("routePathOnly(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// --- redirectHandlerFactory ---

func TestRedirectHandlerFactory_DefaultCode(t *testing.T) {
	h := buildHandler(t, redirectHandlerFactory(), routeNode(t, map[string]any{"path": "/old", "to": "/new"}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/old", nil))

	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusFound)
	}
	if got := w.Header().Get("Location"); got != "/new" {
		t.Errorf("Location = %q, want %q", got, "/new")
	}
}

func TestRedirectHandlerFactory_ExplicitCode(t *testing.T) {
	h := buildHandler(t, redirectHandlerFactory(), routeNode(t, map[string]any{"path": "/old", "to": "/new", "code": 301}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/old", nil))

	if w.Code != http.StatusMovedPermanently {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMovedPermanently)
	}
}

func buildHandlerOnMux(t *testing.T, factory HandlerFactory, node *yaml.Node, routePath string) http.Handler {
	t.Helper()
	bldr, err := factory(node)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	ep, err := bldr.Build(Config{}, &Server{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	pattern := routePath
	if sp, ok := bldr.(subPathBuilder); ok {
		pattern = sp.muxPattern(routePath)
	}
	mux := http.NewServeMux()
	mux.Handle(pattern, endpoint.HandleFunc(ep))
	return mux
}

func TestRedirectHandlerFactory_PreservePathDefault(t *testing.T) {
	// Default: preserve_path is true — sub-path is appended to to.
	h := buildHandlerOnMux(t, redirectHandlerFactory(), routeNode(t, map[string]any{"path": "/old/", "to": "/new/"}), "/old/")

	cases := []struct {
		request string
		wantLoc string
	}{
		{"/old/", "/new/"},
		{"/old/foo/bar", "/new/foo/bar"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", tc.request, nil))
		if got := w.Header().Get("Location"); got != tc.wantLoc {
			t.Errorf("request %q: Location = %q, want %q", tc.request, got, tc.wantLoc)
		}
	}
}

func TestRedirectHandlerFactory_PreservePathFalse(t *testing.T) {
	// preserve_path: false — all sub-paths redirect to the same fixed target.
	h := buildHandlerOnMux(t, redirectHandlerFactory(), routeNode(t, map[string]any{"path": "/old/", "to": "/new/", "preserve_path": false}), "/old/")

	cases := []string{"/old/", "/old/foo/bar"}
	for _, req := range cases {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", req, nil))
		if got := w.Header().Get("Location"); got != "/new/" {
			t.Errorf("request %q: Location = %q, want /new/", req, got)
		}
	}
}

func TestRedirectHandlerFactory_MissingTo(t *testing.T) {
	bldr, err := redirectHandlerFactory()(routeNode(t, map[string]any{"path": "/old"}))
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if err := bldr.Validate(Config{}); err == nil {
		t.Fatal("expected Validate error when to is empty")
	}
}

// TestRedirectHandler_PathTraversalCleanedByStdlib verifies that net/http's
// ServeMux cleans ".." components from request paths before populating the
// {path...} wildcard, so params.Path never carries traversal sequences into
// the redirect target. If this test fails it likely means the stdlib changed
// its path-cleaning behaviour and explicit sanitisation is now required.
func TestRedirectHandler_PathTraversalCleanedByStdlib(t *testing.T) {
	h := buildHandlerOnMux(t, redirectHandlerFactory(),
		routeNode(t, map[string]any{"path": "/old/", "to": "/new/"}), "/old/")

	cases := []struct {
		request string
	}{
		// These resolve outside /old/ after cleaning — mux redirects before handler runs.
		{"/old/../../etc/passwd"},
		{"/old/../secret"},
		// This resolves inside /old/ — handler runs with cleaned params.Path.
		{"/old/foo/../bar"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", tc.request, nil))
		if loc := w.Header().Get("Location"); strings.Contains(loc, "..") {
			t.Errorf("request %q: Location %q contains path traversal sequences", tc.request, loc)
		}
	}
}

// --- proxyHandlerFactory ---

func TestProxyHandlerFactory_PrefixStripping(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.URL.RequestURI())
	}))
	defer upstream.Close()

	bldr, err := proxyHandlerFactory()(routeNode(t, map[string]any{"path": "/gh/", "to": upstream.URL}))
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	ep, err := bldr.Build(Config{}, &Server{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/gh/{path...}", endpoint.HandleFunc(ep))

	cases := []struct {
		request string
		want    string
	}{
		{"/gh/repos/foo", "/repos/foo"},
		{"/gh/repos/foo?page=2", "/repos/foo?page=2"},
		{"/gh/", "/"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", tc.request, nil))
		if body := w.Body.String(); body != tc.want {
			t.Errorf("request %q: upstream received %q, want %q", tc.request, body, tc.want)
		}
	}
}

func TestProxyHandlerFactory_MissingTo(t *testing.T) {
	bldr, err := proxyHandlerFactory()(routeNode(t, map[string]any{"path": "/api/"}))
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if err := bldr.Validate(Config{}); err == nil {
		t.Fatal("expected Validate error when to is empty")
	}
}

func TestProxyHandlerFactory_RelativeURL(t *testing.T) {
	bldr, err := proxyHandlerFactory()(routeNode(t, map[string]any{"path": "/api/", "to": "/relative"}))
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if err := bldr.Validate(Config{}); err == nil {
		t.Fatal("expected Validate error for non-absolute to URL")
	}
}

// TestProxyHandler_PathTraversalCleanedByStdlib verifies that net/http's
// ServeMux cleans ".." components from request paths before populating the
// {path...} wildcard, so params.Path never carries traversal sequences into
// the upstream URL. If this test fails it likely means the stdlib changed
// its path-cleaning behaviour and explicit sanitisation is now required.
func TestProxyHandler_PathTraversalCleanedByStdlib(t *testing.T) {
	var capturedPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
	}))
	defer upstream.Close()

	bldr, err := proxyHandlerFactory()(routeNode(t, map[string]any{"path": "/api/", "to": upstream.URL}))
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	ep, err := bldr.Build(Config{}, &Server{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/{path...}", endpoint.HandleFunc(ep))

	cases := []struct {
		request string
	}{
		// These resolve outside /api/ after cleaning — mux redirects before handler runs.
		{"/api/../../etc/passwd"},
		{"/api/../secret"},
		// This resolves inside /api/ — handler runs with cleaned params.Path.
		{"/api/foo/../bar"},
	}
	for _, tc := range cases {
		capturedPath = ""
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", tc.request, nil))
		if strings.Contains(capturedPath, "..") {
			t.Errorf("request %q: upstream received path %q containing traversal sequences", tc.request, capturedPath)
		}
	}
}

// --- filesHandlerFactory + filteredFS ---

func TestFilteredFS_DotfilesBlocked(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, ".secret"), []byte("hidden"), 0600)
	_ = os.WriteFile(filepath.Join(dir, "visible.txt"), []byte("visible"), 0600)

	fsys := &filteredFS{base: dir, dotfiles: false, symlinks: false}

	_, err := fsys.Open(".secret")
	if err == nil {
		t.Error("expected error opening dotfile when dotfiles=false")
	}

	f, err := fsys.Open("visible.txt")
	if err != nil {
		t.Errorf("unexpected error opening regular file: %v", err)
	} else {
		_ = f.Close()
	}
}

func TestFilteredFS_DotfilesAllowed(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, ".config"), []byte("cfg"), 0600)

	fsys := &filteredFS{base: dir, dotfiles: true, symlinks: false}

	f, err := fsys.Open(".config")
	if err != nil {
		t.Errorf("unexpected error opening dotfile when dotfiles=true: %v", err)
	} else {
		_ = f.Close()
	}
}

func TestFilesHandlerFactory_MissingDir(t *testing.T) {
	bldr, err := filesHandlerFactory()(routeNode(t, map[string]any{"path": "/assets/"}))
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if err := bldr.Validate(Config{}); err == nil {
		t.Fatal("expected Validate error when dir is empty")
	}
}

func TestFilesHandlerFactory_IndexHTMLDefault(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hi</h1>"), 0600)

	h := buildHandler(t, filesHandlerFactory(), routeNode(t, map[string]any{
		"path": "/assets/",
		"dir":  dir,
		// index_html omitted → defaults to true
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/assets/", nil))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (index.html should be served)", w.Code, http.StatusOK)
	}
}

// --- defaultMuxHandlerFactory ---

func TestDefaultMuxHandlerFactory_ServesDefaultMux(t *testing.T) {
	const sentinel = "/defaultmux-test-sentinel"
	http.HandleFunc(sentinel, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	h := buildHandler(t, defaultMuxHandlerFactory(), nil)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", sentinel, nil))
	if w.Code != http.StatusTeapot {
		t.Errorf("status = %d, want %d", w.Code, http.StatusTeapot)
	}
}
