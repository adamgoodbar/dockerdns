package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/docker/docker/client"

	"dockerdns/pkg/dnsupdater"
	"dockerdns/pkg/resolver"
	"dockerdns/pkg/watcher"
)

func main() {
	server := requireEnv("DNS_SERVER")
	zone := requireEnv("DNS_ZONE")
	ttl := uint32(envInt("TTL", 60))

	var tsig *dnsupdater.TSIGConfig
	if key := os.Getenv("DNS_TSIG_KEY"); key != "" {
		tsig = &dnsupdater.TSIGConfig{
			Key:    key,
			Secret: requireEnv("DNS_TSIG_SECRET"),
		}
	}

	docker, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("docker client: %v", err)
	}
	defer docker.Close()

	var (
		res            resolver.Resolver
		proxyContainer string
	)
	switch strings.ToLower(os.Getenv("REVERSE_PROXY_TYPE")) {
	case "traefik":
		var ip resolver.IPProvider
		if name := os.Getenv("REVERSE_PROXY_CONTAINER"); name != "" {
			ip = resolver.NewContainerIP(docker, name)
			proxyContainer = name
		} else if staticIP := os.Getenv("REVERSE_PROXY_IP"); staticIP != "" {
			ip = resolver.NewStaticIP(staticIP)
		} else {
			log.Fatal("REVERSE_PROXY_TYPE=traefik requires either REVERSE_PROXY_IP or REVERSE_PROXY_CONTAINER")
		}
		res = resolver.NewTraefik(ip)
		log.Printf("dockerdns started (server=%s zone=%s resolver=traefik ttl=%d)", server, zone, ttl)
	default:
		label := envOr("LABEL", "dns.hostname")
		res = resolver.NewLabel(label)
		log.Printf("dockerdns started (server=%s zone=%s label=%s ttl=%d)", server, zone, label, ttl)
	}

	owner := envOr("DOCKERDNS_OWNER", "default")
	u := dnsupdater.New(server, zone, ttl, tsig, owner)
	w := watcher.New(docker, u, res, proxyContainer)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := w.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("watcher: %v", err)
	}
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env var %s is not set", key)
	}
	return v
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Fatalf("invalid value for %s: %v", key, err)
	}
	return n
}
