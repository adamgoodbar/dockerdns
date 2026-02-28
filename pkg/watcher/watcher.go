package watcher

import (
	"context"
	"log"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"

	"dockerdns/pkg/dnsupdater"
	"dockerdns/pkg/resolver"
)

type dockerClient interface {
	Events(ctx context.Context, options events.ListOptions) (<-chan events.Message, <-chan error)
	ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error)
	ContainerList(ctx context.Context, options container.ListOptions) ([]container.Summary, error)
}

type registrar interface {
	Register(hostname, ip, resource string) error
	Deregister(hostname, ip string) error
}

type Watcher struct {
	docker         dockerClient
	updater        registrar
	res            resolver.Resolver
	proxyContainer string

	mu    sync.Mutex
	cache map[string][]resolver.Record // containerID -> registered records

	// Per-container worker goroutines for serialised event processing.
	workerMu sync.Mutex
	workers  map[string]chan events.Message
	wg       sync.WaitGroup
}

func New(docker dockerClient, u *dnsupdater.Updater, res resolver.Resolver, proxyContainer string) *Watcher {
	return &Watcher{
		docker:         docker,
		updater:        u,
		res:            res,
		proxyContainer: proxyContainer,
		cache:          make(map[string][]resolver.Record),
		workers:        make(map[string]chan events.Message),
	}
}

// Run subscribes to Docker events and processes them until ctx is cancelled.
// On shutdown it deregisters all DNS records it previously registered.
func (w *Watcher) Run(ctx context.Context) error {
	w.syncExisting(ctx)

	f := filters.NewArgs(filters.Arg("type", "container"))
	eventsCh, errsCh := w.docker.Events(ctx, events.ListOptions{Filters: f})

	for {
		select {
		case <-ctx.Done():
			w.wg.Wait()
			w.deregisterAll()
			return ctx.Err()
		case err := <-errsCh:
			return err
		case ev := <-eventsCh:
			switch ev.Action {
			case "start", "die":
				w.dispatch(ctx, ev)
			}
		}
	}
}

// syncExisting registers DNS records for all currently-running containers.
func (w *Watcher) syncExisting(ctx context.Context) {
	containers, err := w.docker.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		log.Printf("list existing containers: %v", err)
		return
	}
	for _, c := range containers {
		w.handleStart(ctx, events.Message{Action: "start", Actor: events.Actor{ID: c.ID}})
	}
}

// deregisterAll removes DNS records for every container still in the cache.
// Called on clean shutdown after all worker goroutines have finished.
func (w *Watcher) deregisterAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for id, records := range w.cache {
		for _, rec := range records {
			if err := w.updater.Deregister(rec.Hostname, rec.IP); err != nil {
				log.Printf("shutdown deregister %s: %v", rec.Hostname, err)
				continue
			}
			log.Printf("shutdown deregistered %s", rec.Hostname)
		}
		delete(w.cache, id)
	}
}

// dispatch routes an event to the per-container worker goroutine, creating
// one if it does not exist yet. Serialising events per container ID prevents
// a fast die+start sequence from racing against an in-progress handleStart.
func (w *Watcher) dispatch(ctx context.Context, ev events.Message) {
	id := ev.Actor.ID
	w.workerMu.Lock()
	ch, ok := w.workers[id]
	if !ok {
		ch = make(chan events.Message, 16)
		w.workers[id] = ch
		w.wg.Add(1)
		go w.runWorker(ctx, id, ch)
	}
	w.workerMu.Unlock()

	select {
	case ch <- ev:
	default:
		log.Printf("event queue full for %s, dropping %s", id[:12], ev.Action)
	}
}

func (w *Watcher) runWorker(ctx context.Context, id string, ch <-chan events.Message) {
	defer func() {
		w.wg.Done()
		w.workerMu.Lock()
		delete(w.workers, id)
		w.workerMu.Unlock()
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			switch ev.Action {
			case "start":
				w.handleStart(ctx, ev)
			case "die":
				w.handleDie(ev)
				return // container is gone; exit this worker
			}
		}
	}
}

func (w *Watcher) handleStart(ctx context.Context, ev events.Message) {
	// If the event is from the proxy container itself, re-register every
	// cached container so their records reflect the proxy's new IP.
	if w.proxyContainer != "" && ev.Actor.Attributes["name"] == w.proxyContainer {
		log.Printf("proxy container %s restarted, re-registering all cached containers", w.proxyContainer)
		w.reregisterAll(ctx)
		return
	}

	info, err := w.docker.ContainerInspect(ctx, ev.Actor.ID)
	if err != nil {
		log.Printf("inspect %s: %v", ev.Actor.ID[:12], err)
		return
	}

	records, err := w.res.Records(ctx, info)
	if err != nil {
		log.Printf("resolve %s: %v", ev.Actor.ID[:12], err)
		return
	}
	if len(records) == 0 {
		return
	}

	resource := "container/" + ev.Actor.ID[:12]
	var registered []resolver.Record
	for _, rec := range records {
		rec.Resource = resource
		if err := w.updater.Register(rec.Hostname, rec.IP, rec.Resource); err != nil {
			log.Printf("register %s -> %s: %v", rec.Hostname, rec.IP, err)
			continue
		}
		log.Printf("registered %s -> %s", rec.Hostname, rec.IP)
		registered = append(registered, rec)
	}

	if len(registered) == 0 {
		return
	}

	w.mu.Lock()
	w.cache[ev.Actor.ID] = registered
	w.mu.Unlock()
}

// reregisterAll deregisters every cached record and re-registers each
// container. Called when the proxy container restarts and its IP may have
// changed.
func (w *Watcher) reregisterAll(ctx context.Context) {
	w.mu.Lock()
	snapshot := make(map[string][]resolver.Record, len(w.cache))
	for id, recs := range w.cache {
		snapshot[id] = recs
		delete(w.cache, id)
	}
	w.mu.Unlock()

	for id, old := range snapshot {
		for _, rec := range old {
			if err := w.updater.Deregister(rec.Hostname, rec.IP); err != nil {
				log.Printf("proxy restart: deregister %s: %v", rec.Hostname, err)
			}
		}
		w.handleStart(ctx, events.Message{Action: "start", Actor: events.Actor{ID: id}})
	}
}

func (w *Watcher) handleDie(ev events.Message) {
	w.mu.Lock()
	records, ok := w.cache[ev.Actor.ID]
	if ok {
		delete(w.cache, ev.Actor.ID)
	}
	w.mu.Unlock()

	if !ok {
		return
	}

	for _, rec := range records {
		if err := w.updater.Deregister(rec.Hostname, rec.IP); err != nil {
			log.Printf("deregister %s: %v", rec.Hostname, err)
			continue
		}
		log.Printf("deregistered %s", rec.Hostname)
	}
}
