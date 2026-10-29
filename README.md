# proxify

HTTP proxy in Go. Routes `.ru`, `.su`, and `.рф` directly; sends everything else through SOCKS5, including DNS resolution. No external dependencies.

```sh
go build -o proxify .
./proxify  # HTTP: 127.0.0.1:10808; SOCKS5 upstream: 127.0.0.1:1080
```

Point apps at `http://127.0.0.1:10808`. Override defaults with `-listen`, `-socks5`, or `-config config.json`.

See [config.example.json](config.example.json) for named upstreams and domain-suffix rules. Rules use the first match, then `default_route`; `direct` is always available.
