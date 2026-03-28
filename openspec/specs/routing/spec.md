## ADDED Requirements

### Requirement: Flat route list
Routes SHALL be declared as a flat list in config. Nested or tree-structured route definitions are not supported.

#### Scenario: Multiple routes
- **WHEN** the config has three route entries
- **THEN** all three are registered independently on the ServeMux

### Requirement: Route matching via http.ServeMux
All route matching SHALL be delegated to Go's `http.ServeMux`. No custom matching logic is implemented.

#### Scenario: Method-scoped route
- **WHEN** a route path is `GET /notes/`
- **THEN** only GET requests match; other methods fall through to the next pattern

#### Scenario: Specificity-based precedence
- **WHEN** two routes overlap (e.g. `/` and `/admin/`)
- **THEN** `http.ServeMux` specificity rules determine which route matches

### Requirement: No framework-imposed path conventions
The package SHALL NOT impose any path prefix convention. The base path for the auth route tree is declared by the user in config.

#### Scenario: Custom auth base path
- **WHEN** the user declares `path: /auth/` with `handler: auth`
- **THEN** the auth handler is registered at `/auth/` with no modification; the user may also use e.g. `/_/auth/`

### Requirement: Built-in handler types
The following handler types SHALL be supported: `pages`, `files`, `redirect`, `proxy`, `auth`, `defaultmux`.

#### Scenario: Unknown handler type
- **WHEN** a route declares `handler: unknown-type` and no custom handler is registered for that name
- **THEN** Validate returns an error

### Requirement: pages handler
The `pages` handler SHALL serve content via the `page` FS library. It SHALL support an optional `include_drafts:` boolean field (default false).

#### Scenario: Draft exclusion by default
- **WHEN** `include_drafts` is absent or false
- **THEN** draft content is not served

### Requirement: files handler
The `files` handler SHALL serve a directory tree via `http.FileServer`. It SHALL support `dir:`, `dirlist:` (default false), `index_html:` (default true), `dotfiles:` (default false), and `symlinks:` (default false).

#### Scenario: Dotfiles hidden by default
- **WHEN** `dotfiles` is false and a request targets a dotfile
- **THEN** the response is 404

#### Scenario: Directory listing disabled by default
- **WHEN** `dirlist` is false and a request targets a directory with no index.html
- **THEN** the response is 404, not a directory listing

### Requirement: redirect handler
The `redirect` handler SHALL issue an HTTP redirect to the `to:` URL. An optional `code:` field sets the status code (default 302).

#### Scenario: Default redirect code
- **WHEN** `code:` is absent
- **THEN** the response status is 302

### Requirement: proxy handler
The `proxy` handler SHALL forward requests to the `to:` destination URL. The route path prefix SHALL be stripped before forwarding. Client headers SHALL be forwarded unchanged.

#### Scenario: Prefix stripping
- **WHEN** a route is `path: /gh/` with `to: https://api.github.com` and a request arrives at `/gh/repos/foo`
- **THEN** the upstream request is sent to `https://api.github.com/repos/foo`

### Requirement: auth handler
The `auth` handler SHALL implement the full auth route tree (login redirect, OAuth callback, logout) at the declared base path, using the top-level `oauth:` and `session:` config. No credentials are declared on the route itself.

#### Scenario: Auth route tree from base path
- **WHEN** the auth route is at `/auth/`
- **THEN** the handler manages all sub-paths (e.g. `/auth/login`, `/auth/callback`, `/auth/logout`) internally

### Requirement: defaultmux handler
The `defaultmux` handler SHALL expose Go's default mux, including `/debug/vars` and `/debug/pprof`, at the declared path prefix.

#### Scenario: Debug routes available
- **WHEN** `handler: defaultmux` is declared at `/debug/`
- **THEN** `/debug/vars` and `/debug/pprof` are accessible

### Requirement: Custom handler registration
Custom handler types SHALL be registerable via the Go API at Build time. The route's handler-specific config SHALL be passed as a raw YAML node for the factory to unmarshal.

#### Scenario: Custom handler used in config
- **WHEN** a custom factory is registered for `"mytype"` and a route declares `handler: mytype`
- **THEN** the factory is called with the route's raw YAML config during Build
