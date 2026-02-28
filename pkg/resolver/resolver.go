package resolver

import (
	"context"

	"github.com/docker/docker/api/types/container"
)

type Record struct {
	Hostname string
	IP       string
	Resource string // e.g. "container/abc123def456" — set by the watcher
}

type Resolver interface {
	Records(ctx context.Context, info container.InspectResponse) ([]Record, error)
}
