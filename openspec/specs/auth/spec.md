## ADDED Requirements

### Requirement: Google OAuth authentication
The package SHALL support Google as the OAuth provider. The OAuth flow SHALL be initiated when an unauthenticated request hits a protected route.

#### Scenario: Redirect to provider
- **WHEN** an unauthenticated request arrives at a route with `auth: <policy>`
- **THEN** the response redirects to the Google OAuth authorisation URL

### Requirement: Post-authentication redirect preserves original URL
When an unauthenticated request triggers an OAuth redirect, the original URL SHALL be preserved and the user returned to it after successful login. This is handled by `PrepareAuth` and `ResultEndpoint` in `mnehpets/http`: `PrepareAuth` stores `NextURL` in a secure cookie, which is then passed to the `ResultEndpoint` callback on login success or failure.

#### Scenario: User returned to original URL after login
- **WHEN** an unauthenticated request to `/private/page` triggers an OAuth redirect
- **THEN** after successful authentication the user is redirected back to `/private/page`

### Requirement: Named email allow-list policies
Auth policies SHALL be defined globally in the `auth:` config section. Each policy SHALL have an `allow:` list of email glob patterns.

#### Scenario: Glob pattern match
- **WHEN** the policy allow-list contains `"*@mycompany.com"` and the authenticated email is `alice@mycompany.com`
- **THEN** the user is granted access

#### Scenario: Exact email match
- **WHEN** the allow-list contains `"bob@example.com"` and the authenticated email is `bob@example.com`
- **THEN** the user is granted access

#### Scenario: No match
- **WHEN** the authenticated email matches no pattern in any defined policy
- **THEN** no session is created and the user is denied

### Requirement: Session gating at OAuth callback
After OAuth authn, the user's email SHALL be checked against all defined auth policies. If no policy matches, no session SHALL be created.

#### Scenario: Unauthorised user blocked at callback
- **WHEN** a user authenticates via OAuth but their email matches no policy
- **THEN** no session cookie is set and the response is an access denied error

#### Scenario: Authorised user gets session
- **WHEN** a user authenticates via OAuth and their email matches at least one policy
- **THEN** a session is created storing the user's email

### Requirement: Session stores email only
The session SHALL store the authenticated user's email address. Role membership SHALL NOT be stored in the session; it SHALL be re-evaluated from config on each request.

#### Scenario: Role re-evaluation per request
- **WHEN** a request arrives with a valid session
- **THEN** the handler checks the session email against the route's auth policy from the current config

### Requirement: Routes without auth are public
A route with no `auth:` field SHALL be accessible without a session or authentication.

#### Scenario: Public route access
- **WHEN** a request arrives at a route with no `auth:` field
- **THEN** the request is handled without checking for a session

### Requirement: Session cookie management
Sessions SHALL use a signed cookie. The session library's key rotation mechanism SHALL be used; the `session.keys` config provides key ID and secret pairs.

#### Scenario: Key rotation — old sessions still valid
- **WHEN** a new key is added to `session.keys` and the old key is retained
- **THEN** sessions signed with the old key remain valid until they expire naturally
