## ADDED Requirements

### Requirement: Public API is Load and Build
The public API SHALL expose two functions: `Load` (which internally runs Parse then Validate) and `Build`. `Load` returns a plain, mutable `Config` struct; the caller may modify it before passing it to `Build`.

### Requirement: Three strict internal phases — Parse, Validate, Build
Internally, config processing SHALL be split into three phases with no cross-phase dependencies. Parse produces plain structs; Validate checks correctness; Build constructs the handler. These phases are implementation details, not separate public API calls.

#### Scenario: Parse does not touch net/http
- **WHEN** the Parse phase runs
- **THEN** no `net/http` types are constructed or referenced

#### Scenario: Validate does not touch net/http
- **WHEN** the Validate phase runs
- **THEN** no `net/http` types are constructed or referenced

### Requirement: Validation errors before server start
All config errors SHALL be reported by `Load` before `Build` is called. The server SHALL not start with an invalid config.

#### Scenario: Missing required field
- **WHEN** a required config field is absent
- **THEN** Validate returns an error and Build is not called

#### Scenario: Unresolvable auth reference
- **WHEN** a route references `auth: nonexistent`
- **THEN** Validate returns an error naming the unresolved reference

