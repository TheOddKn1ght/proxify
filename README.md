# proxify

A local HTTP proxy that routes traffic by destination. Hosts under `.ru`,
`.su`, `.рф` are dialed **directly**; everything else is forwarded through a
local **SOCKS5** server. Pure Go stdlib — zero dependencies.

Apps point at proxify as their HTTP proxy; proxify makes the per-connection
routing decision. Hostnames routed through SOCKS5 are sent to the server
unresolved (ATYP=DOMAIN), so DNS for proxied destinations never resolves
locally.

## Usage

```sh
go build -o proxify .

# Defaults: listen on 127.0.0.1:8118, SOCKS5 at 127.0.0.1:1080
./proxify

# Overrides
./proxify -listen 127.0.0.1:8888 -socks5 127.0.0.1:9050 -v

# Or with a config file
./proxify -config config.json
```

Point an app at it:

```sh
curl -x http://127.0.0.1:8118 https://ya.ru        # direct
curl -x http://127.0.0.1:8118 https://example.com  # via SOCKS5
export https_proxy=http://127.0.0.1:8118 http_proxy=http://127.0.0.1:8118
```

## Configuration

Without `-config`, built-in defaults apply (equivalent to
[config.example.json](config.example.json)):

```json
{
  "listen": "127.0.0.1:8118",
  "dial_timeout": "10s",
  "upstreams": {
    "socks": { "type": "socks5", "address": "127.0.0.1:1080" }
  },
  "rules": [
    { "type": "domain-suffix", "values": ["ru", "su", "рф"], "route": "direct" }
  ],
  "default_route": "socks"
}
```

- **upstreams** — named dialers. Types: `socks5` (optional `username`/
  `password`) and `direct`. The name `direct` is reserved and always
  available as a route.
- **rules** — evaluated in order, first match wins; no match falls through to
  `default_route`. The only type today is `domain-suffix`: matches the domain
  itself and any subdomain, on label boundaries (`ru` matches `mail.ru`, not
  `guru`). Unicode values like `рф` are converted to punycode at load time.
- Fields omitted from the config file keep their default values. Unknown
  fields are rejected — typos fail fast.

### Extending the rule engine

Rules are `Matcher` implementations behind a `type` string, so richer routing
plugs in without touching the proxy or router core:

1. Implement `router.Matcher` (`Match(host string) bool`) in
   `internal/router`.
2. Register the type name in `internal/config` (`knownRuleTypes`) and in the
   construction switch in `main.go` (`buildRouter`).

Matchers receive hosts already normalized (lowercase, no port, punycode), and
run in rule order — future types like `domain-regex`, `cidr`, or a
country-list file follow the same shape.
