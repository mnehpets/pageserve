## Why

`pageserve` is a new package that sits above the `page` content rendering library and handles server assembly: config, routing, auth, and session management. None of these exist yet. Routing, auth, and server assembly are tightly coupled to the config schema — the config fields are directly dictated by what those components need — so the foundational design should cover all of them together.

## What Changes

**Config**
- A YAML config file drives the entire server; it is safe to commit (no secrets)
- Secrets are kept in a separate `secrets.env` file and referenced by name from the config
- Config is validated at startup; missing or invalid values are hard errors before the server starts

**Routing**
- Routes are declared as a flat list in config
- Each route specifies a path and a handler type, with an optional auth policy reference
- Routes without an auth reference are public

**Handler types**
- `pages` — serves content via the `page` FS library
- `files` — serves raw files from the filesystem, no `page` processing overhead
- `redirect` — HTTP redirect
- `proxy` — reverse proxy to a specified destination URL
- `oauth` — OAuth callback handler
- `defaultmux` — Go's default mux (exposes `/debug/vars`, `/debug/pprof`)
- `status` — deferred; may be added later
- Custom handlers — extensible via the Go API

**Auth**
- Authentication: OAuth only (Google)
- Authorisation: named allow-list policies referenced by routes
- Users with no matching policy cannot authenticate

**Server assembly**
- The server is assembled from validated config into a single `http.Handler`
- All initialisation is eager; no lazy or async init

## Capabilities

### New Capabilities

- `config-schema`: YAML config schema covering server, OAuth, session, routes, and auth policies
- `config-pipeline`: Parse → Validate → Build pipeline with strict phase separation
- `secrets-loading`: Separate secrets file, merged with OS env, referenced by name from config
- `routing`: Flat route list with handler dispatch and auth policy references
- `auth`: OAuth authn, named email allow-list policies, session gate, session cookie management
- `server-assembly`: Assembles all components into a single `http.Handler`

### Modified Capabilities

_(none — this is the initial implementation)_

## Impact

- New Go package `pageserve` (no existing code affected)
- `secrets.env` lives alongside `config.yaml`, outside `public/` content FS, excluded from version control
- Depends on the existing `page` library for content rendering; no changes to `page` required
- Requires Go 1.22+
