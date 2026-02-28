package resolver

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types/container"
)

type mockIPProvider struct {
	ip  string
	err error
}

func (m *mockIPProvider) ProxyIP(_ context.Context) (string, error) {
	return m.ip, m.err
}

func traefikInspect(labels map[string]string) container.InspectResponse {
	return container.InspectResponse{
		Config: &container.Config{Labels: labels},
	}
}

func TestTraefikResolver_SingleHost(t *testing.T) {
	r := NewTraefik(&mockIPProvider{ip: "1.2.3.4"})
	records, err := r.Records(context.Background(), traefikInspect(map[string]string{
		"traefik.http.routers.myapp.rule": "Host(`myapp.example.com`)",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 || records[0].Hostname != "myapp.example.com" || records[0].IP != "1.2.3.4" {
		t.Fatalf("unexpected records: %v", records)
	}
}

func TestTraefikResolver_MultipleRouters(t *testing.T) {
	r := NewTraefik(&mockIPProvider{ip: "1.2.3.4"})
	records, err := r.Records(context.Background(), traefikInspect(map[string]string{
		"traefik.http.routers.app1.rule": "Host(`app1.example.com`)",
		"traefik.http.routers.app2.rule": "Host(`app2.example.com`)",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d: %v", len(records), records)
	}
	hostSet := map[string]bool{}
	for _, rec := range records {
		hostSet[rec.Hostname] = true
		if rec.IP != "1.2.3.4" {
			t.Fatalf("unexpected IP: %s", rec.IP)
		}
	}
	if !hostSet["app1.example.com"] || !hostSet["app2.example.com"] {
		t.Fatalf("missing expected hostnames: %v", hostSet)
	}
}

func TestTraefikResolver_MultipleHostsInOneRule(t *testing.T) {
	r := NewTraefik(&mockIPProvider{ip: "1.2.3.4"})
	records, err := r.Records(context.Background(), traefikInspect(map[string]string{
		"traefik.http.routers.myapp.rule": `Host("a.com", "b.com")`,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d: %v", len(records), records)
	}
	hostSet := map[string]bool{}
	for _, rec := range records {
		hostSet[rec.Hostname] = true
	}
	if !hostSet["a.com"] || !hostSet["b.com"] {
		t.Fatalf("missing expected hostnames: %v", hostSet)
	}
}

func TestTraefikResolver_BacktickSyntax(t *testing.T) {
	r := NewTraefik(&mockIPProvider{ip: "1.2.3.4"})
	records, err := r.Records(context.Background(), traefikInspect(map[string]string{
		"traefik.http.routers.myapp.rule": "Host(`myapp.example.com`)",
	}))
	if err != nil || len(records) != 1 || records[0].Hostname != "myapp.example.com" {
		t.Fatalf("unexpected: records=%v err=%v", records, err)
	}
}

func TestTraefikResolver_DoubleQuoteSyntax(t *testing.T) {
	r := NewTraefik(&mockIPProvider{ip: "1.2.3.4"})
	records, err := r.Records(context.Background(), traefikInspect(map[string]string{
		"traefik.http.routers.myapp.rule": `Host("myapp.example.com")`,
	}))
	if err != nil || len(records) != 1 || records[0].Hostname != "myapp.example.com" {
		t.Fatalf("unexpected: records=%v err=%v", records, err)
	}
}

func TestTraefikResolver_NonHostRule(t *testing.T) {
	r := NewTraefik(&mockIPProvider{ip: "1.2.3.4"})
	records, err := r.Records(context.Background(), traefikInspect(map[string]string{
		"traefik.http.routers.myapp.rule": "PathPrefix(`/api`)",
	}))
	if err != nil || len(records) != 0 {
		t.Fatalf("expected no records, got %v (err=%v)", records, err)
	}
}

func TestTraefikResolver_NoMatchingLabels(t *testing.T) {
	r := NewTraefik(&mockIPProvider{ip: "1.2.3.4"})
	records, err := r.Records(context.Background(), traefikInspect(map[string]string{
		"dns.hostname":              "app.example.com",
		"traefik.enable":            "true",
		"traefik.http.services.foo": "bar",
	}))
	if err != nil || records != nil {
		t.Fatalf("expected nil nil, got %v %v", records, err)
	}
}

func TestTraefikResolver_IPProviderError(t *testing.T) {
	r := NewTraefik(&mockIPProvider{err: errors.New("container not found")})
	records, err := r.Records(context.Background(), traefikInspect(map[string]string{
		"traefik.http.routers.myapp.rule": "Host(`myapp.example.com`)",
	}))
	if err == nil {
		t.Fatal("expected error from IP provider, got nil")
	}
	if records != nil {
		t.Fatalf("expected nil records on error, got %v", records)
	}
}
