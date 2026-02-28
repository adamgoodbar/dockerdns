package resolver

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
)

// IPProvider resolves the proxy IP at event time.
type IPProvider interface {
	ProxyIP(ctx context.Context) (string, error)
}

// StaticIP returns a fixed IP address.
type StaticIP struct {
	ip string
}

func NewStaticIP(ip string) *StaticIP {
	return &StaticIP{ip: ip}
}

func (s *StaticIP) ProxyIP(_ context.Context) (string, error) {
	return s.ip, nil
}

// dockerInspector is satisfied by the real Docker client.
type dockerInspector interface {
	ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error)
}

// ContainerIP resolves the IP of a named container via Docker inspect,
// caching the result for ttl to avoid redundant calls during container-start bursts.
type ContainerIP struct {
	docker   dockerInspector
	name     string
	ttl      time.Duration
	now      func() time.Time

	mu       sync.Mutex
	cached   string
	cachedAt time.Time
}

func NewContainerIP(docker dockerInspector, name string) *ContainerIP {
	return &ContainerIP{
		docker: docker,
		name:   name,
		ttl:    10 * time.Second,
		now:    time.Now,
	}
}

func (c *ContainerIP) ProxyIP(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.cached != "" && c.now().Sub(c.cachedAt) < c.ttl {
		ip := c.cached
		c.mu.Unlock()
		return ip, nil
	}
	c.mu.Unlock()

	info, err := c.docker.ContainerInspect(ctx, c.name)
	if err != nil {
		return "", fmt.Errorf("inspect container %q: %w", c.name, err)
	}
	if info.NetworkSettings != nil {
		for _, net := range info.NetworkSettings.Networks {
			if net.IPAddress != "" {
				c.mu.Lock()
				c.cached = net.IPAddress
				c.cachedAt = c.now()
				c.mu.Unlock()
				return net.IPAddress, nil
			}
		}
	}
	return "", fmt.Errorf("container %q has no IP address", c.name)
}
