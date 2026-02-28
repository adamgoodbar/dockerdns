package watcher

import (
	"context"
	"log"
	"sync"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"

	"dockerdns/pkg/dnsupdater"
)

type entry struct {
	hostname string
	ip       string
}

type Watcher struct {
	docker  *client.Client
	updater *dnsupdater.Updater
	label   string

	mu    sync.Mutex
	cache map[string]entry // containerID -> entry
}

func New(docker *client.Client, u *dnsupdater.Updater, label string) *Watcher {
	return &Watcher{
		docker:  docker,
		updater: u,
		label:   label,
		cache:   make(map[string]entry),
	}
}

func (w *Watcher) Run(ctx context.Context) error {
	f := filters.NewArgs(filters.Arg("type", "container"))
	eventsCh, errsCh := w.docker.Events(ctx, events.ListOptions{Filters: f})

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errsCh:
			return err
		case ev := <-eventsCh:
			switch ev.Action {
			case "start":
				w.handleStart(ctx, ev)
			case "die":
				w.handleDie(ev)
			}
		}
	}
}

func (w *Watcher) handleStart(ctx context.Context, ev events.Message) {
	info, err := w.docker.ContainerInspect(ctx, ev.Actor.ID)
	if err != nil {
		log.Printf("inspect %s: %v", ev.Actor.ID[:12], err)
		return
	}

	hostname, ok := info.Config.Labels[w.label]
	if !ok {
		return
	}

	var ip string
	for _, net := range info.NetworkSettings.Networks {
		if net.IPAddress != "" {
			ip = net.IPAddress
			break
		}
	}
	if ip == "" {
		log.Printf("container %s has label %s=%s but no IP", ev.Actor.ID[:12], w.label, hostname)
		return
	}

	if err := w.updater.Register(hostname, ip); err != nil {
		log.Printf("register %s -> %s: %v", hostname, ip, err)
		return
	}

	w.mu.Lock()
	w.cache[ev.Actor.ID] = entry{hostname: hostname, ip: ip}
	w.mu.Unlock()

	log.Printf("registered %s -> %s", hostname, ip)
}

func (w *Watcher) handleDie(ev events.Message) {
	w.mu.Lock()
	e, ok := w.cache[ev.Actor.ID]
	if ok {
		delete(w.cache, ev.Actor.ID)
	}
	w.mu.Unlock()

	if !ok {
		return
	}

	if err := w.updater.Deregister(e.hostname, e.ip); err != nil {
		log.Printf("deregister %s: %v", e.hostname, err)
		return
	}

	log.Printf("deregistered %s", e.hostname)
}
