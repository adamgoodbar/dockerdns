# dockerdns

WARNING: Vibecoded and a WIP.  Not recommended to use.

A lightweight daemon that watches Docker container events and registers/deregisters DNS A records via [RFC 2136 Dynamic DNS UPDATE](https://dataworks.com/RFC2136).

When a container starts, its hostname(s) and IP are registered in DNS. When it stops, the records are removed. Two resolver modes are supported: direct label-based (default) and reverse-proxy-aware (Traefik).

## How it works

1. Connects to the Docker socket and listens for `start` and `die` events
2. On `start`: inspects the container, resolves hostname(s) and IP via the configured resolver, sends RFC 2136 UPDATE to add A record(s)
3. On `die`: sends RFC 2136 UPDATE to remove the A record(s)

## Requirements

- A DNS server with RFC 2136 dynamic updates enabled (BIND, PowerDNS, CoreDNS, etc.)
- Docker socket access

## Configuration

All configuration is via environment variables.

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DNS_SERVER` | yes | — | DNS server address, e.g. `ns1.example.com:53` |
| `DNS_ZONE` | yes | — | Zone to update, e.g. `example.com.` |
| `DNS_TSIG_KEY` | no | — | TSIG key name for authenticated updates |
| `DNS_TSIG_SECRET` | no | — | TSIG secret (base64-encoded) |
| `LABEL` | no | `dns.hostname` | Docker label to read the FQDN from (label resolver only) |
| `TTL` | no | `60` | DNS record TTL in seconds |
| `REVERSE_PROXY_TYPE` | no | — | Set to `traefik` to enable proxy-aware resolver |
| `REVERSE_PROXY_IP` | yes if proxy type set | — | IP of the reverse proxy to register for all records |

## Resolver modes

### Label resolver (default)

Reads a single hostname from a Docker label on each container and registers the container's own IP.

Label containers with the configured label (default `dns.hostname`):

```sh
docker run -d \
  --label dns.hostname=myapp.example.com \
  nginx
```

### Traefik resolver

When containers sit behind a reverse proxy like Traefik, the IP to register is the proxy's IP, and hostnames come from Traefik's routing rules.

Set `REVERSE_PROXY_TYPE=traefik` and `REVERSE_PROXY_IP` to the proxy's IP. dockerdns will scan each container's `traefik.http.routers.*.rule` labels, extract all `Host(...)` values, and register them pointing at the proxy IP.

Both backtick and double-quote syntax are supported, and multiple routers or multiple hosts per rule all produce individual DNS records:

```sh
# Single host
docker run -d \
  --label "traefik.http.routers.myapp.rule=Host(\`myapp.example.com\`)" \
  nginx

# Multiple hosts in one rule
docker run -d \
  --label 'traefik.http.routers.myapp.rule=Host("a.example.com", "b.example.com")' \
  nginx
```

## Usage

### Label resolver

```sh
docker run -d \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e DNS_SERVER=ns1.example.com:53 \
  -e DNS_ZONE=example.com. \
  dockerdns
```

### Traefik resolver

```sh
docker run -d \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e DNS_SERVER=ns1.example.com:53 \
  -e DNS_ZONE=example.com. \
  -e REVERSE_PROXY_TYPE=traefik \
  -e REVERSE_PROXY_IP=10.0.0.5 \
  dockerdns
```

### With TSIG authentication

```sh
docker run -d \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e DNS_SERVER=ns1.example.com:53 \
  -e DNS_ZONE=example.com. \
  -e DNS_TSIG_KEY=mykey. \
  -e DNS_TSIG_SECRET=<base64-secret> \
  dockerdns
```

## Building

```sh
# Local
go build -o dockerdns .

# Docker
docker build -t dockerdns .
```
