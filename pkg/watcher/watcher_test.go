package watcher

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/network"

	"dockerdns/pkg/resolver"
)

// mockDocker implements dockerClient.
type mockDocker struct {
	inspect    container.InspectResponse
	containers []container.Summary
	err        error
}

func (m *mockDocker) Events(_ context.Context, _ events.ListOptions) (<-chan events.Message, <-chan error) {
	return make(chan events.Message), make(chan error)
}

func (m *mockDocker) ContainerInspect(_ context.Context, _ string) (container.InspectResponse, error) {
	return m.inspect, m.err
}

func (m *mockDocker) ContainerList(_ context.Context, _ container.ListOptions) ([]container.Summary, error) {
	return m.containers, m.err
}

// mockRegistrar implements registrar.
type mockRegistrar struct {
	registered   []string
	deregistered []string
	resources    []string
	err          error
}

func (m *mockRegistrar) Register(hostname, _, resource string) error {
	if m.err != nil {
		return m.err
	}
	m.registered = append(m.registered, hostname)
	m.resources = append(m.resources, resource)
	return nil
}

func (m *mockRegistrar) Deregister(hostname, _ string) error {
	if m.err != nil {
		return m.err
	}
	m.deregistered = append(m.deregistered, hostname)
	return nil
}

// mockResolver implements resolver.Resolver.
type mockResolver struct {
	records []resolver.Record
	err     error
}

func (m *mockResolver) Records(_ context.Context, _ container.InspectResponse) ([]resolver.Record, error) {
	return m.records, m.err
}

func newTestWatcher(docker dockerClient, reg registrar, res resolver.Resolver) *Watcher {
	return &Watcher{
		docker:  docker,
		updater: reg,
		res:     res,
		cache:   make(map[string][]resolver.Record),
		workers: make(map[string]chan events.Message),
	}
}

func containerEvent(id, action string) events.Message {
	return events.Message{
		Action: events.Action(action),
		Actor:  events.Actor{ID: id},
	}
}

func inspectResponse(labels map[string]string, ip string) container.InspectResponse {
	nets := map[string]*network.EndpointSettings{}
	if ip != "" {
		nets["bridge"] = &network.EndpointSettings{IPAddress: ip}
	}
	return container.InspectResponse{
		Config: &container.Config{Labels: labels},
		NetworkSettings: &container.NetworkSettings{
			Networks: nets,
		},
	}
}

// ---- handleStart tests ----

func TestHandleStart_RegistersContainer(t *testing.T) {
	reg := &mockRegistrar{}
	res := &mockResolver{records: []resolver.Record{{Hostname: "app.example.com", IP: "10.0.0.1"}}}
	w := newTestWatcher(&mockDocker{}, reg, res)
	id := "aabbccddeeff0011"

	w.handleStart(context.Background(), containerEvent(id, "start"))

	if len(reg.registered) != 1 || reg.registered[0] != "app.example.com" {
		t.Fatalf("expected registered [app.example.com], got %v", reg.registered)
	}
	w.mu.Lock()
	cached, ok := w.cache[id]
	w.mu.Unlock()
	if !ok || len(cached) != 1 || cached[0].Hostname != "app.example.com" || cached[0].IP != "10.0.0.1" {
		t.Fatalf("unexpected cache entry: %+v", cached)
	}
	t.Logf("registered: hostname=%s ip=%s cached=%v", cached[0].Hostname, cached[0].IP, ok)
}

func TestHandleStart_SetsResource(t *testing.T) {
	reg := &mockRegistrar{}
	res := &mockResolver{records: []resolver.Record{{Hostname: "app.example.com", IP: "10.0.0.1"}}}
	w := newTestWatcher(&mockDocker{}, reg, res)

	w.handleStart(context.Background(), containerEvent("aabbccddeeff0011", "start"))

	if len(reg.resources) != 1 || reg.resources[0] != "container/aabbccddeeff" {
		t.Fatalf("expected resource container/aabbccddeeff, got %v", reg.resources)
	}
	t.Logf("resource correctly set: %s", reg.resources[0])
}

func TestHandleStart_NoRecords(t *testing.T) {
	reg := &mockRegistrar{}
	res := &mockResolver{records: nil}
	w := newTestWatcher(&mockDocker{}, reg, res)

	w.handleStart(context.Background(), containerEvent("aabbccddeeff0011", "start"))

	if len(reg.registered) != 0 {
		t.Fatalf("expected no registrations, got %v", reg.registered)
	}
	t.Logf("correctly skipped container with no records from resolver")
}

func TestHandleStart_InspectError(t *testing.T) {
	reg := &mockRegistrar{}
	docker := &mockDocker{err: errors.New("not found")}
	res := &mockResolver{records: []resolver.Record{{Hostname: "app.example.com", IP: "10.0.0.1"}}}
	w := newTestWatcher(docker, reg, res)

	w.handleStart(context.Background(), containerEvent("aabbccddeeff0011", "start"))

	if len(reg.registered) != 0 {
		t.Fatalf("expected no registrations, got %v", reg.registered)
	}
	t.Logf("correctly skipped registration after inspect error: %v", docker.err)
}

func TestHandleStart_ResolverError(t *testing.T) {
	reg := &mockRegistrar{}
	res := &mockResolver{err: errors.New("proxy IP: container not found")}
	w := newTestWatcher(&mockDocker{}, reg, res)

	w.handleStart(context.Background(), containerEvent("aabbccddeeff0011", "start"))

	if len(reg.registered) != 0 {
		t.Fatalf("expected no registrations on resolver error, got %v", reg.registered)
	}
	t.Logf("correctly skipped registration after resolver error: %v", res.err)
}

func TestHandleStart_RegisterError(t *testing.T) {
	reg := &mockRegistrar{err: errors.New("refused")}
	res := &mockResolver{records: []resolver.Record{{Hostname: "app.example.com", IP: "10.0.0.1"}}}
	w := newTestWatcher(&mockDocker{}, reg, res)
	id := "aabbccddeeff0011"

	w.handleStart(context.Background(), containerEvent(id, "start"))

	w.mu.Lock()
	_, cached := w.cache[id]
	w.mu.Unlock()
	if cached {
		t.Fatal("container should not be cached on register error")
	}
	t.Logf("correctly did not cache container after register error: %v", reg.err)
}

func TestHandleStart_PassesInspectToResolver(t *testing.T) {
	cap := &captureResolver{
		records: []resolver.Record{{Hostname: "app.example.com", IP: "10.0.0.1"}},
	}
	docker := &mockDocker{
		inspect: inspectResponse(map[string]string{"dns.hostname": "app.example.com"}, "10.0.0.1"),
	}
	w := newTestWatcher(docker, &mockRegistrar{}, cap)

	w.handleStart(context.Background(), containerEvent("aabbccddeeff0011", "start"))

	if cap.received.Config == nil {
		t.Fatal("resolver did not receive inspect response")
	}
	if cap.received.Config.Labels["dns.hostname"] != "app.example.com" {
		t.Fatalf("unexpected labels: %v", cap.received.Config.Labels)
	}
	t.Logf("resolver correctly received inspect response with labels: %v", cap.received.Config.Labels)
}

// captureResolver captures the inspect response passed to Records.
type captureResolver struct {
	received container.InspectResponse
	records  []resolver.Record
}

func (c *captureResolver) Records(_ context.Context, info container.InspectResponse) ([]resolver.Record, error) {
	c.received = info
	return c.records, nil
}

// ---- handleDie tests ----

func TestHandleDie_DeregistersContainer(t *testing.T) {
	reg := &mockRegistrar{}
	w := newTestWatcher(&mockDocker{}, reg, &mockResolver{})
	id := "aabbccddeeff0011"

	w.mu.Lock()
	w.cache[id] = []resolver.Record{{Hostname: "app.example.com", IP: "10.0.0.1"}}
	w.mu.Unlock()

	w.handleDie(containerEvent(id, "die"))

	if len(reg.deregistered) != 1 || reg.deregistered[0] != "app.example.com" {
		t.Fatalf("expected deregistered [app.example.com], got %v", reg.deregistered)
	}
	w.mu.Lock()
	_, still := w.cache[id]
	w.mu.Unlock()
	if still {
		t.Fatal("cache entry should be removed after die")
	}
	t.Logf("deregistered: hostname=%s cache evicted=%v", reg.deregistered[0], !still)
}

func TestHandleDie_UnknownContainer(t *testing.T) {
	reg := &mockRegistrar{}
	w := newTestWatcher(&mockDocker{}, reg, &mockResolver{})

	w.handleDie(containerEvent("aabbccddeeff0011", "die"))

	if len(reg.deregistered) != 0 {
		t.Fatalf("expected no deregistrations, got %v", reg.deregistered)
	}
	t.Logf("correctly ignored die event for unknown container")
}

// ---- proxy container restart test ----

func TestHandleStart_ProxyContainerRestart(t *testing.T) {
	reg := &mockRegistrar{}
	// Resolver returns a record with the new proxy IP.
	res := &mockResolver{records: []resolver.Record{{Hostname: "app.example.com", IP: "5.6.7.8"}}}
	w := newTestWatcher(&mockDocker{}, reg, res)
	w.proxyContainer = "traefik"

	// Simulate an existing cached registration with the old proxy IP.
	const containerID = "aabbccddeeff0011"
	w.mu.Lock()
	w.cache[containerID] = []resolver.Record{{Hostname: "app.example.com", IP: "1.2.3.4"}}
	w.mu.Unlock()

	// Simulate traefik restart event.
	w.handleStart(context.Background(), events.Message{
		Action: "start",
		Actor: events.Actor{
			ID:         "traefikid000000",
			Attributes: map[string]string{"name": "traefik"},
		},
	})

	if len(reg.deregistered) != 1 || reg.deregistered[0] != "app.example.com" {
		t.Fatalf("expected old record deregistered, got %v", reg.deregistered)
	}
	if len(reg.registered) != 1 || reg.registered[0] != "app.example.com" {
		t.Fatalf("expected re-registration, got %v", reg.registered)
	}
	t.Logf("proxy restart: deregistered old IP, registered new IP")
}

// ---- Run-level tests ----

func TestRun_SyncsExistingContainers(t *testing.T) {
	reg := &mockRegistrar{}
	res := &mockResolver{records: []resolver.Record{{Hostname: "app.example.com", IP: "10.0.0.1"}}}
	docker := &mockDocker{
		containers: []container.Summary{{ID: "aabbccddeeff0011"}},
	}
	w := newTestWatcher(docker, reg, res)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Run so the event loop exits immediately

	w.Run(ctx) //nolint:errcheck

	if len(reg.registered) != 1 || reg.registered[0] != "app.example.com" {
		t.Fatalf("expected existing container to be registered, got %v", reg.registered)
	}
	t.Logf("startup sync registered: %v", reg.registered)
}

func TestRun_DeregistersOnShutdown(t *testing.T) {
	reg := &mockRegistrar{}
	w := newTestWatcher(&mockDocker{}, reg, &mockResolver{})
	const id = "aabbccddeeff0011"

	w.mu.Lock()
	w.cache[id] = []resolver.Record{{Hostname: "app.example.com", IP: "10.0.0.1"}}
	w.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	w.Run(ctx) //nolint:errcheck

	if len(reg.deregistered) != 1 || reg.deregistered[0] != "app.example.com" {
		t.Fatalf("expected shutdown deregistration, got %v", reg.deregistered)
	}
	t.Logf("shutdown deregistered: %v", reg.deregistered)
}

func TestRun_CancelledContext(t *testing.T) {
	w := newTestWatcher(&mockDocker{}, &mockRegistrar{}, &mockResolver{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := w.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	t.Logf("Run returned expected error: %v", err)
}
