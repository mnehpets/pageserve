package pageserve

import (
	"encoding/base64"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// parse reads the config file at configPath, unmarshals it into Config, resolves
// secret fields from env, and attaches raw YAML nodes to each route.
func parse(configPath string, env map[string]string) (Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return Config{}, fmt.Errorf("pageserve: read config %q: %w", configPath, err)
	}

	// Decode into typed Config struct.
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("pageserve: parse config: %w", err)
	}

	// Decode raw document to capture per-route YAML nodes for custom handlers.
	var rawDoc yaml.Node
	if err := yaml.Unmarshal(data, &rawDoc); err != nil {
		return Config{}, fmt.Errorf("pageserve: parse config (raw): %w", err)
	}
	if rawDoc.Kind == yaml.DocumentNode && len(rawDoc.Content) > 0 {
		attachRouteNodes(&cfg, rawDoc.Content[0])
	}

	// Resolve secret fields from the provided env map.
	resolveSecrets(&cfg, env)

	return cfg, nil
}

// resolveSecrets substitutes Env var names with their values from env.
func resolveSecrets(cfg *Config, env map[string]string) {
	for i := range cfg.OAuth.Providers {
		p := &cfg.OAuth.Providers[i]
		if p.ClientSecret.Env != "" {
			p.ClientSecret.Value = env[p.ClientSecret.Env]
		}
	}
	for i := range cfg.Session.Keys {
		k := &cfg.Session.Keys[i]
		if k.Env != "" {
			raw, err := base64.StdEncoding.DecodeString(env[k.Env])
			if err != nil {
				// Leave Value empty; validation will catch the missing key.
				continue
			}
			k.Value = string(raw)
		}
	}
}

// attachRouteNodes walks the raw YAML mapping node to find the "routes" sequence
// and stores each item's node in the corresponding RouteConfig.Raw.
func attachRouteNodes(cfg *Config, mappingNode *yaml.Node) {
	if mappingNode == nil || mappingNode.Kind != yaml.MappingNode {
		return
	}
	// Mapping nodes store key-value pairs as alternating children.
	for i := 0; i+1 < len(mappingNode.Content); i += 2 {
		if mappingNode.Content[i].Value != "routes" {
			continue
		}
		routesNode := mappingNode.Content[i+1]
		if routesNode.Kind != yaml.SequenceNode {
			return
		}
		for j, itemNode := range routesNode.Content {
			if j < len(cfg.Routes) {
				n := *itemNode
				cfg.Routes[j].Raw = &n
			}
		}
		return
	}
}
