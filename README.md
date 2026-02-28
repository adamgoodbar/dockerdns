# dockerdns

WARNING: Vibecoded and a WIP.  Not recommended to use.

A lightweight daemon that watches Docker container events and registers/deregisters DNS A records via [RFC 2136 Dynamic DNS UPDATE](https://dataworks.com/RFC2136).

When a container with the configured label starts, its IP is registered in DNS. When it stops, the record is removed.

## How it works

1. Connects to the Docker socket and listens for `start` and `die` events
2. On `start`: inspects the container for a DNS hostname label, gets its IP, sends an RFC 2136 UPDATE to add an A record
3. On `die`: sends an RFC 2136 UPDATE to remove the A record

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
| `LABEL` | no | `dns.hostname` | Docker label to read the FQDN from |
| `TTL` | no | `60` | DNS record TTL in seconds |

## Usage

Label containers with the configured label (default `dns.hostname`):

```sh
docker run -d \
  --label dns.hostname=myapp.example.com \
  nginx
```

Run dockerdns with the Docker socket mounted:

```sh
docker run -d \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e DNS_SERVER=ns1.example.com:53 \
  -e DNS_ZONE=example.com. \
  dockerdns
```

With TSIG authentication:

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
