# dockerdns

## Project structure

```
dockerdns/
├── main.go              # Config loading (env vars), wires packages, signal handling
├── Dockerfile           # Multi-stage: golang builder + alpine runtime
└── pkg/
    ├── dnsupdater/      # RFC 2136 DNS UPDATE client (package dnsupdater)
    │   └── dnsupdater.go
    ├── resolver/        # Hostname/IP resolution strategies (package resolver)
    │   ├── resolver.go  # Resolver interface and Record type
    │   ├── ip.go        # IPProvider interface, StaticIP, ContainerIP
    │   ├── label.go     # LabelResolver — reads dns.hostname label
    │   └── traefik.go   # TraefikResolver — reads traefik router rules
    └── watcher/         # Docker event watcher (package watcher)
        └── watcher.go
```

## Build

```sh
go build ./...
docker build -t dockerdns .
```

## Key conventions

- Package naming: `dnsupdater` (not `dns`) to avoid collision with `github.com/miekg/dns`
- Constructor pattern: `dnsupdater.New(...)` / `watcher.New(...)` (not `NewUpdater`/`NewWatcher`)
- Config via environment variables only — no config files
- `tsigConfig` fields are exported (`Key`, `Secret`) since it crosses package boundaries

## Dependencies

- `github.com/docker/docker` — Docker client
- `github.com/miekg/dns` — RFC 2136 DNS UPDATE (aliased as `mdns` inside `dnsupdater` package)

## Environment variables

| Var | Required | Default | Description |
|-----|----------|---------|-------------|
| `DNS_SERVER` | yes | — | DNS server `host:port` |
| `DNS_ZONE` | yes | — | Zone to update, e.g. `example.com.` |
| `DNS_TSIG_KEY` | no | — | TSIG key name |
| `DNS_TSIG_SECRET` | no | — | TSIG secret (base64) |
| `LABEL` | no | `dns.hostname` | Docker label to read FQDN from (ignored when `REVERSE_PROXY_TYPE` is set) |
| `TTL` | no | `60` | Record TTL in seconds |
| `REVERSE_PROXY_TYPE` | no | — | `traefik` — use proxy-aware resolver |
| `REVERSE_PROXY_IP` | yes if proxy type set and no container name | — | IP of reverse proxy to register for all records |
| `REVERSE_PROXY_CONTAINER` | no | — | Docker container name to resolve proxy IP dynamically (alternative to `REVERSE_PROXY_IP`) |
