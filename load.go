package pageserve

// loadOptions holds functional options for Load.
type loadOptions struct {
	env map[string]string
}

// LoadOption configures the Load function.
type LoadOption func(*loadOptions)

// WithEnv provides the resolved environment map used for secret field resolution.
// The caller is responsible for constructing this map (e.g. by merging a dotenv
// file with os.Environ). If not provided, an empty map is used and any declared
// secret fields will fail validation.
func WithEnv(env map[string]string) LoadOption {
	return func(o *loadOptions) {
		o.env = env
	}
}

// Load reads and validates the config file at configPath, returning a validated
// Config struct. The caller may mutate the returned Config before passing it to Build.
//
// Secrets are resolved from the env map provided via WithEnv. If WithEnv is not
// provided, an empty map is used and any declared secret fields will fail validation.
//
// Load runs Parse then Validate internally. Handler type validation for custom
// handler types (registered via WithHandler on Build) is deferred to Build.
func Load(configPath string, opts ...LoadOption) (Config, error) {
	o := &loadOptions{}
	for _, opt := range opts {
		opt(o)
	}
	env := o.env
	if env == nil {
		env = make(map[string]string)
	}

	cfg, err := parse(configPath, env)
	if err != nil {
		return Config{}, err
	}

	// Pass nil customHandlers: handler type validation is deferred to Build.
	if err := validate(cfg, nil); err != nil {
		return Config{}, err
	}

	return cfg, nil
}
