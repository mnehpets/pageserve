## ADDED Requirements

### Requirement: Single http.Handler output
The Build phase SHALL produce a single `http.Handler` that dispatches all registered routes.

#### Scenario: All routes reachable
- **WHEN** Build completes successfully
- **THEN** every route declared in config is registered and reachable via the returned handler

### Requirement: Custom handler factory API
The Go API SHALL accept custom handler factories via a `WithHandler(name, factory)` option on Build. Factories registered this way MAY unmarshal handler-specific config from the raw YAML node provided.

#### Scenario: Factory called with raw config
- **WHEN** a route with `handler: mytype` is processed and a factory for `"mytype"` is registered
- **THEN** the factory receives the route's raw YAML node and returns an `http.Handler`

### Requirement: No config reload
Config is loaded once at startup. A server restart is required to apply config changes.

#### Scenario: Config file modified at runtime
- **WHEN** `config.yaml` is modified while the server is running
- **THEN** the running server is unaffected; changes take effect only after restart
