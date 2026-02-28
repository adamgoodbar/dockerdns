package resolver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

// countingInspector records call count for cache tests.
type countingInspector struct {
	resp  container.InspectResponse
	err   error
	calls int
}

func (c *countingInspector) ContainerInspect(_ context.Context, _ string) (container.InspectResponse, error) {
	c.calls++
	return c.resp, c.err
}

func inspectWithIP(ip string) container.InspectResponse {
	return container.InspectResponse{
		NetworkSettings: &container.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"bridge": {IPAddress: ip},
			},
		},
	}
}

func TestStaticIP(t *testing.T) {
	p := NewStaticIP("1.2.3.4")
	ip, err := p.ProxyIP(context.Background())
	if err != nil || ip != "1.2.3.4" {
		t.Fatalf("expected 1.2.3.4, got %q %v", ip, err)
	}
}

func TestContainerIP_HappyPath(t *testing.T) {
	insp := &countingInspector{resp: inspectWithIP("10.0.0.2")}
	p := NewContainerIP(insp, "traefik")
	ip, err := p.ProxyIP(context.Background())
	if err != nil || ip != "10.0.0.2" {
		t.Fatalf("expected 10.0.0.2, got %q %v", ip, err)
	}
}

func TestContainerIP_InspectError(t *testing.T) {
	insp := &countingInspector{err: errors.New("not found")}
	p := NewContainerIP(insp, "traefik")
	_, err := p.ProxyIP(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestContainerIP_NoIP(t *testing.T) {
	insp := &countingInspector{resp: inspectWithIP("")}
	p := NewContainerIP(insp, "traefik")
	_, err := p.ProxyIP(context.Background())
	if err == nil {
		t.Fatal("expected error for missing IP, got nil")
	}
}

func TestContainerIP_NilNetworkSettings(t *testing.T) {
	insp := &countingInspector{resp: container.InspectResponse{NetworkSettings: nil}}
	p := NewContainerIP(insp, "traefik")
	_, err := p.ProxyIP(context.Background())
	if err == nil {
		t.Fatal("expected error for nil NetworkSettings, got nil")
	}
}

func TestContainerIP_CacheHit(t *testing.T) {
	insp := &countingInspector{resp: inspectWithIP("10.0.0.2")}
	p := NewContainerIP(insp, "traefik")
	now := time.Now()
	p.now = func() time.Time { return now }

	p.ProxyIP(context.Background())
	p.ProxyIP(context.Background()) // within TTL — should use cache

	if insp.calls != 1 {
		t.Fatalf("expected 1 inspect call within TTL, got %d", insp.calls)
	}
}

func TestContainerIP_CacheExpiry(t *testing.T) {
	insp := &countingInspector{resp: inspectWithIP("10.0.0.2")}
	p := NewContainerIP(insp, "traefik")
	now := time.Now()
	p.now = func() time.Time { return now }

	p.ProxyIP(context.Background())

	// Advance clock past TTL.
	now = now.Add(p.ttl + time.Second)
	p.ProxyIP(context.Background())

	if insp.calls != 2 {
		t.Fatalf("expected 2 inspect calls after TTL expiry, got %d", insp.calls)
	}
}
