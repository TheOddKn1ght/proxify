package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

const DirectRoute = "direct"

var (
	knownRuleTypes     = map[string]bool{"domain-suffix": true}
	knownUpstreamTypes = map[string]bool{"socks5": true, "direct": true}
)

type Config struct {
	Listen       string              `json:"listen"`
	DialTimeout  string              `json:"dial_timeout"`
	Upstreams    map[string]Upstream `json:"upstreams"`
	Rules        []Rule              `json:"rules"`
	DefaultRoute string              `json:"default_route"`
}

type Upstream struct {
	Type     string `json:"type"`
	Address  string `json:"address"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type Rule struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
	Route  string   `json:"route"`
}

func Default() *Config {
	return &Config{
		Listen:      "127.0.0.1:8118",
		DialTimeout: "10s",
		Upstreams: map[string]Upstream{
			"socks": {Type: "socks5", Address: "127.0.0.1:1080"},
		},
		Rules: []Rule{
			{Type: "domain-suffix", Values: []string{"ru", "su", "рф"}, Route: DirectRoute},
		},
		DefaultRoute: "socks",
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	loaded := &Config{}
	if err := unmarshalStrict(data, loaded); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	if loaded.Listen == "" {
		loaded.Listen = cfg.Listen
	}
	if loaded.DialTimeout == "" {
		loaded.DialTimeout = cfg.DialTimeout
	}
	if loaded.Upstreams == nil {
		loaded.Upstreams = cfg.Upstreams
	}
	if loaded.Rules == nil {
		loaded.Rules = cfg.Rules
	}
	if loaded.DefaultRoute == "" {
		loaded.DefaultRoute = cfg.DefaultRoute
	}
	return loaded, nil
}

func unmarshalStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func (c *Config) Validate() error {
	if c.Listen == "" {
		return fmt.Errorf("listen address is empty")
	}
	if _, err := c.ParsedDialTimeout(); err != nil {
		return err
	}
	for name, u := range c.Upstreams {
		if name == DirectRoute {
			return fmt.Errorf("upstream name %q is reserved", DirectRoute)
		}
		if !knownUpstreamTypes[u.Type] {
			return fmt.Errorf("upstream %q: unknown type %q", name, u.Type)
		}
		if u.Type == "socks5" && u.Address == "" {
			return fmt.Errorf("upstream %q: socks5 requires an address", name)
		}
	}
	routes := func(route string) bool {
		if route == DirectRoute {
			return true
		}
		_, ok := c.Upstreams[route]
		return ok
	}
	for i, r := range c.Rules {
		if !knownRuleTypes[r.Type] {
			return fmt.Errorf("rule %d: unknown type %q", i, r.Type)
		}
		if len(r.Values) == 0 {
			return fmt.Errorf("rule %d: no values", i)
		}
		if !routes(r.Route) {
			return fmt.Errorf("rule %d: unknown route %q", i, r.Route)
		}
	}
	if !routes(c.DefaultRoute) {
		return fmt.Errorf("default_route %q is not a known upstream", c.DefaultRoute)
	}
	return nil
}

func (c *Config) ParsedDialTimeout() (time.Duration, error) {
	d, err := time.ParseDuration(c.DialTimeout)
	if err != nil {
		return 0, fmt.Errorf("dial_timeout %q: %w", c.DialTimeout, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("dial_timeout %q is negative", c.DialTimeout)
	}
	return d, nil
}
