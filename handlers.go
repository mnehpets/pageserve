package pageserve

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	mnfs "github.com/mnehpets/fs"
	"github.com/mnehpets/http/endpoint"
	"github.com/mnehpets/page"
	"gopkg.in/yaml.v3"
)

// Endpoint is the common return type for HandlerBuilder.Build.
// build.go is the sole site that wraps an Endpoint with processors
// and registers the resulting http.Handler on the mux.
//
// Ideally we would use a generic Endpoint type like endpoint.EndpointFunc[T any],
// however, the common HandlerFactory map would need to have dynamic types
// so that the call to endpoint.HandleFunc() in Build() gets the correct param type.
// For now, the only handlers that use params are pages and files, and they
// both use endpoint.FileSystemParams,
type Params = endpoint.FileSystemParams
type Endpoint = endpoint.EndpointFunc[Params]

// HandlerBuilder is returned by a HandlerFactory. It holds the decoded config
// for a single route and knows how to validate and build its Endpoint.
type HandlerBuilder interface {
	Validate(cfg Config) error
	Build(cfg Config, srv *Server) (Endpoint, error)
}

// HandlerFactory parses a route's YAML node into a HandlerBuilder.
// Both built-in and custom handlers use this type.
type HandlerFactory func(node *yaml.Node) (HandlerBuilder, error)

// --- pages ---

type pagesBuilder struct {
	Path          string `yaml:"path"`
	Dir           string `yaml:"dir"`
	IncludeDrafts bool   `yaml:"include_drafts"`
}

func (b *pagesBuilder) Validate(cfg Config) error          { return nil }
func (b *pagesBuilder) muxPattern(routePath string) string { return withSubPath(routePath) }

func (b *pagesBuilder) Build(cfg Config, srv *Server) (Endpoint, error) {
	dir := b.Dir
	if dir == "" {
		dir = "."
	}
	fsys := os.DirFS(dir)

	opts := []page.SiteOption{
		page.WithConfig(page.SiteConfig{
			BaseURL: cfg.Site.BaseURL,
			Name:    cfg.Site.Name,
			Lang:    cfg.Site.Lang,
		}),
	}
	if b.IncludeDrafts {
		opts = append(opts, page.WithIncludeDrafts())
	}

	site, err := page.NewSite(fsys, opts...)
	if err != nil {
		return nil, fmt.Errorf("pages (path=%q): %w", b.Path, err)
	}

	noLayouts, err := mnfs.WithGlob("_layouts/*", mnfs.Disallowed)
	if err != nil {
		return nil, fmt.Errorf("pages (path=%q): _layouts filter: %w", b.Path, err)
	}
	public := mnfs.NewFilterFS(fsys, noLayouts)

	return (&endpoint.FileSystem{
		FS: func(_ context.Context, _ *http.Request) (fs.FS, error) {
			return public, nil
		},
		IndexHTML:    true,
		FileRenderer: site.FileRenderer(),
		DirRenderer:  site.DirRenderer(),
	}).Endpoint, nil
}

func pagesHandlerFactory() HandlerFactory {
	return func(node *yaml.Node) (HandlerBuilder, error) {
		b := new(pagesBuilder)
		if node != nil {
			if err := node.Decode(b); err != nil {
				return nil, fmt.Errorf("pages: decode config: %w", err)
			}
		}
		return b, nil
	}
}

// --- files ---

type filesBuilder struct {
	Path      string `yaml:"path"`
	Dir       string `yaml:"dir"`
	IndexHTML *bool  `yaml:"index_html"`
	DirList   bool   `yaml:"dir_list"`
	Dotfiles  bool   `yaml:"dotfiles"`
	Symlinks  bool   `yaml:"symlinks"`
}

func (b *filesBuilder) Validate(cfg Config) error {
	if b.Dir == "" {
		return fmt.Errorf("files (path=%q): dir is required", b.Path)
	}
	return nil
}
func (b *filesBuilder) muxPattern(routePath string) string { return withSubPath(routePath) }

func (b *filesBuilder) Build(cfg Config, srv *Server) (Endpoint, error) {
	indexHTML := true
	if b.IndexHTML != nil {
		indexHTML = *b.IndexHTML
	}

	fsys := &filteredFS{
		base:     b.Dir,
		dotfiles: b.Dotfiles,
		symlinks: b.Symlinks,
	}

	return (&endpoint.FileSystem{
		FS: func(_ context.Context, _ *http.Request) (fs.FS, error) {
			return fsys, nil
		},
		IndexHTML:        indexHTML,
		DirectoryListing: b.DirList,
		DirTemplate:      endpoint.FancyDirTemplate,
	}).Endpoint, nil
}

func filesHandlerFactory() HandlerFactory {
	return func(node *yaml.Node) (HandlerBuilder, error) {
		b := new(filesBuilder)
		if node != nil {
			if err := node.Decode(b); err != nil {
				return nil, fmt.Errorf("files: decode config: %w", err)
			}
		}
		return b, nil
	}
}

// --- redirect ---

type redirectBuilder struct {
	Path string `yaml:"path"`
	To   string `yaml:"to"`
	Code int    `yaml:"code"`
	// PreservePath controls whether the sub-path is appended to To.
	// Only meaningful when Path ends with "/" (a tree route).
	// Default true: /old/foo/bar → /new/foo/bar.
	// Set false: all requests under /old/ redirect to the same fixed To target.
	// Has no effect on exact-match routes (no trailing slash, or ending with {$}).
	PreservePath *bool `yaml:"preserve_path"`
}

func (b *redirectBuilder) muxPattern(routePath string) string {
	p := routePathOnly(routePath)
	// {$} anchors an exact match (e.g. "/{$}" matches only "/"), so treat it
	// like a non-slash route even though the path before {$} ends with "/".
	if strings.HasSuffix(p, "/") && !strings.HasSuffix(p, "{$}") {
		return withSubPath(routePath)
	}
	return routePath
}

func (b *redirectBuilder) Validate(cfg Config) error {
	if b.To == "" {
		return fmt.Errorf("redirect (path=%q): to is required", b.Path)
	}
	return nil
}

func (b *redirectBuilder) Build(cfg Config, srv *Server) (Endpoint, error) {
	code := b.Code
	if code == 0 {
		code = http.StatusFound
	}
	preservePath := b.PreservePath == nil || *b.PreservePath
	toBase := strings.TrimSuffix(b.To, "/")
	return func(w http.ResponseWriter, r *http.Request, params Params) (endpoint.Renderer, error) {
		to := b.To
		if preservePath && params.Path != "" {
			to = toBase + "/" + params.Path
		}
		return &endpoint.RedirectRenderer{URL: to, Status: code}, nil
	}, nil
}

func redirectHandlerFactory() HandlerFactory {
	return func(node *yaml.Node) (HandlerBuilder, error) {
		b := new(redirectBuilder)
		if node != nil {
			if err := node.Decode(b); err != nil {
				return nil, fmt.Errorf("redirect: decode config: %w", err)
			}
		}
		return b, nil
	}
}

// --- proxy ---

type proxyBuilder struct {
	Path string `yaml:"path"`
	To   string `yaml:"to"`
}

func (b *proxyBuilder) Validate(cfg Config) error {
	if b.To == "" {
		return fmt.Errorf("proxy (path=%q): to is required", b.Path)
	}
	if u, err := url.Parse(b.To); err != nil || !u.IsAbs() {
		return fmt.Errorf("proxy (path=%q): to must be an absolute URL", b.Path)
	}
	return nil
}

func (b *proxyBuilder) muxPattern(routePath string) string { return withSubPath(routePath) }

func (b *proxyBuilder) Build(cfg Config, srv *Server) (Endpoint, error) {
	target, _ := url.Parse(b.To) // already validated

	return func(w http.ResponseWriter, r *http.Request, params Params) (endpoint.Renderer, error) {
		proxy := &httputil.ReverseProxy{
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.Out.URL.Scheme = target.Scheme
				pr.Out.URL.Host = target.Host
				pr.Out.URL.Path = "/" + params.Path
				pr.Out.URL.RawPath = ""
				pr.Out.Host = target.Host
			},
		}
		return &endpoint.ProxyRenderer{Proxy: proxy}, nil
	}, nil
}

func proxyHandlerFactory() HandlerFactory {
	return func(node *yaml.Node) (HandlerBuilder, error) {
		b := new(proxyBuilder)
		if node != nil {
			if err := node.Decode(b); err != nil {
				return nil, fmt.Errorf("proxy: decode config: %w", err)
			}
		}
		return b, nil
	}
}

// --- defaultmux ---

type defaultMuxBuilder struct{}

func (b *defaultMuxBuilder) Validate(cfg Config) error { return nil }
func (b *defaultMuxBuilder) Build(cfg Config, srv *Server) (Endpoint, error) {
	return opaqueHandlerEndpoint(http.DefaultServeMux), nil
}

func defaultMuxHandlerFactory() HandlerFactory {
	return func(*yaml.Node) (HandlerBuilder, error) {
		return new(defaultMuxBuilder), nil
	}
}

// --- helpers ---

// opaqueHandlerEndpoint wraps an http.Handler as an Endpoint for handlers that
// own their full response lifecycle (e.g. srv.AuthHandler, http.DefaultServeMux).
// Do NOT use this for handlers that should participate in the endpoint renderer
// or processor contract — they must be written as endpoints from the start.
func opaqueHandlerEndpoint(h http.Handler) Endpoint {
	return func(w http.ResponseWriter, r *http.Request, _ Params) (endpoint.Renderer, error) {
		return endpoint.RendererFunc(func(w http.ResponseWriter, r *http.Request) error {
			h.ServeHTTP(w, r)
			return nil
		}), nil
	}
}

// withSubPath appends /{path...} to a mux pattern, handling optional method prefixes.
// "/notes/" → "/notes/{path...}", "GET /notes/" → "GET /notes/{path...}"
func withSubPath(pattern string) string {
	if i := strings.Index(pattern, " "); i != -1 {
		return pattern[:i+1] + strings.TrimSuffix(pattern[i+1:], "/") + "/{path...}"
	}
	return strings.TrimSuffix(pattern, "/") + "/{path...}"
}

// filteredFS wraps an OS directory, optionally blocking dotfiles and symlinks.
type filteredFS struct {
	base     string
	dotfiles bool // if false, paths with dotfile components return ErrNotExist
	symlinks bool // if false, symlinks return ErrNotExist
}

func (f *filteredFS) Open(name string) (fs.File, error) {
	if !f.dotfiles {
		for _, part := range strings.Split(name, "/") {
			if part != "." && part != ".." && strings.HasPrefix(part, ".") {
				return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
			}
		}
	}
	if !f.symlinks {
		osPath := filepath.Join(f.base, filepath.FromSlash(name))
		info, err := os.Lstat(osPath)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
		}
	}
	return os.DirFS(f.base).Open(name)
}

// routePathOnly returns the path part of a ServeMux pattern, stripping any
// leading method prefix (e.g. "GET /notes/" → "/notes/").
func routePathOnly(pattern string) string {
	if i := strings.Index(pattern, " "); i != -1 {
		return pattern[i+1:]
	}
	return pattern
}
