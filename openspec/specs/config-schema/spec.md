## ADDED Requirements

### Requirement: Server listeners config
The config SHALL include a top-level `server:` section with a `listeners:` list. Each entry SHALL have an `address:` field. An optional `tls:` sub-section with `cert_file:` and `key_file:` enables HTTPS for that listener. At least one listener is required. Multiple listeners may be declared to serve HTTP and HTTPS simultaneously or on multiple ports.

#### Scenario: Single plain HTTP listener
- **WHEN** `server.listeners` contains one entry with no `tls:` field
- **THEN** the server binds a plain HTTP listener to that address

#### Scenario: HTTPS listener
- **WHEN** a listener entry includes `tls.cert_file` and `tls.key_file`
- **THEN** the server binds a TLS listener to that address using those certificate and key files

#### Scenario: Simultaneous HTTP and HTTPS
- **WHEN** `server.listeners` contains both a plain and a TLS entry
- **THEN** both listeners start concurrently sharing the same handler

#### Scenario: No listeners
- **WHEN** `server.listeners` is absent or empty
- **THEN** Validate returns an error

### Requirement: OAuth config section
The config SHALL include a top-level `oauth:` section with a `providers:` list. Each entry SHALL have a `provider:` field naming the provider type (currently only `google` is supported), plus `client_id:` and `client_secret:` (secret field). No callback URL is declared here; it is derived from `site.base_url` and the registered `auth` route path.

#### Scenario: Single Google provider
- **WHEN** `oauth.providers` contains one entry with `provider: google`
- **THEN** Google OAuth is used for all auth routes

#### Scenario: Secret field form
- **WHEN** a provider entry's `client_secret` is declared as `env: SOME_VAR`
- **THEN** the value is resolved from the provided env map at Parse time

#### Scenario: Unknown provider type
- **WHEN** a provider entry declares `provider: github`
- **THEN** Validate returns an error indicating that provider is not yet supported

### Requirement: Session config section
The config SHALL include a top-level `session:` section with `cookie_name:` and a `keys:` list. Each key entry SHALL have an `id:` string and an `env:` field naming the secret environment variable.

#### Scenario: Multiple session keys
- **WHEN** `session.keys` contains two entries with distinct IDs
- **THEN** both keys are loaded and passed to the session library for rotation support

### Requirement: Auth policies section
The config SHALL include a top-level `auth:` section mapping policy names to allow-list definitions. Each policy SHALL have an `allow:` list of email glob patterns.

#### Scenario: Named policy reference
- **WHEN** a route references `auth: admin` and `auth.admin` is defined
- **THEN** the route enforces that policy

### Requirement: Site config section
The config SHALL include a top-level `site:` section with at minimum `base_url:`. Optional fields include `name:` and `lang:` (BCP 47, default `"en"`).

#### Scenario: base_url used by multiple components
- **WHEN** `site.base_url` is set
- **THEN** the `pages` handler and OAuth callback construction derive their base URL from this field

### Requirement: Routes list
The config SHALL include a top-level `routes:` list. Each entry SHALL have at minimum a `path:` and `handler:` field. An optional `auth:` field names a policy.

#### Scenario: Public route
- **WHEN** a route has no `auth:` field
- **THEN** the route is accessible without a session

#### Scenario: Protected route
- **WHEN** a route has `auth: <policy>`
- **THEN** requests without a valid session matching the policy are redirected to OAuth

### Requirement: Config is safe to commit
All secret values SHALL be referenced by env var name in the config, never as inline values.

#### Scenario: No secrets in config.yaml
- **WHEN** `config.yaml` is inspected
- **THEN** it contains no OAuth client secrets, session signing keys, or other credentials
