## ADDED Requirements

### Requirement: Caller-supplied environment map
The library SHALL NOT load dotenv files or read `os.Environ` directly. Instead, the resolved environment SHALL be passed to `Load` via the `WithEnv(env map[string]string)` option. The caller is responsible for assembling this map (typically: parse a dotenv file, merge with `os.Environ()`, OS env wins on conflict).

#### Scenario: Env map provided
- **WHEN** `Load` is called with `WithEnv(env)` where `env` contains `OAUTH_CLIENT_SECRET=secret`
- **THEN** secret fields referencing `OAUTH_CLIENT_SECRET` resolve to `secret`

#### Scenario: No WithEnv option
- **WHEN** `Load` is called without `WithEnv`
- **THEN** secret field resolution uses an empty environment; any declared secret fields will fail Validate

### Requirement: Undefined secret is a hard error
If a secret field references an env var name that is absent from the provided env map, Validate SHALL return an error.

#### Scenario: Missing secret
- **WHEN** a provider's `client_secret.env` names a variable not present in the env map
- **THEN** Validate returns an error identifying the missing variable
