package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"proxify/internal/config"
	"proxify/internal/proxy"
	"proxify/internal/router"
	"proxify/internal/socks5"
)

func main() {
	var (
		configPath = flag.String("config", "", "path to JSON config file (optional; built-in defaults apply)")
		listen     = flag.String("listen", "", "listen address, overrides config (default 127.0.0.1:10808)")
		socksAddr  = flag.String("socks5", "", "SOCKS5 server address, overrides config (default 127.0.0.1:1080)")
		verbose    = flag.Bool("v", false, "debug logging")
	)
	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	if err := run(*configPath, *listen, *socksAddr, log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(configPath, listen, socksAddr string, log *slog.Logger) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if listen != "" {
		cfg.Listen = listen
	}
	if socksAddr != "" {
		overrideSocksAddr(cfg, socksAddr)
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	r, dialTimeout, err := buildRouter(cfg)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return proxy.ListenAndServe(ctx, cfg.Listen, proxy.New(r, log, dialTimeout))
}

func overrideSocksAddr(cfg *config.Config, addr string) {
	for name, u := range cfg.Upstreams {
		if u.Type == "socks5" {
			u.Address = addr
			cfg.Upstreams[name] = u
		}
	}
}

func buildRouter(cfg *config.Config) (*router.Router, time.Duration, error) {
	dialTimeout, err := cfg.ParsedDialTimeout()
	if err != nil {
		return nil, 0, err
	}

	upstreams := map[string]router.Dialer{
		config.DirectRoute: &net.Dialer{},
	}
	for name, u := range cfg.Upstreams {
		switch u.Type {
		case "socks5":
			upstreams[name] = &socks5.Client{
				Addr:     u.Address,
				Username: u.Username,
				Password: u.Password,
				Timeout:  dialTimeout,
			}
		case "direct":
			upstreams[name] = &net.Dialer{}
		default:
			return nil, 0, fmt.Errorf("upstream %q: unknown type %q", name, u.Type)
		}
	}

	var rules []router.Rule
	for i, rc := range cfg.Rules {
		var m router.Matcher
		switch rc.Type {
		case "domain-suffix":
			m, err = router.NewDomainSuffix(rc.Values)
			if err != nil {
				return nil, 0, fmt.Errorf("rule %d: %w", i, err)
			}
		default:
			return nil, 0, fmt.Errorf("rule %d: unknown type %q", i, rc.Type)
		}
		rules = append(rules, router.Rule{Matcher: m, Route: rc.Route})
	}

	r, err := router.New(rules, upstreams, cfg.DefaultRoute)
	if err != nil {
		return nil, 0, err
	}
	return r, dialTimeout, nil
}
