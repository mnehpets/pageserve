## 1. Package scaffolding

- [ ] 1.1 Create `pageserve` package with top-level `Config` struct covering all sections: `Server`, `OAuth`, `Session`, `Auth`, `Site`, `Routes`
- [ ] 1.2 Define secret field types: nested `SecretField{Env string}` for standalone secrets (e.g. `client_secret`), flat `env:` string on struct for list-item secrets (e.g. session keys)
- [ ] 1.3 Define `LoadOption` and `BuildOption` functional option types

## 2. Config schema

- [ ] 2.1 Write the formal config schema for `config.yaml`: all sections, fields, types, required/optional status, and valid values (e.g. provider enum)

## 3. Config parsing

- [ ] 3.1 Implement Parse: unmarshal `config.yaml` into `Config`
- [ ] 3.2 Implement secret field resolution: substitute `env:` names with values from the provided env map at Parse time
- [ ] 3.3 Implement `WithEnv(env map[string]string)` as a `LoadOption`; default to empty map if absent

## 3. Validation

- [ ] 3.1 Validate required fields are present (`server.address`, `session.cookie_name`, at least one session key)
- [ ] 3.2 Validate all declared secret fields resolved (env var present in map); report missing var names
- [ ] 3.3 Validate all route `auth:` references resolve to a defined policy in `auth:`
- [ ] 3.4 Validate all `oauth.providers` entries declare a supported provider type; return error for unknown types
- [ ] 3.5 Validate all route `handler:` types are known (built-in or registered via `WithHandler`); return error for unknown types

## 4. Public Load API

- [ ] 4.1 Implement `pageserve.Load(configPath string, opts ...LoadOption) (Config, error)` running Parse then Validate internally

## 5. Handler implementations

- [ ] 5.1 Implement `pages` handler: serve content via `page` FS library; support `include_drafts` bool (default false)
- [ ] 5.2 Implement `files` handler: serve directory via `http.FileServer`; support `dir`, `dirlist` (default false), `index_html` (default true), `dotfiles` (default false), `symlinks` (default false)
- [ ] 5.3 Implement `redirect` handler: issue HTTP redirect to `to:` URL with `code:` status (default 302)
- [ ] 5.4 Implement `proxy` handler: forward to `to:` URL via `endpoint.NewProxyRenderer`; strip route path prefix before forwarding; forward client headers unchanged
- [ ] 5.5 Implement `defaultmux` handler: serve Go's default mux at the declared path
- [ ] 5.6 Implement `auth` handler: register OAuth login, callback, and logout sub-paths under the declared base path using `mnehpets/http` auth; derive callback URL from `site.base_url` and the registered path; use `PrepareAuth` to store `NextURL` in secure cookie before redirecting to provider

## 6. Auth session middleware

- [ ] 6.1 Wire session-check middleware using `mnehpets/http` `RequireEmailMatch`; supply an `onFailure` handler that calls `PrepareAuth` and redirects to login with `NextURL` set to the original request path
- [ ] 6.2 Build the `matchFn` for each named policy by calling `mnehpets/http` email pattern matching against the policy's `allow:` list
- [ ] 6.4 In the OAuth `ResultEndpoint` callback: check authenticated email against all defined policies; if no policy matches, return access denied and do not create a session; if matched, create session storing email only

## 7. Build and server assembly

- [ ] 7.1 Implement `pageserve.Build(cfg Config, opts ...BuildOption) (http.Handler, error)`
- [ ] 7.2 Register all routes from config onto `http.ServeMux` using declared paths verbatim; fail immediately on handler init error with no goroutines left running
- [ ] 7.3 Wrap each route handler with session-check auth middleware when the route has an `auth:` policy
- [ ] 7.4 Implement `WithHandler(name string, factory HandlerFactory)` as a `BuildOption` for custom handler registration; pass raw YAML route node to factory
- [ ] 7.5 Wrap each registered handler with stats instrumentation recording request count and serve latency via `expvar`
- [ ] 7.6 Record Build phase duration as an `expvar` value on completion
