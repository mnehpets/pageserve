## Context

`pageserve` is a new Go package that assembles a complete HTTP server from a YAML config file. It sits above the `page` content rendering library, which handles content FS rendering but has no routing, auth, or session concerns.

The immediate predecessor (`oneserve`) had several problems that inform this design:
- Async/lazy handler initialisation caused permanent goroutine leaks on error
- Secrets were stored inline in the config JSON, making it unsafe to commit
- Recursive handler tree made routing logic hard to follow

## Goals / Non-Goals

**Goals:**
- Define the full config schema driven by what routing, auth, and server assembly actually need
- Strict Parse → Validate → Build phase separation; no net/http in Parse or Validate
- Secrets kept out of `config.yaml` via `secrets.env`; config safe to commit
- Route matching delegated to `http.ServeMux`; no custom matching logic
- Named auth policies as the single mechanism for both session gating and per-route authz
- Custom handler extension mechanism expressive enough to implement third-party OAuth proxies (e.g. DecapCMS GitHub backend) without modifying the core package

**Non-Goals:**
- File upload handler
- Multiple simultaneous auth providers — the config schema supports a list of providers for future extensibility, but routing users to different providers based on context is deferred; only Google is currently implemented
- Role storage in session (email only; roles re-evaluated from config per request)
- Variable expansion in config YAML
- Built-in DecapCMS support — but the custom handler extension mechanism (D10) must be sufficient to implement a DecapCMS GitHub OAuth proxy without changes to the core package
- Popup-based OAuth (requires frontend coupling; out of scope for the server package)
- Configurable unauthenticated action — redirect to OAuth is the only supported behaviour; making this configurable (e.g. for multi-provider setups) is deferred

## Decisions

### D1: Three-phase pipeline — Parse → Validate → Build

Config loading is split into three strict phases with no cross-phase dependencies:

1. **Parse** — unmarshal YAML into plain structs; resolve secret fields from merged env; no validation
2. **Validate** — check all auth references resolve, required secrets present, no conflicting routes; return errors before any `net/http` is touched
3. **Build** — construct `http.Handler` from validated config; eager init only

**Why:** Oneserve mixed these concerns, leading to handlers that could fail silently mid-request. Strict separation means all errors surface at startup.

### D2: Secret fields — two forms

Standalone secret fields use a nested `env:` struct:
```yaml
oauth:
  client_secret:
    env: OAUTH_CLIENT_SECRET
```

List-item secrets (session keys) use a flat `env:` field on the item struct, since the parent key already names the thing:
```yaml
session:
  keys:
    - id: key-2025
      env: SESSION_KEY_2025
```

**Why:** The nested form is explicit when the field name alone doesn't convey what the secret is. The flat form avoids redundant nesting when context is already clear.

### D3: `secrets.env` for secret values

An optional `secrets.env` dotenv file lives alongside `config.yaml`. At startup it is loaded and merged with OS environment; OS env wins on conflict. Secret fields are resolved from this merged environment.

**Why over JSON secrets file:** dotenv is the de-facto standard for local secret management; simpler to edit; no indirection through a file path + ID pair (the old `JSONFilename`/`JSONID` mechanism).

**Why OS env wins:** allows deployment environments to override secrets without touching the file (e.g. injected by a secrets manager).

### D4: Route matching via `http.ServeMux`

Routes in config are registered directly onto `http.ServeMux`. No custom matching logic.

**Why:** Go 1.22+ ServeMux supports method+path patterns and specificity-based precedence. Writing a custom router adds complexity with no benefit for this use case.

No framework-imposed path conventions (e.g. no `/_/` prefix). Users express all paths, including the OAuth callback, in the config.

### D5: Named global auth policies

Auth policies are defined once in a top-level `auth:` section and referenced by name from routes. Policies use email glob patterns.

```yaml
auth:
  admin:
    allow: ["*@mycompany.com"]
  family:
    allow: ["alice@gmail.com", "bob@gmail.com"]

routes:
  - path: /private
    handler: pages
    auth: admin
```

**Why over per-route allow-lists:** policies act as named roles — changing membership means one edit. Routes stay clean. The old `session.allowed` global list is eliminated; instead, a user with no matching policy cannot create a session at all.

### D6: Session gating at OAuth callback

After OAuth authn, the user's email is checked against all defined auth policies. If the email matches no policy, no session is created and the user is denied. If it matches, a session is created storing the email only.

**Why:** Cleaner than a separate global allowed list. The auth policies are already the source of truth for who has access; duplicating that in `session.allowed` only causes confusion.

### D7: OAuth config is top-level, not per-route

OAuth credentials live in a top-level `oauth:` section containing a `providers:` list. Each entry names its `provider:` type (currently only `google` is implemented). The route typed `handler: auth` designates the base path for the entire auth route tree (login redirect, callback, logout). The auth library handles all sub-paths under this base; no explicit callback URL is declared in the oauth config — the callback URL is derived from `site.base_url` and the registered route path.

The providers list is structured for future extensibility (multiple providers in parallel), but currently only one provider is active at a time and only `google` is implemented.

**Why:** OAuth is a server-wide concern. Attaching credentials to a route conflates path registration with provider configuration (as the old JSON configs did).

### D8: Top-level `site:` block for page rendering config

Site-level rendering config lives in a top-level `site:` block, not on the `pages` handler:

```yaml
site:
  base_url: https://example.com   # canonical base URL, no trailing slash
  name: My Blog                   # available to templates as .Config.Name
  lang: en                        # BCP 47 language tag (default: "en")
```

These map directly to `page.SiteConfig` and are passed to `page.NewSite` via `page.WithConfig`.

**Why global, not per-handler:** `base_url` is used by two independent components — `page` templates (canonical links) and the OAuth handler (callback URL construction). Repeating it on the `pages` handler would be wrong; the OAuth handler has no handler config at all.

`include_drafts` is the only pages-specific rendering option and lives on the `pages` route handler since it affects what content is served, not site identity.

### D9: Built-in handler types

| Type | Description | Config fields |
|---|---|---|
| `pages` | Content via the `page` FS library | `include_drafts:` (default false) |
| `files` | Raw file serving via `http.FileServer`; no `page` overhead | see below |
| `redirect` | HTTP redirect | `to:`, optional `code:` (default 302) |
| `proxy` | Reverse proxy to a destination URL | `to:` |
| `auth` | Auth route tree (login, callback, logout); references top-level `oauth:` and `session:` config | _(none)_ |
| `defaultmux` | Go's default mux (`/debug/vars`, `/debug/pprof`) | _(none)_ |
| `status` | Health/status page | deferred |

`files` is distinct from `pages` — it serves a directory tree as-is via `http.FileServer`, useful for static assets where `page` rendering overhead is unwanted.

`files` handler config fields:
```yaml
- path: /assets/
  handler: files
  dir: ./static
  dirlist: false        # serve directory listings (default: false)
  index_html: true      # serve index.html for directory requests (default: true)
  dotfiles: false       # serve dotfiles (default: false)
  symlinks: false       # follow symlinks (default: false)
```

`dotfiles` and `symlinks` control both serving and listing — a disallowed file is neither served nor shown in directory listings.

### D10: Custom handler extensibility via raw YAML + Go API

Custom handler types are registered via the Go API at build time:
```go
pageserve.Build(cfg, pageserve.WithHandler("mytype", myFactory))
```

The route's handler-specific config is captured as a raw YAML node and passed to the factory for unmarshaling into its own struct.

**Why:** Keeps config declarative without requiring a plugin registry in the config format. The factory pattern is idiomatic Go.

### D11: Stats — request counts, serve latency, startup latency

Stats instrumentation is implemented in the `http` package, not the server assembly layer. Each handler is wrapped to record request count and serve latency. Startup latency is measured across the Build phase. Metrics are exposed via `defaultmux` (Go's `/debug/vars`).

**Why `/debug/vars`:** zero additional dependency; `expvar` is stdlib; pairs naturally with the `defaultmux` handler type which users opt into explicitly.

### D12: `proxy` handler — thin reverse proxy, client headers forwarded as-is

The `proxy` handler forwards requests to a destination URL using `endpoint.NewProxyRenderer` from `github.com/mnehpets/http`. The only config field is `to:`, an absolute URL.

```yaml
- path: /gh/
  handler: proxy
  to: https://api.github.com

- path: /admin/
  handler: proxy
  to: http://localhost:3001
  auth: members
```

The route prefix is stripped before forwarding: a request to `/gh/repos/foo` is forwarded as `/repos/foo`. This is implicit — no separate `strip_prefix` field.

Client request headers (including `Authorization`) are forwarded to the destination unchanged. No server-side header injection.

The `Host` header is set to the destination host (handled by `endpoint.NewProxyRenderer`).

**Why no header injection:** the two target use cases are a CORS/auth bypass relay (client supplies its own `Authorization`) and a localhost auth gate (no upstream auth needed). Server-side header injection would add config complexity with no benefit for these cases. Custom handler extension (D10) covers cases that need it.

**Why implicit prefix stripping:** consistent with how `redirect` works — `to:` is the destination, and the route path implicitly defines what prefix is consumed. Explicit `strip_prefix` fields only pay off when you need to forward without stripping, which is not a target use case.

### D13: Public Go API — Load and Build

The package exposes two public functions that the CLI calls:

```go
cfg, err := pageserve.Load(configPath string, opts ...LoadOption)
handler, err := pageserve.Build(cfg, ...BuildOption)
```

`Load` internally runs the Parse → Validate pipeline and returns a validated, plain `Config` struct. The caller may mutate the struct between Load and Build — the primary use case is applying CLI flag overrides (e.g. `cfg.Server.Address = *flagPort`).

The resolved environment (OS env merged with any dotenv file) is passed to Load via `WithEnv`:

```go
cfg, err := pageserve.Load(configPath, pageserve.WithEnv(env))
```

where `env` is a `map[string]string` the caller has assembled. The library is not responsible for loading dotenv files.

Custom handler factories are registered via `Build` options (D10). This API is the extension point for any caller that needs to customise the server (e.g. register additional handler types) without forking the core package.

**Why:** `Load`/`Build` gives callers a natural seam to inject CLI overrides. Exposing Parse and Validate as separate public calls adds surface with no real benefit — the three-phase separation remains an internal implementation detail. Keeping env loading out of the library removes a dotenv dependency and makes secret injection trivially testable.

## Config example — simple site

```yaml
server:
  address: ":8080"

oauth:
  providers:
    - provider: google
      client_id: 1234567890-abc.apps.googleusercontent.com
      client_secret:
        env: OAUTH_CLIENT_SECRET

session:
  cookie_name: pageserve_session
  keys:
    - id: key-2025
      env: SESSION_KEY_2025

auth:
  members:
    allow: ["*@mycompany.com"]

routes:
  - path: /auth/
    handler: auth

  - path: /debug/
    handler: defaultmux
    auth: members

  - path: /
    handler: pages
    auth: members
```

```shell
# secrets.env
OAUTH_CLIENT_SECRET=supersecretvalue
SESSION_KEY_2025=anothersecretvalue
```

## Config example — mixed public/private site

```yaml
server:
  address: ":8080"

oauth:
  providers:
    - provider: google
      client_id: 1234567890-abc.apps.googleusercontent.com
      client_secret:
        env: OAUTH_CLIENT_SECRET

session:
  cookie_name: pageserve_session
  keys:
    - id: key-2025
      env: SESSION_KEY_2025
    - id: key-2024
      env: SESSION_KEY_2024

auth:
  admin:
    allow: ["alice@example.com"]
  family:
    allow: ["alice@example.com", "bob@example.com"]

routes:
  - path: /auth/
    handler: auth

  - path: GET /notes/
    handler: pages
    auth: admin

  - path: /photos/
    handler: pages
    auth: family

  - path: /n
    handler: redirect
    to: /notes/

  - path: /
    handler: pages
```

### D14: Auth and session functionality provided by `mnehpets/http`

`mnehpets/http` provides reusable functionality for OAuth authentication, secure session storage of the authenticated username, and email pattern matching against provided lists of patterns. pageserve uses this functionality where appropriate rather than re-implementing it.

## Risks / Trade-offs

- **Go 1.22+ required** — `http.ServeMux` pattern matching (method prefixes, wildcards) was introduced in 1.22. Not a concern for a new package.
- **Session invalidation on key rotation** — removing all keys invalidates all sessions; the `id`+multi-key design allows graceful rotation (add new key, keep old key valid until sessions expire, then remove old key). The session library owns this logic.
- **`secrets.env` must not be committed** — documented convention, not enforced by the package. A `.gitignore` entry is the user's responsibility.
- **No config reload** — config is loaded once at startup. Changing config requires a restart. Acceptable for this use case; avoids the complexity of live reload.
