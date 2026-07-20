package router

import (
	"context"
	"fmt"
	"net"
	"strings"
)

type Dialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

type Matcher interface {
	Match(host string) bool
}

type Rule struct {
	Matcher Matcher
	Route   string
}

type Router struct {
	rules        []Rule
	upstreams    map[string]Dialer
	defaultRoute string
}

func New(rules []Rule, upstreams map[string]Dialer, defaultRoute string) (*Router, error) {
	if _, ok := upstreams[defaultRoute]; !ok {
		return nil, fmt.Errorf("router: default route %q is not a known upstream", defaultRoute)
	}
	for i, r := range rules {
		if _, ok := upstreams[r.Route]; !ok {
			return nil, fmt.Errorf("router: rule %d routes to unknown upstream %q", i, r.Route)
		}
	}
	return &Router{rules: rules, upstreams: upstreams, defaultRoute: defaultRoute}, nil
}

func (r *Router) RouteFor(host string) (string, Dialer) {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	normalized, err := NormalizeHost(host)
	if err != nil {
		return r.defaultRoute, r.upstreams[r.defaultRoute]
	}
	for _, rule := range r.rules {
		if rule.Matcher.Match(normalized) {
			return rule.Route, r.upstreams[rule.Route]
		}
	}
	return r.defaultRoute, r.upstreams[r.defaultRoute]
}

type DomainSuffix struct {
	suffixes []string
}

func NewDomainSuffix(values []string) (*DomainSuffix, error) {
	m := &DomainSuffix{}
	for _, v := range values {
		normalized, err := NormalizeHost(strings.TrimPrefix(v, "."))
		if err != nil {
			return nil, fmt.Errorf("domain-suffix %q: %w", v, err)
		}
		if normalized == "" {
			return nil, fmt.Errorf("domain-suffix: empty value")
		}
		m.suffixes = append(m.suffixes, normalized)
	}
	return m, nil
}

func (m *DomainSuffix) Match(host string) bool {
	for _, s := range m.suffixes {
		if host == s || strings.HasSuffix(host, "."+s) {
			return true
		}
	}
	return false
}
